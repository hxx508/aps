package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// 获取 Windows 主版本号 (6=Win7, 10=Win10/11)
func getWindowsMajorVersion() int {
	cmd := exec.Command("cmd", "/c", "ver")
	out, err := cmd.Output()
	if err != nil {
		return 10
	}
	line := strings.TrimSpace(string(out))
	logMsg("系统版本信息: " + line)

	// 尝试多种模式匹配版本号
	// Win7 中文版输出类似: Microsoft Windows [°汾 6.1.7601] 或 [Version 6.1.7601]
	patterns := []string{
		"Version ",
		"版本 ",
		"[",
	}

	verStr := ""
	for _, prefix := range patterns {
		start := strings.Index(line, prefix)
		if start != -1 {
			verStr = line[start:]
			break
		}
	}

	// 从 verStr 中提取版本号 (如 "6.1.7601" 或 "10.0.26100")
	if verStr == "" {
		return 10
	}

	// 去掉 "[" 和可能的乱码前缀，找到数字开始的位置
	verStr = strings.TrimLeft(verStr, "[ ")
	// 找到第一个数字的位置
	digitStart := -1
	for i, c := range verStr {
		if c >= '0' && c <= '9' {
			digitStart = i
			break
		}
	}
	if digitStart == -1 {
		return 10
	}
	verStr = verStr[digitStart:]

	// 去掉可能的 "]"
	verStr = strings.TrimRight(verStr, "]")
	parts := strings.SplitN(strings.TrimSpace(verStr), ".", 2)
	if len(parts) == 0 {
		return 10
	}
	major, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 10
	}
	return major
}

// 判断是否 64 位系统
func is64BitOS() bool {
	_, err := os.Stat(`C:\Windows\SysWOW64`)
	return err == nil
}

// 查注册表值，返回是否存在
func regKeyExists(keyPath string) bool {
	cmd := exec.Command("reg", "query", keyPath)
	err := cmd.Run()
	return err == nil
}

// 检查 VC++ 运行库是否已安装（支持 VC++ 2012-2022）
// VC++ 2015-2022 使用 VisualStudio 14.0
func isVCRedistInstalled() bool {
	// VC++ 2012 (11.0) 和 VC++ 2015-2022 (14.0) 的注册表路径
	paths := []string{
		`HKLM\SOFTWARE\Microsoft\VisualStudio\11.0\VC\Runtimes`,
		`HKLM\SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\VisualStudio\11.0\VC\Runtimes`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\VisualStudio\14.0\VC\Runtimes`,
	}
	for _, path := range paths {
		if is64BitOS() {
			if regKeyExists(path + `\x64`) || regKeyExists(path + `\x86`) {
				return true
			}
		} else {
			if regKeyExists(path + `\x86`) {
				return true
			}
		}
	}
	return false
}

// 检查 Npcap 是否已安装
func isNpcapInstalled() bool {
	return regKeyExists(`HKLM\SOFTWARE\Npcap`) ||
		regKeyExists(`HKLM\SOFTWARE\WOW6432Node\Npcap`)
}

// 检查 WinPcap 是否已安装
func isWinPcapInstalled() bool {
	return regKeyExists(`HKLM\SOFTWARE\WinPcap`) ||
		regKeyExists(`HKLM\SOFTWARE\WOW6432Node\WinPcap`)
}

// 检查 dig 是否已安装
func isDigInstalled() bool {
	_, err := os.Stat(`C:\Windows\System32\dig.exe`)
	return err == nil
}

// 检查 KB2533623 是否已安装
func isKBInstalled() bool {
	cmd := exec.Command("wmic", "qfe", "get", "HotFixID")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "KB2533623")
}

// 运行安装程序并等待完成（通过 PowerShell 以管理员权限启动）
func runInstaller(path string, args ...string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("安装文件不存在: %s", path)
	}
	// 构造 PowerShell 参数字符串
	var psCmd string
	if len(args) > 0 {
		argStr := ""
		for _, a := range args {
			argStr += fmt.Sprintf(" '%s'", strings.ReplaceAll(a, "'", "''"))
		}
		psCmd = fmt.Sprintf("Start-Process -FilePath '%s' -ArgumentList %s -Verb RunAs -Wait",
			strings.ReplaceAll(path, "'", "''"), argStr)
	} else {
		psCmd = fmt.Sprintf("Start-Process -FilePath '%s' -Verb RunAs -Wait",
			strings.ReplaceAll(path, "'", "''"))
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// 使用 PowerShell Start-Process 以管理员权限运行安装程序
func runInstallerCmd(path string, args ...string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("安装文件不存在: %s", path)
	}
	var psCmd string
	if len(args) > 0 {
		psCmd = fmt.Sprintf(`Start-Process -FilePath '%s' -ArgumentList "%s" -Verb RunAs -Wait`,
			path, strings.Join(args, " "))
	} else {
		psCmd = fmt.Sprintf(`Start-Process -FilePath '%s' -Verb RunAs -Wait`, path)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// 安装 VC++ 运行库（静默安装，已安装则跳过），失败返回 error
func installVCRedist(toolsDir string) error {
	if isVCRedistInstalled() {
		logMsg("[跳过] VC++ 运行库已安装")
		return nil
	}
	logMsg("正在安装 VC++ 运行库...")
	var vcPath string
	if is64BitOS() {
		vcPath = filepath.Join(toolsDir, "vcredist_x64.exe")
	} else {
		vcPath = filepath.Join(toolsDir, "vcredist_x86.exe")
	}
	// /quiet /norestart 静默安装
	if err := runInstallerCmd(vcPath, "/quiet", "/norestart"); err != nil {
		logMsg(fmt.Sprintf("[错误] VC++ 安装失败: %v", err))
		return err
	}
	logMsg("[OK] VC++ 安装完成")
	return nil
}

// 安装 pcap 驱动（静默安装，已安装则跳过）
func installPcap(toolsDir string, isWin7 bool) {
	if isWin7 {
		if isWinPcapInstalled() {
			logMsg("[跳过] WinPcap 已安装")
			return
		}
		logMsg("正在安装 WinPcap...")
		pcapPath := filepath.Join(toolsDir, "WinPcap_4_1_3.exe")
		// WinPcap 静默安装参数
		if err := runInstaller(pcapPath, "/S"); err != nil {
			logMsg(fmt.Sprintf("[警告] WinPcap 安装失败: %v", err))
		} else {
			logMsg("[OK] WinPcap 安装完成")
		}
	} else {
		if isNpcapInstalled() {
			logMsg("[跳过] Npcap 已安装")
			return
		}
		logMsg("正在安装 Npcap...")
		pcapPath := filepath.Join(toolsDir, "npcap-1.87.exe")
		// Npcap 标准版不支持静默安装，需手动操作
		if err := runInstaller(pcapPath); err != nil {
			logMsg(fmt.Sprintf("[警告] Npcap 安装失败: %v", err))
		} else {
			logMsg("[OK] Npcap 安装完成")
		}
	}
}

// 复制 dig 到 System32（已存在则跳过）
func installDig(toolsDir string) {
	if isDigInstalled() {
		logMsg("[跳过] dig 已安装")
		return
	}
	logMsg("正在安装 dig 工具...")
	digSrc := filepath.Join(toolsDir, "dig")
	if _, err := os.Stat(digSrc); os.IsNotExist(err) {
		logMsg("[错误] 未找到 tools\\dig 目录")
		return
	}
	exec.Command("taskkill", "/f", "/im", "dig.exe").Run()
	cmd := exec.Command("cmd", "/c", "xcopy",
		digSrc+`\*`, `C:\Windows\System32\`, "/Y", "/E", "/I")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logMsg(fmt.Sprintf("[错误] dig 复制失败: %v", err))
	} else {
		logMsg("[OK] dig 安装完成")
	}
}

// 重命名 aps_client_detect 目录
func installClientDetect(baseDir string, isWin7 bool) {
	var srcName string
	if isWin7 {
		srcName = "aps_client_detect_win7"
	} else {
		srcName = "aps_client_detect_win10"
	}
	src := filepath.Join(baseDir, srcName)
	dst := filepath.Join(baseDir, "aps_client_detect")

	if _, err := os.Stat(src); os.IsNotExist(err) {
		logMsg(fmt.Sprintf("[跳过] 未找到 %s", srcName))
		return
	}
	logMsg("正在安装探头检测工具...")
	if _, err := os.Stat(dst); err == nil {
		os.RemoveAll(dst)
	}
	if err := os.Rename(src, dst); err != nil {
		logMsg(fmt.Sprintf("[错误] 重命名失败: %v", err))
	} else {
		logMsg("[OK] 探头检测工具安装完成")
	}
}

// Win7 额外安装 KB2533623 补丁（已安装则跳过）
func installWin7Patch(toolsDir string) {
	if isKBInstalled() {
		logMsg("[跳过] KB2533623 已安装")
		return
	}
	patchPath := filepath.Join(toolsDir, "Windows6.1-KB2533623-x64.msu")
	if _, err := os.Stat(patchPath); os.IsNotExist(err) {
		logMsg("[警告] 未找到 KB2533623 补丁文件，跳过")
		return
	}
	logMsg("正在安装 Win7 补丁 KB2533623...")
	cmd := exec.Command("wusa.exe", patchPath, "/quiet", "/norestart")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logMsg(fmt.Sprintf("[警告] KB2533623 安装失败: %v", err))
	} else {
		logMsg("[OK] KB2533623 安装完成")
	}
}

// 主入口：安装环境
func installEnvironment() {
	logMsg("=== 开始安装运行环境 ===")

	toolsDir := filepath.Join(WorkDir, "tools")
	isWin7 := getWindowsMajorVersion() == 6

	if isWin7 {
		logMsg("检测到 Windows 7 系统")
	} else {
		logMsg("检测到 Windows 10/11 系统")
	}

	if err := installVCRedist(toolsDir); err != nil {
		logMsg("=== 安装中止：VC++ 安装失败 ===")
		return
	}
	installPcap(toolsDir, isWin7)
	installDig(toolsDir)
	installClientDetect(WorkDir, isWin7)
	if isWin7 {
		installWin7Patch(toolsDir)
	}

	logMsg("=== 运行环境安装完成 ===")
}
