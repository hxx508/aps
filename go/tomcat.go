package main

import (
	"fmt"
	"golang.org/x/sys/windows/registry"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// 设置开机自启
func setupAutoStart() {
	exePath, err := os.Executable()
	if err != nil {
		logMsg(fmt.Sprintf("设置开机自启失败: 获取程序路径失败: %v", err))
		return
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE)
	if err != nil {
		logMsg(fmt.Sprintf("设置开机自启失败: 打开注册表失败: %v", err))
		return
	}
	defer key.Close()

	if err := key.SetStringValue("ApsAgentLaunch", fmt.Sprintf(`"%s"`, exePath)); err != nil {
		logMsg(fmt.Sprintf("设置开机自启失败: 写入注册表失败: %v", err))
	} else {
		logMsg("开机自启设置成功")
	}
}

// 杀掉已有 Tomcat 进程（程序启动时即调用）
func killTomcat() {
	tomcatShutdown := filepath.Join(WorkDir, "tomcat", "bin", "shutdown.bat")
	if _, err := os.Stat(tomcatShutdown); err == nil {
		logMsg("执行 Tomcat shutdown.bat...")
		cmd := exec.Command("cmd", "/c", tomcatShutdown)
		cmd.Dir = filepath.Join(WorkDir, "tomcat", "bin")
		cmd.Run()
		time.Sleep(2 * time.Second)
	}

	// 按端口杀掉残留进程
	logMsg("检查并杀掉占用 " + TomcatPort + " 端口的进程...")
	out, err := exec.Command("cmd", "/c", "netstat", "-ano").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, ":"+TomcatPort) && strings.Contains(line, "LISTENING") {
				fields := strings.Fields(line)
				if len(fields) >= 5 {
					pid := fields[4]
					exec.Command("taskkill", "/F", "/PID", pid).Run()
					logMsg(fmt.Sprintf("已杀掉 PID %s", pid))
				}
			}
		}
	}

	killTomcatJavaProcesses()
	waitForTomcatJavaProcessesExit(60 * time.Second)
	logMsg("Tomcat 清理完成")
}

func killTomcatJavaProcesses() {
	logMsg("检查 Tomcat 对应的 java/javaw 进程...")

	processes, err := findTomcatJavaProcesses()
	if err != nil {
		logMsg(fmt.Sprintf("查询 Java 进程失败: %v", err))
		return
	}

	for _, proc := range processes {
		exec.Command("taskkill", "/F", "/PID", proc.PID).Run()
		logMsg(fmt.Sprintf("已杀掉 Tomcat Java 进程 PID %s", proc.PID))
	}
}

type tomcatJavaProcess struct {
	PID         string
	CommandLine string
}

func findTomcatJavaProcesses() ([]tomcatJavaProcess, error) {
	tomcatDir := strings.ToLower(filepath.Join(WorkDir, "tomcat"))
	out, err := exec.Command("wmic", "process", "where", "name='java.exe' or name='javaw.exe'", "get", "ProcessId,CommandLine", "/FORMAT:LIST").Output()
	if err != nil {
		return nil, err
	}

	var processes []tomcatJavaProcess
	blocks := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n\n")
	for _, block := range blocks {
		lines := strings.Split(block, "\n")
		var pid string
		var commandLine string

		for _, line := range lines {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "CommandLine="):
				commandLine = strings.TrimSpace(strings.TrimPrefix(line, "CommandLine="))
			case strings.HasPrefix(line, "ProcessId="):
				pid = strings.TrimSpace(strings.TrimPrefix(line, "ProcessId="))
			}
		}

		if pid == "" || commandLine == "" {
			continue
		}

		commandLineLower := strings.ToLower(commandLine)
		if !strings.Contains(commandLineLower, "org.apache.catalina.startup.bootstrap") &&
			!strings.Contains(commandLineLower, tomcatDir) &&
			!strings.Contains(commandLineLower, "-dcatalina.home=") {
			continue
		}

		processes = append(processes, tomcatJavaProcess{
			PID:         pid,
			CommandLine: commandLine,
		})
	}

	return processes, nil
}

func waitForTomcatJavaProcessesExit(timeout time.Duration) {
	logMsg("等待 Tomcat Java 进程完全退出...")
	logMsg("Tomcat 清理后固定等待 60 秒...")
	time.Sleep(60 * time.Second)

	deadline := time.Now().Add(timeout)
	for {
		processes, err := findTomcatJavaProcesses()
		if err != nil {
			logMsg(fmt.Sprintf("查询 Java 进程失败: %v", err))
			return
		}

		if len(processes) == 0 {
			logMsg("Tomcat Java 进程已全部退出")
			return
		}

		if time.Now().After(deadline) {
			logMsg(fmt.Sprintf("等待超时，仍有 %d 个 Tomcat Java 进程未退出", len(processes)))
			return
		}

		logMsg(fmt.Sprintf("仍有 %d 个 Tomcat Java 进程未退出，10 秒后重试", len(processes)))
		time.Sleep(10 * time.Second)
	}
}

// 将 agent.war 复制到 Tomcat webapps
func deployWar() error {
	warSrc := filepath.Join(WorkDir, "agent.war")
	webapps := filepath.Join(WorkDir, "tomcat", "webapps")

	if _, err := os.Stat(warSrc); os.IsNotExist(err) {
		return fmt.Errorf("agent.war 不存在: %s", warSrc)
	}
	if _, err := os.Stat(webapps); os.IsNotExist(err) {
		return fmt.Errorf("webapps 目录不存在: %s", webapps)
	}

	dst := filepath.Join(webapps, "agent.war")
	src, err := os.ReadFile(warSrc)
	if err != nil {
		return fmt.Errorf("读取 agent.war 失败: %v", err)
	}
	if err := os.WriteFile(dst, src, 0644); err != nil {
		return fmt.Errorf("复制 agent.war 失败: %v", err)
	}
	logMsg("agent.war 部署完成")
	return nil
}

// 启动 Tomcat（后台运行，兼容 IPv6，关闭窗口不会杀死进程）
func startTomcat() error {
	tomcatDir := filepath.Join(WorkDir, "tomcat")
	javaExe := filepath.Join(WorkDir, "jdk17", "bin", "javaw.exe")

	bootstrapJar := filepath.Join(tomcatDir, "bin", "bootstrap.jar")
	juliJar := filepath.Join(tomcatDir, "bin", "tomcat-juli.jar")

	if _, err := os.Stat(javaExe); os.IsNotExist(err) {
		return fmt.Errorf("java.exe 不存在: %s", javaExe)
	}
	if _, err := os.Stat(bootstrapJar); os.IsNotExist(err) {
		return fmt.Errorf("bootstrap.jar 不存在: %s", bootstrapJar)
	}
	if _, err := os.Stat(juliJar); os.IsNotExist(err) {
		return fmt.Errorf("tomcat-juli.jar 不存在: %s", juliJar)
	}

	logsDir := filepath.Join(tomcatDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return fmt.Errorf("创建 logs 目录失败: %w", err)
	}

	consoleLogPath := filepath.Join(logsDir, "tomcat-console.log")

	consoleLog, err := os.OpenFile(
		consoleLogPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		return fmt.Errorf("创建控制台日志失败: %w", err)
	}

	classpath := bootstrapJar + ";" + juliJar

	jvmArgs := []string{
		"-Xss256k",
		"-Xms256m",
		"-Xmx256m",
		"-XX:MaxMetaspaceSize=128m",
		"-XX:MaxDirectMemorySize=64m",
		"-XX:ReservedCodeCacheSize=64m",
		"-XX:+UseSerialGC",

		"-Dcatalina.home=" + tomcatDir,
		"-Dcatalina.base=" + tomcatDir,
		"-Djava.io.tmpdir=" + filepath.Join(tomcatDir, "temp"),

		"-Djava.util.logging.manager=org.apache.juli.ClassLoaderLogManager",
		"-Djava.util.logging.config.file=" + filepath.Join(tomcatDir, "conf", "logging.properties"),

		"-Djava.net.preferIPv4Stack=false",
		"-Djava.net.preferIPv6Addresses=true",

		"-classpath", classpath,
		"org.apache.catalina.startup.Bootstrap",
		"start",
	}

	cmd := exec.Command(javaExe, jvmArgs...)
	cmd.Dir = tomcatDir

	// 输出到日志文件
	cmd.Stdout = consoleLog
	cmd.Stderr = consoleLog

	// Windows 下以独立后台进程启动，关闭当前 exe 窗口后 Tomcat 继续运行。
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008,
	}

	startErr := cmd.Start()
	if startErr != nil && isProcessFileLockedError(startErr) {
		consoleLog.Close()
		logMsg("检测到 javaw.exe 仍被占用，等待后重试启动 Tomcat...")

		var err error
		for i := 1; i <= 5; i++ {
			time.Sleep(10 * time.Second)

			consoleLog, err = os.OpenFile(
				consoleLogPath,
				os.O_CREATE|os.O_WRONLY|os.O_APPEND,
				0644,
			)
			if err != nil {
				return fmt.Errorf("重新创建控制台日志失败: %w", err)
			}

			cmd = exec.Command(javaExe, jvmArgs...)
			cmd.Dir = tomcatDir
			cmd.Stdout = consoleLog
			cmd.Stderr = consoleLog
			cmd.SysProcAttr = &syscall.SysProcAttr{
				HideWindow:    true,
				CreationFlags: 0x00000008,
			}

			startErr = cmd.Start()
			if startErr == nil {
				logMsg(fmt.Sprintf("Tomcat 启动重试成功，第 %d 次重试", i))
				break
			}

			consoleLog.Close()
			if !isProcessFileLockedError(startErr) {
				return fmt.Errorf("启动 Tomcat 失败: %w", startErr)
			}
		}
	}

	if startErr != nil {
		consoleLog.Close()
		return fmt.Errorf("启动 Tomcat 失败: %w", startErr)
	}

	consoleLog.Close()

	logMsg(fmt.Sprintf("Tomcat 已启动，PID: %d", cmd.Process.Pid))
	logMsg("Tomcat 控制台日志: " + consoleLogPath)
	logMsg("Tomcat 日志目录: " + logsDir)
	return nil
}

func isProcessFileLockedError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "being used by another process") ||
		strings.Contains(errMsg, "另一个程序正在使用此文件") ||
		strings.Contains(errMsg, "the process cannot access the file")
}
