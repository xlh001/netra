package main

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target bpfel -go-package main -no-strip xdpflow bpf/xdp_flow.c -- -I/usr/include/x86_64-linux-gnu

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

func recoverAndLog(name string) {
	if r := recover(); r != nil {
		log.Printf("%s: recovered from panic: %v\n%s", name, r, debug.Stack())
	}
}

// retentionDuration implements flag.Value for -db-retention, accepting
// only "<N>d" (days) or "<N>m" (months, approximated as 30 days each) --
// retention is never meaningfully set finer than a day, and reusing
// time.Duration's "h" syntax made every real value an awkward
// multiply-by-24 (14 days as "336h").
type retentionDuration struct {
	d time.Duration
}

func (r *retentionDuration) String() string {
	if r.d == 0 {
		return "30d"
	}
	return strconv.Itoa(int(r.d/(24*time.Hour))) + "d"
}

func (r *retentionDuration) Set(s string) error {
	if len(s) < 2 {
		return fmt.Errorf("invalid -db-retention %q: expected a number followed by d (days) or m (months), e.g. 30d or 1m", s)
	}
	unit := s[len(s)-1]
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return fmt.Errorf("invalid -db-retention %q: expected a number followed by d (days) or m (months), e.g. 30d or 1m", s)
	}
	switch unit {
	case 'd':
		r.d = time.Duration(n) * 24 * time.Hour
	case 'm':
		r.d = time.Duration(n) * 30 * 24 * time.Hour
	default:
		return fmt.Errorf("invalid -db-retention unit %q in %q: only d (days) or m (months) are supported", string(unit), s)
	}
	return nil
}

func main() {
	ifaceNames := flag.String("iface", "", "network interface(s) to attach the XDP program to; comma-separated for multiple, e.g. eno1,eno2 (required) -- the same loaded program attaches to each, sharing one set of maps, so multi-interface traffic aggregates into a single view")
	generic := flag.Bool("generic", false, "force generic/SKB XDP mode (needed on NICs without native XDP driver support, e.g. WSL2's virtual NIC, or when the target NIC has no spare hardware queues left for native XDP); applies to every interface in -iface")
	interval := flag.Duration("interval", 5*time.Second, "collector tick: how often to read the map and roll a new dashboard bucket")
	webAddr := flag.String("web-addr", ":10211", "address to serve the web dashboard on, e.g. :8080 (set to empty string to disable the web dashboard and run collection-only)")
	geoipDB := flag.String("geoip-db", "GeoLite2-City.mmdb", "path to a MaxMind GeoLite2-City.mmdb file, enables the world map panel (optional; kept external so updating GeoIP data doesn't require rebuilding netra). Defaults to that filename in the working directory -- silently disabled if not found there, override the path if you keep it elsewhere")
	geoipASNDB := flag.String("geoip-asn-db", "GeoLite2-ASN.mmdb", "path to a MaxMind GeoLite2-ASN.mmdb file, adds an organization name (e.g. \"Amazon.com\") next to public IPs (optional, independent of -geoip-db; kept external for the same reason). Defaults to that filename in the working directory -- silently disabled if not found there, override the path if you keep it elsewhere")
	dbPath := flag.String("db", "netra.db", "path to the SQLite file for persisted history and app state (users, config, alerts, etc.) -- always on, override the path if needed")
	dbRetention := &retentionDuration{d: 30 * 24 * time.Hour}
	flag.Var(dbRetention, "db-retention", "how long persisted history is kept before being pruned, as <N>d (days) or <N>m (months, = 30 days each), e.g. 30d or 1m")
	dbHotPeriod := flag.Duration("db-hot-period", 1*time.Hour, "how often flow/ip/port traffic history is sealed from the DuckDB hot buffer into a Parquet file; also the effective minimum retention granularity")
	flag.Parse()
	if *ifaceNames == "" {
		log.Fatal("usage: netra -iface <ifname>[,<ifname>...] [-web-addr <addr>]")
	}

	var ifaces []net.Interface
	for _, name := range strings.Split(*ifaceNames, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		iface, err := net.InterfaceByName(name)
		if err != nil {
			log.Fatalf("lookup interface %q: %v", name, err)
		}
		ifaces = append(ifaces, *iface)
	}
	if len(ifaces) == 0 {
		log.Fatal("usage: netra -iface <ifname>[,<ifname>...] [-web-addr <addr>]")
	}

	for _, iface := range ifaces {
		alreadyPromisc, err := setPromiscuous(iface.Name)
		if err != nil {
			log.Fatalf("enable promiscuous mode on %s: %v", iface.Name, err)
		}
		if alreadyPromisc {
			log.Printf("%s is already in promiscuous mode, leaving it as-is", iface.Name)
		} else {
			log.Printf("enabled promiscuous mode on %s (needed to see mirrored traffic not addressed to this host's own MAC)", iface.Name)
			defer func() {
				if err := clearPromiscuous(iface.Name); err != nil {
					log.Printf("disable promiscuous mode on %s: %v", iface.Name, err)
				}
			}()
		}
	}

	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("remove memlock rlimit: %v", err)
	}

	objs := xdpflowObjects{}
	if err := loadXdpflowObjects(&objs, nil); err != nil {
		log.Fatalf("load eBPF objects: %v", err)
	}
	defer objs.Close()

	for _, iface := range ifaces {
		opts := link.XDPOptions{
			Program:   objs.XdpFlowCount,
			Interface: iface.Index,
		}
		if *generic {
			opts.Flags = link.XDPGenericMode
		}
		xdpLink, err := link.AttachXDP(opts)
		if err != nil {
			log.Fatalf("attach XDP to %s: %v", iface.Name, err)
		}
		defer xdpLink.Close()
		log.Printf("XDP program attached to %s (ifindex %d)", iface.Name, iface.Index)
	}
	log.Println("Ctrl-C to detach and exit")

	agg := newAggregator(*interval)

	sniReader, err := startSNIReader(objs.SniEvents, agg.recordDomain)
	if err != nil {
		log.Fatalf("start SNI reader: %v", err)
	}
	defer sniReader.Close()

	httpReader, err := startHTTPReader(objs.HttpEvents, agg.recordDomain)
	if err != nil {
		log.Fatalf("start HTTP host reader: %v", err)
	}
	defer httpReader.Close()

	geoDB, err := loadGeoDB(*geoipDB)
	if err != nil {
		log.Printf("geoip: could not open %q, world map will be disabled: %v", *geoipDB, err)
		geoDB = nil
	}
	if geoDB != nil {
		defer geoDB.Close()
	}

	asnDB, err := loadASNDB(*geoipASNDB)
	if err != nil {
		log.Printf("geoip: could not open %q, organization names will be disabled: %v", *geoipASNDB, err)
		asnDB = nil
	}
	if asnDB != nil {
		defer asnDB.Close()
	}

	store, err := NewStore(*dbPath, dbRetention.d, *dbHotPeriod)
	if err != nil {
		log.Fatalf("open store %q: %v", *dbPath, err)
	}
	defer store.Close()
	log.Printf("persisting history to %s (retention %s)", *dbPath, dbRetention.String())

	cfg := defaultConfig()
	if store != nil {
		if saved, ok, err := store.LoadConfig(); err != nil {
			log.Printf("load config: %v (using defaults)", err)
		} else if ok {
			cfg.Apply(saved)
		}
	}
	applyAnomalyConfig(agg, cfg)
	applyCapacityConfig(agg, cfg)

	kafkaExp := newKafkaExporter()
	defer kafkaExp.Close()
	applyKafkaConfig(kafkaExp, cfg)

	ipTags := newIPTagCache()
	mcpMgr := newMCPManager()
	defer mcpMgr.closeAll()
	if store != nil {
		if saved, err := store.ListIPTags(); err != nil {
			log.Printf("load ip tags: %v", err)
		} else {
			ipTags.rebuild(saved)
		}
		if savedPorts, err := store.ListPortMappings(); err != nil {
			log.Printf("load port mappings: %v", err)
		} else {
			portMappings.rebuild(savedPorts)
		}
		if savedMCP, err := store.ListMCPServers(); err != nil {
			log.Printf("load mcp servers: %v", err)
		} else {
			go mcpMgr.sync(savedMCP)
		}
	}
	go mcpMgr.runHealthLoop(mcpHealthCheckInterval)

	if *webAddr != "" {
		secret, err := store.EnsureAuthSecret()
		if err != nil {
			log.Fatalf("set up auth secret: %v", err)
		}
		if result, created, err := store.BootstrapAdminIfEmpty(); err != nil {
			log.Fatalf("bootstrap admin account: %v", err)
		} else if created {
			log.Printf("created initial admin account -- username: %s  password: %s  (shown once, log in and change it)", result.AdminUsername, result.AdminPassword)
			log.Printf("created dashboard account (long-lived session, for screen-casting) -- username: %s  password: %s  (shown once)", result.DashboardUsername, result.DashboardPassword)
		}
		mon := newMonitor(*dbPath, store)
		go mon.run()
		startWebServer(*webAddr, agg, geoDB, asnDB, store, cfg, kafkaExp, secret, mon, ipTags, mcpMgr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	prev := map[xdpflowFlowKey]xdpflowFlowStats{}

	for {
		select {
		case <-ctx.Done():
			log.Println("detaching...")
			return
		case now := <-ticker.C:

			func() {
				defer recoverAndLog("collection tick")
				cur, ok := readFlows(&objs)
				if !ok {

					agg.recordReadFailure(now)
					return
				}
				snap := agg.push(now, cur, prev)
				agg.recordAnomalyCandidates(now, cur, prev)
				alerts := agg.threatAlerts()
				if cfg.Snapshot().PersistScanAlerts {

					snap.scanAlerts = alerts
				}
				notifyAlerts(store, cfg.Snapshot(), alerts, now, ipTags)
				store.Enqueue(snap)
				kafkaExp.Publish(snap)
				prev = cur
			}()
		}
	}
}

func readFlows(objs *xdpflowObjects) (map[xdpflowFlowKey]xdpflowFlowStats, bool) {
	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		out := make(map[xdpflowFlowKey]xdpflowFlowStats)
		var key xdpflowFlowKey
		var stats xdpflowFlowStats
		iter := objs.FlowStatsMap.Iterate()
		for iter.Next(&key, &stats) {
			out[key] = stats
		}
		if err := iter.Err(); err != nil {
			if attempt == maxAttempts {
				log.Printf("map iterate failed after %d attempts (transient, will retry next tick): %v", maxAttempts, err)
				return nil, false
			}
			continue
		}
		return out, true
	}
	return nil, false
}

func ipString(addr uint32) string {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, addr)
	return net.IP(b).String()
}

func ntohs(v uint16) uint16 {
	return (v << 8) | (v >> 8)
}

var ianaProtoNames = map[uint8]string{
	0: "hopopt", 1: "icmp", 2: "igmp", 3: "ggp", 4: "ipip", 5: "st", 6: "tcp",
	7: "cbt", 8: "egp", 9: "igp", 10: "bbn-rcc-mon", 11: "nvp-ii", 12: "pup",
	13: "argus", 14: "emcon", 15: "xnet", 16: "chaos", 17: "udp", 18: "mux",
	19: "dcn-meas", 20: "hmp", 21: "prm", 22: "xns-idp", 23: "trunk-1",
	24: "trunk-2", 25: "leaf-1", 26: "leaf-2", 27: "rdp", 28: "irtp",
	29: "iso-tp4", 30: "netblt", 31: "mfe-nsp", 32: "merit-inp", 33: "dccp",
	34: "3pc", 35: "idpr", 36: "xtp", 37: "ddp", 38: "idpr-cmtp", 39: "tp++",
	40: "il", 41: "ipv6", 42: "sdrp", 43: "ipv6-route", 44: "ipv6-frag",
	45: "idrp", 46: "rsvp", 47: "gre", 48: "dsr", 49: "bna", 50: "esp",
	51: "ah", 52: "i-nlsp", 53: "swipe", 54: "narp", 55: "mobile",
	56: "tlsp", 57: "skip", 58: "ipv6-icmp", 59: "ipv6-nonxt",
	60: "ipv6-opts", 62: "cftp", 64: "sat-expak", 65: "kryptolan",
	66: "rvd", 67: "ippc", 69: "sat-mon", 70: "visa", 71: "ipcv",
	72: "cpnx", 73: "cphb", 74: "wsn", 75: "pvp", 76: "br-sat-mon",
	77: "sun-nd", 78: "wb-mon", 79: "wb-expak", 80: "iso-ip", 81: "vmtp",
	82: "secure-vmtp", 83: "vines", 84: "ttp", 85: "nsfnet-igp", 86: "dgp",
	87: "tcf", 88: "eigrp", 89: "ospf", 90: "sprite-rpc", 91: "larp",
	92: "mtp", 93: "ax.25", 94: "ipip-encap", 95: "micp", 96: "scc-sp",
	97: "etherip", 98: "encap", 100: "gmtp", 101: "ifmp", 102: "pnni",
	103: "pim", 104: "aris", 105: "scps", 106: "qnx", 107: "a/n",
	108: "ipcomp", 109: "snp", 110: "compaq-peer", 111: "ipx-in-ip",
	112: "vrrp", 113: "pgm", 115: "l2tp", 116: "ddx", 117: "iatp",
	118: "stp", 119: "srp", 120: "uti", 121: "smp", 122: "sm", 123: "ptp",
	124: "isis-over-ipv4", 125: "fire", 126: "crtp", 127: "crudp",
	128: "sscopmce", 129: "iplt", 130: "sps", 131: "pipe", 132: "sctp",
	133: "fc", 134: "rsvp-e2e-ignore", 135: "mobility-header",
	136: "udplite", 137: "mpls-in-ip", 138: "manet", 139: "hip",
	140: "shim6", 141: "wesp", 142: "rohc",
}

func protoName(p uint8) string {
	if name, ok := ianaProtoNames[p]; ok {
		return name
	}
	return fmt.Sprintf("proto%d", p)
}
