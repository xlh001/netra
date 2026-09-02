//go:build ignore

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/in.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

struct flow_key {
	__u32 saddr;
	__u32 daddr;
	__u16 sport;
	__u16 dport;
	__u8 proto;
	__u8 pad[3];

};

#define DPI_MAX_CAPTURES_PER_FLOW 3

struct flow_stats {
	__u64 packets;
	__u64 bytes;
	__u8 dpi_capture_count;
	__u8 pad[5];
	__u16 svc_port;
};

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, struct flow_key);
	__type(value, struct flow_stats);
	__uint(max_entries, 1048576);
} flow_stats_map SEC(".maps");

#define DPI_CAPTURE_LEN 256

struct dpi_event {
	__u32 saddr;
	__u32 daddr;
	__u16 sport;
	__u16 dport;
	__u8 proto;
	__u8 pad[3];
	__u32 payload_len;
	__u8 payload[DPI_CAPTURE_LEN];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 23);
} dpi_events SEC(".maps");

#define SNI_CAPTURE_LEN 512

struct sni_event {
	__u32 saddr;
	__u32 daddr;
	__u16 sport;
	__u16 dport;
	__u32 payload_len;
	__u8 payload[SNI_CAPTURE_LEN];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 20);
} sni_events SEC(".maps");

#define HTTP_CAPTURE_LEN 512

struct http_event {
	__u32 saddr;
	__u32 daddr;
	__u16 sport;
	__u16 dport;
	__u32 payload_len;
	__u8 payload[HTTP_CAPTURE_LEN];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 20);
} http_events SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, struct flow_key);
	__type(value, __u8);
	__uint(max_entries, 65536);
} sql_audit_flags SEC(".maps");

#define SQL_AUDIT_CAPTURE_LEN 512

struct sql_audit_event {
	__u32 saddr;
	__u32 daddr;
	__u16 sport;
	__u16 dport;
	__u32 payload_len;
	__u8 truncated;
	__u8 pad[3];
	__u8 payload[SQL_AUDIT_CAPTURE_LEN];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 24);
} sql_audit_events SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__type(key, struct flow_key);
	__type(value, __u8);
	__uint(max_entries, 65536);
} http_auth_flags SEC(".maps");

#define HTTP_AUTH_CAPTURE_LEN 512

struct http_auth_event {
	__u32 saddr;
	__u32 daddr;
	__u16 sport;
	__u16 dport;
	__u32 payload_len;
	__u8 truncated;
	__u8 pad[3];
	__u8 payload[HTTP_AUTH_CAPTURE_LEN];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 24);
} http_auth_events SEC(".maps");

static __always_inline int parse_flow(void *data, void *data_end, struct flow_key *key)
{
	struct ethhdr *eth = data;
	if ((void *)(eth + 1) > data_end)
		return -1;

	if (eth->h_proto != bpf_htons(ETH_P_IP))
		return -1;

	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end)
		return -1;

	if (ip->ihl < 5)
		return -1;

	void *l4 = (void *)ip + (ip->ihl * 4);

	key->saddr = ip->saddr;
	key->daddr = ip->daddr;
	key->proto = ip->protocol;

	if (ip->protocol == IPPROTO_TCP) {
		struct tcphdr *tcp = l4;
		if ((void *)(tcp + 1) > data_end)
			return -1;
		key->sport = tcp->source;
		key->dport = tcp->dest;
	} else if (ip->protocol == IPPROTO_UDP) {
		struct udphdr *udp = l4;
		if ((void *)(udp + 1) > data_end)
			return -1;
		key->sport = udp->source;
		key->dport = udp->dest;
	} else {
		key->sport = 0;
		key->dport = 0;
	}

	return 0;
}

static __always_inline __u16 resolve_svc_port(void *data, void *data_end, const struct flow_key *key)
{
	if (key->proto != IPPROTO_TCP)
		return key->dport;

	struct ethhdr *eth = data;
	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end)
		return key->dport;
	struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
	if ((void *)(tcp + 1) > data_end)
		return key->dport;

	if (tcp->syn && tcp->ack)
		return key->sport;

	return key->dport;
}

static __always_inline void maybe_capture_tls_clienthello(struct xdp_md *ctx, void *data, void *data_end, const struct flow_key *key)
{
	if (key->proto != IPPROTO_TCP)
		return;

	struct ethhdr *eth = data;
	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end)
		return;
	struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
	if ((void *)(tcp + 1) > data_end)
		return;
	if (tcp->doff < 5)
		return;

	void *payload = (void *)tcp + (tcp->doff * 4);
	if (payload + 6 > data_end)
		return;

	__u8 *p = payload;

	if (p[0] != 0x16 || p[5] != 0x01)
		return;

	struct sni_event *ev = bpf_ringbuf_reserve(&sni_events, sizeof(*ev), 0);
	if (!ev)
		return;

	ev->saddr = key->saddr;
	ev->daddr = key->daddr;
	ev->sport = key->sport;
	ev->dport = key->dport;

	if (payload + 512 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 512);
		ev->payload_len = 512;
	} else if (payload + 256 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 256);
		ev->payload_len = 256;
	} else if (payload + 128 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 128);
		ev->payload_len = 128;
	} else if (payload + 64 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 64);
		ev->payload_len = 64;
	} else if (payload + 32 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 32);
		ev->payload_len = 32;
	} else if (payload + 16 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 16);
		ev->payload_len = 16;
	} else if (payload + 6 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 6);
		ev->payload_len = 6;
	} else {
		bpf_ringbuf_discard(ev, 0);
		return;
	}

	bpf_ringbuf_submit(ev, 0);
}

static __always_inline int looks_like_http_request(__u8 *p, void *data_end)
{
	if ((void *)(p + 4) > data_end)
		return 0;
	if (p[0] == 'G' && p[1] == 'E' && p[2] == 'T' && p[3] == ' ')
		return 1;
	if (p[0] == 'P' && p[1] == 'O' && p[2] == 'S' && p[3] == 'T')
		return 1;
	if (p[0] == 'H' && p[1] == 'E' && p[2] == 'A' && p[3] == 'D')
		return 1;
	if (p[0] == 'P' && p[1] == 'U' && p[2] == 'T' && p[3] == ' ')
		return 1;
	if (p[0] == 'D' && p[1] == 'E' && p[2] == 'L' && p[3] == 'E')
		return 1;
	if (p[0] == 'O' && p[1] == 'P' && p[2] == 'T' && p[3] == 'I')
		return 1;
	if (p[0] == 'P' && p[1] == 'A' && p[2] == 'T' && p[3] == 'C')
		return 1;
	return 0;
}

static __always_inline void maybe_capture_http_host(struct xdp_md *ctx, void *data, void *data_end, const struct flow_key *key)
{
	if (key->proto != IPPROTO_TCP || key->dport != bpf_htons(80))
		return;

	struct ethhdr *eth = data;
	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end)
		return;
	struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
	if ((void *)(tcp + 1) > data_end)
		return;
	if (tcp->doff < 5)
		return;

	void *payload = (void *)tcp + (tcp->doff * 4);
	if (!looks_like_http_request(payload, data_end))
		return;

	struct http_event *ev = bpf_ringbuf_reserve(&http_events, sizeof(*ev), 0);
	if (!ev)
		return;

	ev->saddr = key->saddr;
	ev->daddr = key->daddr;
	ev->sport = key->sport;
	ev->dport = key->dport;

	if (payload + HTTP_CAPTURE_LEN <= data_end) {
		__builtin_memcpy(ev->payload, payload, HTTP_CAPTURE_LEN);
		ev->payload_len = HTTP_CAPTURE_LEN;
	} else if (payload + 256 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 256);
		ev->payload_len = 256;
	} else if (payload + 128 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 128);
		ev->payload_len = 128;
	} else if (payload + 64 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 64);
		ev->payload_len = 64;
	} else if (payload + 32 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 32);
		ev->payload_len = 32;
	} else if (payload + 16 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 16);
		ev->payload_len = 16;
	} else if (payload + 4 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 4);
		ev->payload_len = 4;
	} else {
		bpf_ringbuf_discard(ev, 0);
		return;
	}

	bpf_ringbuf_submit(ev, 0);
}

static __always_inline void maybe_capture_sql_audit_payload(struct xdp_md *ctx, void *data, void *data_end, const struct flow_key *key)
{
	if (key->proto != IPPROTO_TCP)
		return;

	if (!bpf_map_lookup_elem(&sql_audit_flags, key))
		return;

	struct ethhdr *eth = data;
	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end)
		return;
	struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
	if ((void *)(tcp + 1) > data_end)
		return;
	if (tcp->doff < 5)
		return;

	void *payload = (void *)tcp + (tcp->doff * 4);
	if (payload >= data_end)
		return;

	struct sql_audit_event *ev = bpf_ringbuf_reserve(&sql_audit_events, sizeof(*ev), 0);
	if (!ev)
		return;

	ev->saddr = key->saddr;
	ev->daddr = key->daddr;
	ev->sport = key->sport;
	ev->dport = key->dport;

	if (payload + SQL_AUDIT_CAPTURE_LEN <= data_end) {
		__builtin_memcpy(ev->payload, payload, SQL_AUDIT_CAPTURE_LEN);
		ev->payload_len = SQL_AUDIT_CAPTURE_LEN;
	} else if (payload + 256 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 256);
		ev->payload_len = 256;
	} else if (payload + 128 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 128);
		ev->payload_len = 128;
	} else if (payload + 64 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 64);
		ev->payload_len = 64;
	} else if (payload + 32 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 32);
		ev->payload_len = 32;
	} else if (payload + 16 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 16);
		ev->payload_len = 16;
	} else if (payload + 8 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 8);
		ev->payload_len = 8;
	} else if (payload + 1 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 1);
		ev->payload_len = 1;
	} else {
		bpf_ringbuf_discard(ev, 0);
		return;
	}

	ev->truncated = (payload + ev->payload_len < data_end) ? 1 : 0;

	bpf_ringbuf_submit(ev, 0);
}

static __always_inline void maybe_capture_http_auth_payload(struct xdp_md *ctx, void *data, void *data_end, const struct flow_key *key)
{
	if (key->proto != IPPROTO_TCP)
		return;
	if (key->dport != bpf_htons(80) && key->sport != bpf_htons(80))
		return;

	struct ethhdr *eth = data;
	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end)
		return;
	struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
	if ((void *)(tcp + 1) > data_end)
		return;
	if (tcp->doff < 5)
		return;

	void *payload = (void *)tcp + (tcp->doff * 4);
	if (payload >= data_end)
		return;

	if (!bpf_map_lookup_elem(&http_auth_flags, key)) {
		if (key->dport != bpf_htons(80) || !looks_like_http_request(payload, data_end))
			return;

		__u8 flag = 1;
		bpf_map_update_elem(&http_auth_flags, key, &flag, BPF_ANY);

		struct flow_key rev = {.saddr = key->daddr, .daddr = key->saddr, .sport = key->dport, .dport = key->sport, .proto = key->proto};
		bpf_map_update_elem(&http_auth_flags, &rev, &flag, BPF_ANY);
	}

	struct http_auth_event *ev = bpf_ringbuf_reserve(&http_auth_events, sizeof(*ev), 0);
	if (!ev)
		return;

	ev->saddr = key->saddr;
	ev->daddr = key->daddr;
	ev->sport = key->sport;
	ev->dport = key->dport;

	if (payload + HTTP_AUTH_CAPTURE_LEN <= data_end) {
		__builtin_memcpy(ev->payload, payload, HTTP_AUTH_CAPTURE_LEN);
		ev->payload_len = HTTP_AUTH_CAPTURE_LEN;
	} else if (payload + 256 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 256);
		ev->payload_len = 256;
	} else if (payload + 128 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 128);
		ev->payload_len = 128;
	} else if (payload + 64 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 64);
		ev->payload_len = 64;
	} else if (payload + 32 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 32);
		ev->payload_len = 32;
	} else if (payload + 16 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 16);
		ev->payload_len = 16;
	} else if (payload + 4 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 4);
		ev->payload_len = 4;
	} else {
		bpf_ringbuf_discard(ev, 0);
		return;
	}

	ev->truncated = (payload + ev->payload_len < data_end) ? 1 : 0;

	bpf_ringbuf_submit(ev, 0);
}

static __always_inline int maybe_capture_dpi_payload(struct xdp_md *ctx, void *data, void *data_end, const struct flow_key *key)
{
	struct ethhdr *eth = data;
	struct iphdr *ip = (void *)(eth + 1);
	if ((void *)(ip + 1) > data_end)
		return 0;

	void *l4 = (void *)ip + (ip->ihl * 4);
	void *payload;

	if (key->proto == IPPROTO_TCP) {
		struct tcphdr *tcp = l4;
		if ((void *)(tcp + 1) > data_end)
			return 0;
		if (tcp->doff < 5)
			return 0;
		payload = (void *)tcp + (tcp->doff * 4);
	} else if (key->proto == IPPROTO_UDP) {
		struct udphdr *udp = l4;
		if ((void *)(udp + 1) > data_end)
			return 0;
		payload = (void *)udp + sizeof(struct udphdr);
	} else {
		return 0;
	}

	if (payload >= data_end)
		return 0;

	struct dpi_event *ev = bpf_ringbuf_reserve(&dpi_events, sizeof(*ev), 0);
	if (!ev)
		return 0;

	ev->saddr = key->saddr;
	ev->daddr = key->daddr;
	ev->sport = key->sport;
	ev->dport = key->dport;
	ev->proto = key->proto;

	if (payload + DPI_CAPTURE_LEN <= data_end) {
		__builtin_memcpy(ev->payload, payload, DPI_CAPTURE_LEN);
		ev->payload_len = DPI_CAPTURE_LEN;
	} else if (payload + 128 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 128);
		ev->payload_len = 128;
	} else if (payload + 64 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 64);
		ev->payload_len = 64;
	} else if (payload + 32 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 32);
		ev->payload_len = 32;
	} else if (payload + 16 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 16);
		ev->payload_len = 16;
	} else if (payload + 8 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 8);
		ev->payload_len = 8;
	} else if (payload + 1 <= data_end) {
		__builtin_memcpy(ev->payload, payload, 1);
		ev->payload_len = 1;
	} else {
		bpf_ringbuf_discard(ev, 0);
		return 0;
	}

	bpf_ringbuf_submit(ev, 0);
	return 1;
}

SEC("xdp")
int xdp_flow_count(struct xdp_md *ctx)
{
	void *data = (void *)(long)ctx->data;
	void *data_end = (void *)(long)ctx->data_end;

	struct flow_key key = {};
	if (parse_flow(data, data_end, &key) < 0)
		return XDP_PASS;

	__u64 pkt_len = (__u64)(data_end - data);

	struct flow_stats *stats = bpf_map_lookup_elem(&flow_stats_map, &key);
	if (stats) {
		__sync_fetch_and_add(&stats->packets, 1);
		__sync_fetch_and_add(&stats->bytes, pkt_len);
		if (stats->dpi_capture_count < DPI_MAX_CAPTURES_PER_FLOW) {
			if (maybe_capture_dpi_payload(ctx, data, data_end, &key))
				stats->dpi_capture_count++;
		}
	} else {
		struct flow_stats init = {.packets = 1, .bytes = pkt_len, .svc_port = resolve_svc_port(data, data_end, &key)};
		bpf_map_update_elem(&flow_stats_map, &key, &init, BPF_ANY);
	}

	maybe_capture_tls_clienthello(ctx, data, data_end, &key);
	maybe_capture_http_host(ctx, data, data_end, &key);
	maybe_capture_sql_audit_payload(ctx, data, data_end, &key);
	maybe_capture_http_auth_payload(ctx, data, data_end, &key);

	return XDP_PASS;
}

char _license[] SEC("license") = "GPL";
