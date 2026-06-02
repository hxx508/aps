package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// 服务器地址
	BaseUrl     = "http://autotest.c-dns.com:9091/software/"
	VersionUrl  = "http://autotest.c-dns.com:9091/software/version.txt"
	RegisterUrl = "http://autotest.c-dns.com:9090/center/api/probe/register"
	//BaseUrl     = "http://172.171.2.141:8888/software/"
	//VersionUrl  = "http://172.171.2.141:8888/software/version.txt"
	//RegisterUrl = "http://172.171.2.141:28080/center/api/probe/register"

	// Tomcat
	TomcatPort = "8080"
)

// 运行时路径，基于 exe 所在目录
var (
	WorkDir          string
	LogFile          string
	LocalVersionFile string
	AgentInfoFile    string
)

func daemonPidFile() string {
	return filepath.Join(WorkDir, "daemon.pid")
}

// 杀掉已有 daemon 进程
func killExistingDaemon() {
	pidFile := daemonPidFile()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid := strings.TrimSpace(string(data))
	if pid == "" {
		return
	}
	exec.Command("taskkill", "/F", "/PID", pid).Run()
	os.Remove(pidFile)
	logMsg(fmt.Sprintf("已终止旧 daemon 进程 (PID %s)", pid))
}

func startDaemon() {
	// 先杀掉已有 daemon
	killExistingDaemon()

	exePath, _ := os.Executable()
	cmd := exec.Command(exePath, "--daemon", "--workdir="+WorkDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008}
	if err := cmd.Start(); err == nil {
		// 写入新 daemon 的 pid
		os.WriteFile(daemonPidFile(), []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644)
		logMsg(fmt.Sprintf("后台 daemon 已启动 (PID %d)", cmd.Process.Pid))
	}
}

func runDaemon() {
	defer func() {
		if r := recover(); r != nil {
			logFileOnly(fmt.Sprintf("daemon panic recovered: %v", r))
		}
	}()
	// 后台模式：只写日志文件，不打控制台
	defer os.Remove(daemonPidFile())

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	logFileOnly("daemon loop started, waiting for ticker...")
	for range ticker.C {
		logFileOnly("=== ticker fired ===")
		remoteVersions := getRemoteVersions(VersionUrl)
		localVersions := getLocalVersions(LocalVersionFile)
		logFileOnly(fmt.Sprintf("remote versions: %v", remoteVersions))
		logFileOnly(fmt.Sprintf("local versions: %v", localVersions))

		hasUpdate := false
		// 检查 nginx 上所有包（不仅限于 filesToDownload）
		for filename, remoteVer := range remoteVersions {
			localVer := localVersions[filename]
			// 本地版本不存在或与远程不一致时触发更新
			if localVer == "" || localVer != remoteVer {
				hasUpdate = true
				break
			}
		}

		if !hasUpdate {
			logFileOnly("=== 定时检查：版本无更新 ===")
			continue
		}

		logFileOnly("=== 定时检查：检测到版本更新，弹出窗口重走流程 ===")
		exePath, _ := os.Executable()
		exec.Command("cmd", "/c", "start", "", exePath).Start()
		os.Exit(0)
	}
}

var filesToDownload = []string{
	"tools.zip",
	"tomcat.zip",
	"aps_client_detect_win10.zip",
	"aps_client_detect_win7.zip",
	"jdk17.zip",
	"agent.war",
}

func main() {
	// 初始化路径，基于 exe 所在目录
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	WorkDir = filepath.Join(exeDir, "apsAgent")
	LogFile = filepath.Join(exeDir, "run.log")
	LocalVersionFile = filepath.Join(WorkDir, "version.txt")
	AgentInfoFile = filepath.Join(WorkDir, "agent_info")

	os.MkdirAll(WorkDir, 0755)

	// 后台 daemon 模式：只做定时检查，无窗口
	if len(os.Args) > 1 && os.Args[1] == "--daemon" {
		// 从参数中获取 workdir，确保路径和主进程一致
		for _, arg := range os.Args[2:] {
			if len(arg) > 10 && arg[:10] == "--workdir=" {
				WorkDir = arg[10:]
				LogFile = filepath.Join(exeDir, "run.log")
				LocalVersionFile = filepath.Join(WorkDir, "version.txt")
				AgentInfoFile = filepath.Join(WorkDir, "agent_info")
			}
		}
		// 确保目录存在
		os.MkdirAll(WorkDir, 0755)
		logFileOnly("=== 后台 daemon 启动 WorkDir=" + WorkDir + " ===")
		runDaemon()
		return
	}

	// 程序启动时立即杀掉旧 Tomcat
	killTomcat()

	// 1. 获取本地 DNS
	dns := handleDNSSelection()
	logMsg(fmt.Sprintf("获取到本地 DNS: %s", dns))

	// 2. 获取/初始化 probe_id 和 probe_name
	probeID, probeName := ensureProbeInfo()
	logMsg(fmt.Sprintf("probe_id=%s, probe_name=%s", probeID, probeName))

	// 3. 注册探针（名称重复继续，其他失败停止）
	logMsg("正在注册探针...")
	if err := registerProbe(probeID, probeName, dns); err != nil {
		logMsg(fmt.Sprintf("=== 安装中止：探针注册失败: %v ===", err))
		fmt.Println("\n按回车键 (Enter) 退出...")
		fmt.Scanln()
		return
	}

	// 4. 设置开机自启
	setupAutoStart()

	// 5. 检查版本并下载更新
	checkAndUpdate()

	// 6. 安装运行环境（首次运行）
	installEnvironment()

	// 7. 部署 WAR
	if err := deployWar(); err != nil {
		logMsg(fmt.Sprintf("=== 安装中止：%v ===", err))
		fmt.Println("\n按回车键 (Enter) 退出...")
		fmt.Scanln()
		return
	}

	// 8. 启动 Tomcat
	logMsg("正在启动 Tomcat...")
	if err := startTomcat(); err != nil {
		logMsg(fmt.Sprintf("=== 启动失败：%v ===", err))
		fmt.Println("\n按回车键 (Enter) 退出...")
		fmt.Scanln()
		return
	}

	logMsg("=== 所有任务执行完毕 ===")

	// 启动后台 daemon 进程做定时检查，当前窗口可以关闭
	startDaemon()

	fmt.Println("\n按回车键 (Enter) 退出（关闭窗口也可以，后台检查已启动）...")
	fmt.Scanln()
}

func logMsg(msg string) {
	timeStr := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] %s\n", timeStr, msg)
	fmt.Print(line)

	f, err := os.OpenFile(LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(line)
		f.Close()
	}
}

func logFileOnly(msg string) {
	timeStr := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] %s\n", timeStr, msg)
	f, err := os.OpenFile(LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(line)
		f.Close()
	}
}
