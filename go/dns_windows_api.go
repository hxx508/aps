package main

import (
	"fmt"
	"net"
	"strings"
	"syscall"
	"unsafe"
)

const (
	gaaFlagIncludePrefix   = 0x00000010
	gaaFlagIncludeGateways = 0x00000080

	ifTypeEthernetCSMACD   = 6
	ifTypeISO88025Ring     = 9
	ifTypePPP              = 23
	ifTypeSoftwareLoopback = 24
	ifTypeATM              = 37
	ifTypeIEEE80211        = 71
	ifTypeTunnel           = 131
	ifTypeIEEE1394         = 144

	ifOperStatusUp = 1
)

var (
	modiphlpapi              = syscall.NewLazyDLL("iphlpapi.dll")
	procGetAdaptersAddresses = modiphlpapi.NewProc("GetAdaptersAddresses")
)

type socketAddress struct {
	Sockaddr       *syscall.RawSockaddrAny
	SockaddrLength int32
}

type ipAdapterUnicastAddress struct {
	Length             uint32
	Flags              uint32
	Next               *ipAdapterUnicastAddress
	Address            socketAddress
	PrefixOrigin       int32
	SuffixOrigin       int32
	DadState           int32
	ValidLifetime      uint32
	PreferredLifetime  uint32
	LeaseLifetime      uint32
	OnLinkPrefixLength uint8
}

type ipAdapterDnsServerAddress struct {
	Length   uint32
	Reserved uint32
	Next     *ipAdapterDnsServerAddress
	Address  socketAddress
}

type ipAdapterGatewayAddress struct {
	Length   uint32
	Reserved uint32
	Next     *ipAdapterGatewayAddress
	Address  socketAddress
}

type ipAdapterPrefix struct {
	Length       uint32
	Flags        uint32
	Next         *ipAdapterPrefix
	Address      socketAddress
	PrefixLength uint32
}

type ipAdapterAddresses struct {
	Length                uint32
	IfIndex               uint32
	Next                  *ipAdapterAddresses
	AdapterName           *byte
	FirstUnicastAddress   *ipAdapterUnicastAddress
	FirstAnycastAddress   uintptr
	FirstMulticastAddress uintptr
	FirstDnsServerAddress *ipAdapterDnsServerAddress
	DnsSuffix             *uint16
	Description           *uint16
	FriendlyName          *uint16
	PhysicalAddress       [syscall.MAX_ADAPTER_ADDRESS_LENGTH]byte
	PhysicalAddressLength uint32
	Flags                 uint32
	Mtu                   uint32
	IfType                uint32
	OperStatus            uint32
	Ipv6IfIndex           uint32
	ZoneIndices           [16]uint32
	FirstPrefix           *ipAdapterPrefix
	TransmitLinkSpeed     uint64
	ReceiveLinkSpeed      uint64
	FirstWinsServer       uintptr
	FirstGatewayAddress   *ipAdapterGatewayAddress
	Ipv4Metric            uint32
	Ipv6Metric            uint32
}

type adapterDNSCandidate struct {
	Name        string
	Description string
	DNS         []string
	Score       int
	Metric      uint32
}

func getDNSByAdapterAPI() string {
	candidate, err := findPreferredAdapterDNS()
	if err != nil {
		logFileOnly(fmt.Sprintf("Windows API 获取 DNS 失败: %v", err))
		return ""
	}
	if candidate == nil || len(candidate.DNS) == 0 {
		logFileOnly("Windows API 未找到可用网卡 DNS")
		return ""
	}

	dns := strings.Join(candidate.DNS, ",")
	logFileOnly(fmt.Sprintf("通过 Windows API 选中网卡: %s (%s), metric=%d, DNS: %s", candidate.Name, candidate.Description, candidate.Metric, dns))
	return dns
}

func findPreferredAdapterDNS() (*adapterDNSCandidate, error) {
	adapters, err := getAdaptersAddresses()
	if err != nil {
		return nil, err
	}

	var best *adapterDNSCandidate
	for _, aa := range adapters {
		candidate := evaluateAdapterDNSCandidate(aa)
		if candidate == nil {
			continue
		}
		if best == nil || candidate.Score > best.Score || (candidate.Score == best.Score && candidate.Metric < best.Metric) {
			best = candidate
		}
	}

	return best, nil
}

func evaluateAdapterDNSCandidate(aa *ipAdapterAddresses) *adapterDNSCandidate {
	dnsList := dnsServersFromAdapter(aa)
	if len(dnsList) == 0 {
		return nil
	}

	name := utf16PtrToString(aa.FriendlyName)
	description := utf16PtrToString(aa.Description)
	virtual := isProbablyVirtualAdapter(name, description, aa.IfType)
	hasGateway := hasUsableGateway(aa)
	hasIPv4, hasIPv6 := hasUsableUnicastAddress(aa)
	metric := preferredAdapterMetric(aa)

	score := 0
	if aa.OperStatus == ifOperStatusUp {
		score += 200
	} else {
		score -= 200
	}
	if hasGateway {
		score += 240
	}
	if hasIPv4 {
		score += 120
	}
	if hasIPv6 {
		score += 50
	}
	if isPreferredPhysicalType(aa.IfType) {
		score += 80
	}
	if aa.PhysicalAddressLength > 0 {
		score += 20
	}
	if virtual {
		score -= 220
	}
	if metric < ^uint32(0) {
		score -= int(minUint32(metric, 500)) / 10
	}

	if name == "" {
		name = description
	}

	return &adapterDNSCandidate{
		Name:        name,
		Description: description,
		DNS:         dnsList,
		Score:       score,
		Metric:      metric,
	}
}

func getAdaptersAddresses() ([]*ipAdapterAddresses, error) {
	size := uint32(15000)

	for {
		buffer := make([]byte, size)
		r0, _, _ := procGetAdaptersAddresses.Call(
			uintptr(syscall.AF_UNSPEC),
			uintptr(gaaFlagIncludePrefix|gaaFlagIncludeGateways),
			0,
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(unsafe.Pointer(&size)),
		)

		if r0 == 0 {
			if size == 0 {
				return nil, nil
			}

			var adapters []*ipAdapterAddresses
			for aa := (*ipAdapterAddresses)(unsafe.Pointer(&buffer[0])); aa != nil; aa = aa.Next {
				adapters = append(adapters, aa)
			}
			return adapters, nil
		}

		err := syscall.Errno(r0)
		if err != syscall.ERROR_BUFFER_OVERFLOW {
			return nil, err
		}
		if size <= uint32(len(buffer)) {
			return nil, err
		}
	}
}

func dnsServersFromAdapter(aa *ipAdapterAddresses) []string {
	seen := make(map[string]bool)
	var ipv4List, ipv6List []string

	for dns := aa.FirstDnsServerAddress; dns != nil; dns = dns.Next {
		ip := socketAddressToIP(dns.Address)
		if ip == nil || isBogusWindowsIPv6DNS(ip) {
			continue
		}

		value := ip.String()
		if seen[value] {
			continue
		}
		seen[value] = true

		if ip.To4() != nil {
			ipv4List = append(ipv4List, value)
		} else {
			ipv6List = append(ipv6List, value)
		}
	}

	return append(ipv4List, ipv6List...)
}

func hasUsableUnicastAddress(aa *ipAdapterAddresses) (bool, bool) {
	var hasIPv4, hasIPv6 bool

	for address := aa.FirstUnicastAddress; address != nil; address = address.Next {
		ip := socketAddressToIP(address.Address)
		if !isUsableUnicastIP(ip) {
			continue
		}

		if ip.To4() != nil {
			hasIPv4 = true
		} else {
			hasIPv6 = true
		}
	}

	return hasIPv4, hasIPv6
}

func hasUsableGateway(aa *ipAdapterAddresses) bool {
	for gateway := aa.FirstGatewayAddress; gateway != nil; gateway = gateway.Next {
		ip := socketAddressToIP(gateway.Address)
		if ip != nil && !ip.IsUnspecified() {
			return true
		}
	}
	return false
}

func socketAddressToIP(address socketAddress) net.IP {
	if address.Sockaddr == nil {
		return nil
	}

	sa, err := address.Sockaddr.Sockaddr()
	if err != nil {
		return nil
	}

	switch sa := sa.(type) {
	case *syscall.SockaddrInet4:
		return net.IPv4(sa.Addr[0], sa.Addr[1], sa.Addr[2], sa.Addr[3])
	case *syscall.SockaddrInet6:
		ip := make(net.IP, net.IPv6len)
		copy(ip, sa.Addr[:])
		return ip
	default:
		return nil
	}
}

func isUsableUnicastIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() {
		return false
	}

	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return false
		}
		return true
	}

	return !ip.IsLinkLocalUnicast()
}

func preferredAdapterMetric(aa *ipAdapterAddresses) uint32 {
	metric := aa.Ipv4Metric
	if metric == 0 || (aa.Ipv6Metric != 0 && aa.Ipv6Metric < metric) {
		metric = aa.Ipv6Metric
	}
	if metric == 0 {
		return ^uint32(0)
	}
	return metric
}

func isPreferredPhysicalType(ifType uint32) bool {
	switch ifType {
	case ifTypeEthernetCSMACD, ifTypeISO88025Ring, ifTypeATM, ifTypeIEEE80211, ifTypeIEEE1394:
		return true
	default:
		return false
	}
}

func isProbablyVirtualAdapter(name, description string, ifType uint32) bool {
	switch ifType {
	case ifTypeSoftwareLoopback, ifTypeTunnel:
		return true
	}

	text := strings.ToLower(name + " " + description)
	keywords := []string{
		"vmware", "virtualbox", "hyper-v", "vethernet", "virtual",
		"loopback", "pseudo", "teredo", "isatap", "tunnel",
		"tap", "vpn", "wan miniport", "miniport", "npcap",
		"wireguard", "tailscale", "zerotier", "docker",
	}

	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	return false
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}

	end := unsafe.Pointer(p)
	count := 0
	for *(*uint16)(end) != 0 {
		end = unsafe.Pointer(uintptr(end) + unsafe.Sizeof(*p))
		count++
	}

	return syscall.UTF16ToString(unsafe.Slice(p, count))
}

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
