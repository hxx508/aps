package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

var WorkDir string

func main() {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	WorkDir = filepath.Join(exeDir, "apsAgent")

	fmt.Println("========================================")
	fmt.Println("   APS Agent 清理工具")
	fmt.Println("========================================")
	fmt.Println()

	// 1. 删除开机自启
	fmt.Println("[1/5] 删除开机自启...")
	removeAutoStart()

	// 2. 杀掉 daemon 进程
	fmt.Println("[2/5] 杀掉 daemon 进程...")
	killDaemon()

	// 3. 杀掉 Tomcat
	fmt.Println("[3/5] 杀掉 Tomcat...")
	killTomcat()

	// 4. 删除 WorkDir
	fmt.Println("[4/5] 删除工作目录...")
	deleteWorkDir()

	// 5. 确认
	fmt.Println("[5/5] 清理完成")
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("清理完成！")
	fmt.Println("========================================")

	fmt.Println("\n按回车键退出...")
	fmt.Scanln()
}

// 删除开机自启注册表
func removeAutoStart() {
	key, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE)
	if err != nil {
		fmt.Printf("  - 打开启动项注册表失败: %v\n", err)
		return
	}
	defer key.Close()

	if err := key.DeleteValue("ApsAgentLaunch"); err != nil {
		fmt.Println("  - 未找到开机自启或已删除")
	} else {
		fmt.Println("  - 已删除开机自启")
	}
}

// 杀掉 daemon 进程
func killDaemon() {
	pidFile := filepath.Join(WorkDir, "daemon.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Println("  - 未找到 daemon.pid，跳过")
		return
	}

	pid := strings.TrimSpace(string(data))
	if pid == "" {
		fmt.Println("  - daemon.pid 为空，跳过")
		return
	}

	cmd := exec.Command("taskkill", "/F", "/PID", pid)
	if err := cmd.Run(); err != nil {
		fmt.Printf("  - 杀掉 daemon (PID %s) 失败，可能已退出\n", pid)
	} else {
		fmt.Printf("  - 已杀掉 daemon (PID %s)\n", pid)
	}

	os.Remove(pidFile)
	fmt.Println("  - 已删除 daemon.pid")
}

// 杀掉 Tomcat（按端口）
func killTomcat() {
	// 先执行 shutdown.bat
	shutdownBat := filepath.Join(WorkDir, "tomcat", "bin", "shutdown.bat")
	if _, err := os.Stat(shutdownBat); err == nil {
		cmd := exec.Command("cmd", "/c", shutdownBat)
		cmd.Dir = filepath.Join(WorkDir, "tomcat", "bin")
		cmd.Run()
		time.Sleep(2 * time.Second)
		fmt.Println("  - 已执行 shutdown.bat")
	}

	// 按端口杀掉残留进程
	out, err := exec.Command("cmd", "/c", "netstat", "-ano").Output()
	if err != nil {
		fmt.Println("  - 检查端口失败")
		return
	}

	killed := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, ":8080") && strings.Contains(line, "LISTENING") {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				pid := fields[4]
				exec.Command("taskkill", "/F", "/PID", pid).Run()
				fmt.Printf("  - 已杀掉占用 8080 端口的进程 (PID %s)\n", pid)
				killed = true
			}
		}
	}

	if !killed {
		fmt.Println("  - 未发现占用 8080 端口的进程")
	}
}

// 删除 WorkDir 目录
func deleteWorkDir() {
	if _, err := os.Stat(WorkDir); os.IsNotExist(err) {
		fmt.Println("  - 工作目录不存在，跳过")
		return
	}

	// 先杀掉所有 java 进程（Tomcat 相关）
	exec.Command("taskkill", "/F", "/IM", "java.exe").Run()
	exec.Command("taskkill", "/F", "/IM", "javaw.exe").Run()
	time.Sleep(1 * time.Second)

	if err := os.RemoveAll(WorkDir); err != nil {
		fmt.Printf("  - 删除失败: %v\n", err)
	} else {
		fmt.Println("  - 已删除工作目录: " + WorkDir)
	}
}
