package main

import (
	"net"
	"regexp"
	"strings"

	"github.com/oschwald/geoip2-golang"
)

func loadGeoDB(path string) (*geoip2.Reader, error) {
	if path == "" {
		return nil, nil
	}
	return geoip2.Open(path)
}

func loadASNDB(path string) (*geoip2.Reader, error) {
	if path == "" {
		return nil, nil
	}
	return geoip2.Open(path)
}

func isPrivateOrSpecialIP(ip net.IP) bool {
	return ip.IsPrivate() ||
		ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

type GeoPoint struct {
	IP      string  `json:"ip"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Country string  `json:"country"`
	Org     string  `json:"org,omitempty"`
	Packets uint64  `json:"packets"`
	Bytes   uint64  `json:"bytes"`
}

type GeoReport struct {
	Enabled            bool       `json:"enabled"`
	Window             string     `json:"window"`
	Points             []GeoPoint `json:"points"`
	ExcludedPrivate    int        `json:"excludedPrivate"`
	ExcludedUnresolved int        `json:"excludedUnresolved"`
}

func buildGeoReport(db *geoip2.Reader, asnDB *geoip2.Reader, candidates []IPStat) GeoReport {

	report := GeoReport{Enabled: db != nil, Points: []GeoPoint{}}
	if db == nil {
		return report
	}

	for _, s := range candidates {
		ip := net.ParseIP(s.IP)
		if ip == nil {
			report.ExcludedUnresolved++
			continue
		}
		if isPrivateOrSpecialIP(ip) {
			report.ExcludedPrivate++
			continue
		}
		record, err := db.City(ip)
		if err != nil || record.Country.IsoCode == "" {
			report.ExcludedUnresolved++
			continue
		}
		report.Points = append(report.Points, GeoPoint{
			IP:      s.IP,
			Lat:     record.Location.Latitude,
			Lng:     record.Location.Longitude,
			Country: normalizeCountry(record.Country.IsoCode),
			Org:     resolveOrg(asnDB, s.IP),
			Packets: s.Packets,
			Bytes:   s.Bytes,
		})
	}
	return report
}

func normalizeCountry(isoCode string) string {
	switch isoCode {
	case "HK", "MO", "TW":
		return "CN"
	default:
		return isoCode
	}
}

func resolveCountry(db *geoip2.Reader, ipStr string) string {
	if db == nil {
		return ""
	}
	ip := net.ParseIP(ipStr)
	if ip == nil || isPrivateOrSpecialIP(ip) {
		return ""
	}
	record, err := db.City(ip)
	if err != nil || record.Country.IsoCode == "" {
		return ""
	}
	return normalizeCountry(record.Country.IsoCode)
}

var orgSuffixRe = regexp.MustCompile(`(?i)[,]?\s*(inc|llc|l\.l\.c|ltd|co|corp|corporation|gmbh|plc|s\.a|s\.p\.a|ag|kg|bv|nv)\.?\s*$`)

func stripOrgSuffix(org string) string {
	trimmed := strings.TrimSpace(org)
	for {
		next := strings.TrimSpace(orgSuffixRe.ReplaceAllString(trimmed, ""))
		next = strings.TrimSpace(strings.TrimRight(next, ","))
		if next == trimmed || next == "" {
			break
		}
		trimmed = next
	}
	if trimmed == "" {
		return strings.TrimSpace(org)
	}
	return trimmed
}

func resolveOrg(asnDB *geoip2.Reader, ipStr string) string {
	if asnDB == nil {
		return ""
	}
	ip := net.ParseIP(ipStr)
	if ip == nil || isPrivateOrSpecialIP(ip) {
		return ""
	}
	record, err := asnDB.ASN(ip)
	if err != nil || record.AutonomousSystemOrganization == "" {
		return ""
	}
	return stripOrgSuffix(record.AutonomousSystemOrganization)
}

func annotateOrgs(asnDB *geoip2.Reader, report *Report) {
	if asnDB == nil {
		return
	}
	for i := range report.TopIPs {
		report.TopIPs[i].Org = resolveOrg(asnDB, report.TopIPs[i].IP)
	}
}

func annotateCountries(db *geoip2.Reader, report *Report) {
	if db == nil {
		return
	}
	for i := range report.TopIPs {
		report.TopIPs[i].Country = resolveCountry(db, report.TopIPs[i].IP)
	}
	annotateCountriesFlows(db, report.TopFlows)
}

func annotateCountriesFlows(db *geoip2.Reader, flows []FlowStat) {
	if db == nil {
		return
	}
	for i := range flows {
		flows[i].SrcCountry = resolveCountry(db, flows[i].SrcIP)
		flows[i].DstCountry = resolveCountry(db, flows[i].DstIP)
	}
}
