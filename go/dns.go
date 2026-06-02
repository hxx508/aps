package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

const resolvConfPath = `C:\Windows\System32\drivers\etc\resolv.conf`

// 获取本地 DNS，优先使用 Windows API 选择正在联网的真实网卡，再降级到命令行方案
func getLocalDNS() string {
	if dns := getDNSByAdapterAPI(); dns != "" {
		return dns
	}
	if dns := getDNSByPowerShellWMI(); dns != "" {
		logFileOnly("通过 PowerShell WMI 获取 DNS 成功: " + dns)
		return dns
	}
	if dns := getDNSByWmic(); dns != "" {
		logFileOnly("通过 wmic 获取 DNS 成功: " + dns)
		return dns
	}
	if dns := getDNSByNetsh(); dns != "" {
		logFileOnly("通过 netsh 获取 DNS 成功: " + dns)
		return dns
	}
	logFileOnly("所有 DNS 获取方式均失败")
	return "获取失败"
}

func getDNSByPowerShellWMI() string {
	script := `$items = @(Get-WmiObject Win32_NetworkAdapterConfiguration -Filter "IPEnabled=TRUE" | Where-Object { $_.DNSServerSearchOrder } | ForEach-Object { $_.DNSServerSearchOrder }); if ($items.Count -gt 0) { $items -join "," }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		logFileOnly(fmt.Sprintf("PowerShell WMI 获取 DNS 失败: %v", err))
		return ""
	}
	return collectUniqueIPs(decodeWindowsCommandOutput(out))
}

func getDNSByWmic() string {
	cmd := exec.Command("wmic", "nicconfig", "where", "IPEnabled=True", "get", "DNSServerSearchOrder", "/Value")
	out, err := cmd.Output()
	if err != nil {
		logFileOnly(fmt.Sprintf("wmic 获取 DNS 失败: %v", err))
		return ""
	}
	text := decodeWindowsCommandOutput(out)

	seen := make(map[string]bool)
	var result []string

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if !strings.HasPrefix(line, "DNSServerSearchOrder=") {
			continue
		}
		value := line[len("DNSServerSearchOrder="):]
		for _, ip := range splitDNSCandidates(value) {
			if !seen[ip] {
				seen[ip] = true
				result = append(result, ip)
			}
		}
	}
	if len(result) == 0 {
		logFileOnly("wmic 未解析到 DNS")
		return ""
	}
	return strings.Join(result, ",")
}

func getDNSByNetsh() string {
	var texts []string
	commands := [][]string{
		{"interface", "ipv4", "show", "dnsservers"},
		{"interface", "ipv6", "show", "dnsservers"},
	}

	for _, args := range commands {
		cmd := exec.Command("netsh", args...)
		out, err := cmd.Output()
		if err != nil {
			logFileOnly(fmt.Sprintf("netsh %s 获取 DNS 失败: %v", strings.Join(args, " "), err))
			continue
		}
		texts = append(texts, decodeWindowsCommandOutput(out))
	}

	return collectUniqueIPs(strings.Join(texts, "\n"))
}

func collectUniqueIPs(text string) string {
	seen := make(map[string]bool)
	var ipv4List, ipv6List []string

	addIP := func(ip string) {
		ip = normalizeIPCandidate(ip)
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		if strings.Contains(ip, ":") {
			ipv6List = append(ipv6List, ip)
			return
		}
		ipv4List = append(ipv4List, ip)
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		for _, ip := range extractIPCandidates(line) {
			addIP(ip)
		}
	}

	all := append(ipv4List, ipv6List...)
	if len(all) == 0 {
		logFileOnly("未从命令输出中解析到 DNS")
		return ""
	}
	return strings.Join(all, ",")
}

func splitDNSCandidates(value string) []string {
	replacer := strings.NewReplacer("{", "", "}", "", "\"", "", ";", ",")
	value = replacer.Replace(value)

	seen := make(map[string]bool)
	var result []string
	for _, part := range strings.Split(value, ",") {
		ip := normalizeIPCandidate(strings.TrimSpace(part))
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		result = append(result, ip)
	}
	return result
}

func extractIPCandidates(line string) []string {
	var fields []string
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		fields = append(fields, current.String())
		current.Reset()
	}

	for _, r := range line {
		if isIPTokenRune(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	seen := make(map[string]bool)
	var result []string
	for _, field := range fields {
		ip := normalizeIPCandidate(field)
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		result = append(result, ip)
	}
	return result
}

func normalizeIPCandidate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.IndexByte(s, '%'); idx >= 0 {
		s = s[:idx]
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	if isBogusWindowsIPv6DNS(ip) {
		return ""
	}
	return ip.String()
}

func isIPTokenRune(r rune) bool {
	switch {
	case r >= '0' && r <= '9':
		return true
	case r >= 'a' && r <= 'f':
		return true
	case r >= 'A' && r <= 'F':
		return true
	case r == '.' || r == ':' || r == '%':
		return true
	default:
		return false
	}
}

func decodeWindowsCommandOutput(out []byte) string {
	if len(out) == 0 {
		return ""
	}
	if text, ok := decodeUTF16LE(out); ok {
		return text
	}
	return string(out)
}

func decodeUTF16LE(out []byte) (string, bool) {
	if len(out) < 2 {
		return "", false
	}

	if out[0] == 0xFF && out[1] == 0xFE {
		out = out[2:]
	} else if !looksLikeUTF16LE(out) {
		return "", false
	}

	if len(out)%2 != 0 {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "", false
	}

	u16 := make([]uint16, 0, len(out)/2)
	for i := 0; i+1 < len(out); i += 2 {
		u16 = append(u16, binary.LittleEndian.Uint16(out[i:i+2]))
	}
	return string(utf16.Decode(u16)), true
}

func looksLikeUTF16LE(out []byte) bool {
	sample := len(out)
	if sample > 128 {
		sample = 128
	}
	if sample < 4 {
		return false
	}

	zeroBytes := 0
	for i := 1; i < sample; i += 2 {
		if out[i] == 0 {
			zeroBytes++
		}
	}

	return zeroBytes >= sample/4
}

func isBogusWindowsIPv6DNS(ip net.IP) bool {
	ip = ip.To16()
	if ip == nil || ip.To4() != nil {
		return false
	}
	return ip[0] == 0xfe && ip[1] == 0xc0
}

// writeDNSToAgentInfo 将 dns 写入 agent_info 文件
func writeDNSToAgentInfo(dns string) {
	info := readAgentInfo()
	info["dns"] = dns
	if err := writeAgentInfo(info); err != nil {
		logMsg(fmt.Sprintf("写入 DNS 到 agent_info 失败: %v", err))
		return
	}
	if err := writeResolvConf(dns); err != nil {
		logMsg(fmt.Sprintf("写入 resolv.conf 失败: %v", err))
	}
}

func writeResolvConf(dns string) error {
	servers := splitDNSCandidates(dns)
	if len(servers) == 0 {
		return fmt.Errorf("未解析到可写入 resolv.conf 的 DNS")
	}

	var sb strings.Builder
	for _, server := range servers {
		sb.WriteString("nameserver ")
		sb.WriteString(server)
		sb.WriteString("\n")
	}

	if err := os.MkdirAll(filepath.Dir(resolvConfPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(resolvConfPath, []byte(sb.String()), 0644)
}

// checkDNSInAgentInfo 检查 agent_info 中是否已有 dns
func checkDNSInAgentInfo() (string, bool) {
	info := readAgentInfo()
	dns := info["dns"]
	return dns, dns != ""
}

func handleDNSSelection() string {
	// 每次启动都获取当前 DNS
	rawDNS := getLocalDNS()
	if rawDNS == "获取失败" || rawDNS == "未找到DNS" {
		return rawDNS
	}

	// 和 agent_info 里存的比对
	storedDNS, hasStored := checkDNSInAgentInfo()
	if hasStored && storedDNS == rawDNS {
		fmt.Printf("当前使用的 DNS 是 %s (无变化)\n", storedDNS)
		return storedDNS
	}

	// 不一样或没有，解析并让用户选择
	dnsList := strings.Split(rawDNS, ",")
	var ipv4List, ipv6List []string
	for _, ip := range dnsList {
		if strings.Contains(ip, ":") {
			ipv6List = append(ipv6List, ip)
		} else {
			ipv4List = append(ipv4List, ip)
		}
	}

	if len(ipv4List) > 0 && len(ipv6List) > 0 {
		fmt.Println("检测到多种类型的 DNS：")
		fmt.Printf("IPv4 DNS: %s\n", strings.Join(ipv4List, ", "))
		fmt.Printf("IPv6 DNS: %s\n", strings.Join(ipv6List, ", "))
		var selectedDNS string

		for {
			fmt.Print("请选择要使用的 DNS 类型 (输入 4 或 6): ")

			var choice string
			fmt.Scanln(&choice)

			if choice == "4" {
				selectedDNS = strings.Join(ipv4List, ",")
				break
			}
			if choice == "6" {
				selectedDNS = strings.Join(ipv6List, ",")
				break
			}

			fmt.Println("输入无效，请重新输入 4 或 6")
		}
		writeDNSToAgentInfo(selectedDNS)
		return selectedDNS
	}

	writeDNSToAgentInfo(rawDNS)
	return rawDNS
}

// 确保 agent_info 目录存在
func ensureAgentInfoDir() {
	os.MkdirAll(WorkDir, 0755)
}
