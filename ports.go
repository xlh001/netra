package main

import (
	"fmt"
	"sort"
	"strconv"
)

var wellKnownPorts = map[uint16]string{
	20:    "ftp-data",
	21:    "ftp",
	22:    "ssh",
	23:    "telnet",
	25:    "smtp",
	53:    "dns",
	67:    "dhcp",
	68:    "dhcp",
	69:    "tftp",
	80:    "http",
	110:   "pop3",
	111:   "rpcbind",
	123:   "ntp",
	135:   "msrpc",
	137:   "netbios",
	138:   "netbios",
	139:   "netbios",
	143:   "imap",
	161:   "snmp",
	162:   "snmp-trap",
	179:   "bgp",
	389:   "ldap",
	443:   "https",
	445:   "smb",
	465:   "smtps",
	514:   "syslog",
	515:   "printer",
	520:   "rip",
	587:   "smtp-submission",
	593:   "http-rpc",
	636:   "ldaps",
	873:   "rsync",
	902:   "vmware",
	989:   "ftps-data",
	990:   "ftps",
	993:   "imaps",
	995:   "pop3s",
	1080:  "socks",
	1194:  "openvpn",
	1433:  "mssql",
	1521:  "oracle",
	1723:  "pptp",
	1883:  "mqtt",
	2049:  "nfs",
	2181:  "zookeeper",
	2375:  "docker",
	2376:  "docker-tls",
	2379:  "etcd",
	2380:  "etcd-peer",
	3000:  "dev-http",
	3128:  "squid",
	3268:  "ldap-gc",
	3306:  "mysql",
	3389:  "rdp",
	4369:  "epmd",
	4789:  "vxlan",
	5000:  "http-alt",
	5044:  "logstash",
	5060:  "sip",
	5061:  "sips",
	5222:  "xmpp",
	5432:  "postgresql",
	5601:  "kibana",
	5671:  "amqps",
	5672:  "amqp",
	5900:  "vnc",
	5984:  "couchdb",
	6379:  "redis",
	6443:  "kube-apiserver",
	6660:  "irc",
	6667:  "irc",
	7000:  "cassandra",
	7001:  "cassandra-ssl",
	7077:  "spark",
	7199:  "cassandra-jmx",
	7474:  "neo4j",
	8000:  "http-alt",
	8009:  "ajp",
	8080:  "http-proxy",
	8086:  "influxdb",
	8123:  "clickhouse-http",
	8161:  "activemq",
	8200:  "vault",
	8443:  "https-alt",
	8500:  "consul",
	8529:  "arangodb",
	8888:  "http-alt",
	9000:  "php-fpm",
	9042:  "cassandra-cql",
	9092:  "kafka",
	9200:  "elasticsearch",
	9300:  "elasticsearch-transport",
	9418:  "git",
	9999:  "http-alt",
	11211: "memcached",
	15672: "rabbitmq-mgmt",
	16379: "redis-cluster",
	20000: "dnp3",
	27017: "mongodb",
	27018: "mongodb-shard",
	28015: "rethinkdb",
	50000: "sap",
	50070: "hadoop-namenode",
}

func serviceName(port uint16) string {
	if name, ok := wellKnownPorts[port]; ok {
		return name
	}
	return ianaPortNames[port]
}

func resolveService(dport uint16, dpiService string) (service string, dpi bool) {
	if dpiService != "" {
		return dpiService, true
	}
	return serviceName(dport), false
}

func effectivePort(svcPort, dport uint16) uint16 {
	if svcPort != 0 {
		return svcPort
	}
	return dport
}

func resolveServiceForFlow(srcPort, dstPort, svcPort uint16, dpiService string) (service string, dpi bool, svcOnSrc bool) {
	service, dpi = resolveService(effectivePort(svcPort, dstPort), dpiService)
	svcOnSrc = svcPort != 0 && svcPort == srcPort
	return
}

var serviceCategories = map[string]string{
	"http": "Web", "https": "Web", "http-alt": "Web", "https-alt": "Web", "http-proxy": "Web", "http-rpc": "Web", "ajp": "Web", "squid": "Web", "php-fpm": "Web",

	"mysql": "数据库", "postgresql": "数据库", "mssql": "数据库", "oracle": "数据库", "mongodb": "数据库", "mongodb-shard": "数据库",
	"redis": "数据库", "redis-cluster": "数据库", "cassandra": "数据库", "cassandra-ssl": "数据库", "cassandra-cql": "数据库", "cassandra-jmx": "数据库",
	"couchdb": "数据库", "neo4j": "数据库", "arangodb": "数据库", "rethinkdb": "数据库", "influxdb": "数据库", "clickhouse-http": "数据库", "memcached": "数据库",

	"ssh": "远程管理", "telnet": "远程管理", "rdp": "远程管理", "vnc": "远程管理", "socks": "远程管理", "pptp": "远程管理", "openvpn": "远程管理",

	"ftp": "文件传输", "ftp-data": "文件传输", "ftps": "文件传输", "ftps-data": "文件传输", "tftp": "文件传输", "rsync": "文件传输", "nfs": "文件传输", "smb": "文件传输",

	"smtp": "邮件", "smtps": "邮件", "smtp-submission": "邮件", "pop3": "邮件", "pop3s": "邮件", "imap": "邮件", "imaps": "邮件",

	"ldap": "目录服务", "ldaps": "目录服务", "ldap-gc": "目录服务",

	"dns": "网络基础设施", "dhcp": "网络基础设施", "ntp": "网络基础设施", "bgp": "网络基础设施", "rip": "网络基础设施", "vxlan": "网络基础设施",
	"msrpc": "网络基础设施", "rpcbind": "网络基础设施", "netbios": "网络基础设施", "snmp": "网络基础设施", "snmp-trap": "网络基础设施",
	"syslog": "网络基础设施", "printer": "网络基础设施", "epmd": "网络基础设施",

	"amqp": "消息队列", "amqps": "消息队列", "kafka": "消息队列", "rabbitmq-mgmt": "消息队列", "xmpp": "消息队列", "irc": "消息队列", "activemq": "消息队列",

	"sip": "语音通信", "sips": "语音通信",

	"docker": "容器/云原生", "docker-tls": "容器/云原生", "etcd": "容器/云原生", "etcd-peer": "容器/云原生", "kube-apiserver": "容器/云原生",
	"consul": "容器/云原生", "vault": "容器/云原生",

	"elasticsearch": "监控/日志", "elasticsearch-transport": "监控/日志", "kibana": "监控/日志", "logstash": "监控/日志",

	"spark": "大数据/计算", "hadoop-namenode": "大数据/计算", "zookeeper": "大数据/计算",

	"dnp3": "工控/物联网", "mqtt": "工控/物联网",

	"git": "开发工具", "dev-http": "开发工具",

	"vmware": "虚拟化",

	"sap": "企业应用",
}

func serviceCategory(port uint16) string {
	svc := serviceName(port)
	if svc == "" {
		return "其他"
	}
	if cat, ok := serviceCategories[svc]; ok {
		return cat
	}
	return "其他"
}

type portAgg struct {
	Packets uint64
	Bytes   uint64
}

const categoryTopServices = 10

func buildCategoryStats(portTotals map[uint16]portAgg) []CategoryStat {
	catServices := map[string]map[string]portAgg{}
	for port, agg := range portTotals {
		cat := serviceCategory(port)
		svc := serviceName(port)
		if svc == "" {
			svc = strconv.Itoa(int(port))
		}
		services, ok := catServices[cat]
		if !ok {
			services = map[string]portAgg{}
			catServices[cat] = services
		}
		e := services[svc]
		e.Packets += agg.Packets
		e.Bytes += agg.Bytes
		services[svc] = e
	}

	var out []CategoryStat
	for cat, services := range catServices {
		cs := CategoryStat{Category: cat}
		for svc, agg := range services {
			cs.Packets += agg.Packets
			cs.Bytes += agg.Bytes
			cs.Services = append(cs.Services, ServiceStat{Service: svc, Packets: agg.Packets, Bytes: agg.Bytes})
		}
		sort.Slice(cs.Services, func(i, j int) bool { return cs.Services[i].Bytes > cs.Services[j].Bytes })
		if len(cs.Services) > categoryTopServices {
			var otherPackets, otherBytes uint64
			for _, s := range cs.Services[categoryTopServices:] {
				otherPackets += s.Packets
				otherBytes += s.Bytes
			}
			cs.Services = append(cs.Services[:categoryTopServices], ServiceStat{
				Service: fmt.Sprintf("其他 (%d 个)", len(cs.Services)-categoryTopServices),
				Packets: otherPackets,
				Bytes:   otherBytes,
			})
		}
		out = append(out, cs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}
