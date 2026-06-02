package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// 从 agent_info 读取所有 key=value
func readAgentInfo() map[string]string {
	result := make(map[string]string)
	data, err := os.ReadFile(AgentInfoFile)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

// 将 map 写回 agent_info，保留所有 key
func writeAgentInfo(info map[string]string) error {
	// 保持固定顺序：probe_id, probe_name, dns，其余追加
	order := []string{"probe_id", "probe_name", "dns"}
	written := make(map[string]bool)

	var sb strings.Builder
	for _, k := range order {
		if v, ok := info[k]; ok {
			sb.WriteString(k + "=" + v + "\n")
			written[k] = true
		}
	}
	for k, v := range info {
		if !written[k] {
			sb.WriteString(k + "=" + v + "\n")
		}
	}
	return os.WriteFile(AgentInfoFile, []byte(sb.String()), 0644)
}

// 生成 UUID v4
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// 降级：用随机数拼接
		return fmt.Sprintf("%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10])
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// 确保 probe_id 和 probe_name 存在，返回 (probeId, probeName)
func ensureProbeInfo() (string, string) {
	info := readAgentInfo()

	// probe_id: 不存在或为空则生成
	probeID := info["probe_id"]
	if probeID == "" {
		probeID = generateUUID()
		logMsg(fmt.Sprintf("生成新的 probe_id: %s", probeID))
	}
	info["probe_id"] = probeID

	// probe_name: 不存在或为空则交互输入
	probeName := info["probe_name"]
	if probeName == "" {
		probeName = promptProbeName()
		info["probe_name"] = probeName
	}

	// 写回文件
	if err := writeAgentInfo(info); err != nil {
		logMsg(fmt.Sprintf("写入 agent_info 失败: %v", err))
	}

	return probeID, probeName
}

// 交互式输入 probe_name，不允许为空
func promptProbeName() string {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("请输入探针名称 (probe_name，不可为空): ")
		name, _ := reader.ReadString('\n')
		name = strings.TrimSpace(strings.ReplaceAll(name, "\r", ""))
		if name != "" {
			return name
		}
		fmt.Println("探针名称不能为空，请重新输入。")
	}
}

// 发送注册请求，失败时如果是名称重复则让用户重新输入，其他失败返回 error
func registerProbe(probeID, probeName, dns string) error {
	for {
		logMsg(fmt.Sprintf("正在注册探针 (probe_name=%s)...", probeName))

		payload := map[string]string{
			"probeId":   probeID,
			"probeName": probeName,
			"dnsInfo":   dns,
			"probeType": "1",
		}
		jsonData, _ := json.Marshal(payload)

		resp, err := http.Post(RegisterUrl, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("注册请求失败: %v", err)
		}

		body, _ := readBody(resp)
		resp.Body.Close()
		logMsg(fmt.Sprintf("注册响应 (HTTP %d): %s", resp.StatusCode, body))

		if isRegisterSuccess(body) {
			logMsg("探针注册成功")
			return nil
		}

		// 名称重复：让用户重新输入，不算失败
		if strings.Contains(body, "already exists") || strings.Contains(body, "已存在") {
			fmt.Println("探针名称已存在，请输入新的名称。")
			probeName = promptProbeName()
			info := readAgentInfo()
			info["probe_name"] = probeName
			writeAgentInfo(info)
			continue
		}

		// 其他失败：返回 error，由调用方决定是否中止
		return fmt.Errorf("注册失败，响应: %s", body)
	}
}

func readBody(resp *http.Response) (string, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	return buf.String(), err
}

func isRegisterSuccess(body string) bool {
	// 兼容 {"code":200} 和 {"code":"200"}
	return strings.Contains(body, `"code":200`) ||
		strings.Contains(body, `"code":"200"`)
}
