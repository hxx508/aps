# APS Agent 探针部署工具

## 目录

- [概述](#概述)
- [安装包目录结构](#安装包目录结构)
- [版本更新流程（运维操作指南）](#版本更新流程运维操作指南)
- [配置文件说明](#配置文件说明)
- [程序目录结构](#程序目录结构)
- [运行模式](#运行模式)
- [完整流程详解](#完整流程详解)
- [模块说明](#模块说明)
- [日志说明](#日志说明)
- [常见问题](#常见问题)

---

## 概述

`aps_agent.exe` 是 APS 探针的自动化部署工具，运行在 Windows 系统上，用于：

- 自动化安装探针运行环境（VC++、JDK、Tomcat、Npcap 等）
- 向中心服务器注册探针
- 启动和管理 Tomcat 服务
- 定时检查远程版本，自动更新

**特点**：
- 单文件部署，无需安装
- 首次运行自动下载所有依赖
- 支持开机自启
- 后台静默检查版本更新
- 零接触运维

**配置地址**：
- 软件包地址：`http://ip:9091/software/`
- 探针注册地址：`http://ip:9090/center/api/probe/register`

---

## 安装包目录结构

在服务器上，软件包存放在 `/software/` 目录下（对应 URL：`http://ip:9091/software/`）：

```
/software/
├── version.txt          # 版本配置文件（JSON格式）
├── tools.zip           # 工具包（VC++安装程序、dig工具等）
├── tomcat.zip          # Tomcat 服务器
├── jdk17.zip           # JDK 17 运行环境
├── agent.war           # Web应用包
├── aps_client_detect_win10.zip   # 探头检测工具（Win10/11）
├── aps_client_detect_win7.zip    # 探头检测工具（Win7）
└── aps_agent.exe       # 部署工具本身
```

---

## 版本更新流程（运维操作指南）

### 方式一：更新软件包（如 tomcat、jdk、agent.war 等）

#### 步骤 1：上传新包到 Nginx

将新的软件包上传到 `/software/` 目录，**保持文件名不变**：

```bash
# 例如更新 tomcat
scp tomcat_new.zip root@nginx-server:/software/tomcat.zip

# 或者直接覆盖
cp /path/to/new/tomcat.zip /software/tomcat.zip
```

#### 步骤 2：修改 version.txt

编辑 `/software/version.txt`，将对应文件的版本号改为新值：

```bash
vi /software/version.txt
```

**version.txt 示例**：
```json
{
  "tools.zip": "1.5",
  "tomcat.zip": "2.1",
  "jdk17.zip": "1.3",
  "agent.war": "3.0",
  "aps_client_detect_win10.zip": "1.2",
  "aps_client_detect_win7.zip": "1.2",
  "aps_agent.exe": "1.0"
}
```

#### 步骤 3：等待探针自动更新

探针每 1 分钟检查一次版本，发现以下情况会自动处理：
1. **新增包**：自动下载并记录版本
2. **版本不一致**：下载新包，解压（如是 zip），记录新版本
3. **版本一致**：跳过

**无需人工干预**。

**示例**：如果 nginx 上新增了 `new_package.zip` 并添加到 version.txt，探针检测到后会：
- 检测到本地无 `new_package.zip` 版本记录
- 自动下载 `new_package.zip`
- 解压（如是 zip 文件）
- 更新本地 version.txt

### 方式二：更新 aps_agent.exe 本身

#### 步骤 1：编译新的 aps_agent.exe

在开发机器上编译：
```bash
cd D:\code\CDNSystem\aps\go
go build -o aps_agent.exe .
```

#### 步骤 2：上传到 Nginx

```bash
scp aps_agent.exe root@nginx-server:/software/aps_agent.exe
```

#### 步骤 3：更新 version.txt 中的版本号

```bash
vi /software/version.txt
# 修改 "aps_agent.exe": "1.0" 为新版本号
```

#### 步骤 4：探针自动更新

当探针检测到 `aps_agent.exe` 版本不一致时：
1. 下载新版本到 `aps_agent.exe.new`
2. 启动更新脚本，等待当前进程结束
3. 替换旧文件，重新启动

**注意**：由于 exe 是自替换，探针会以新版本重新执行完整流程。

### 方式三：批量推送版本更新

可以使用脚本批量更新：

```bash
#!/bin/bash
# update_version.sh - 批量更新版本号

NEW_VERSION=$1
PACKAGE=$2

if [ -z "$NEW_VERSION" ] || [ -z "$PACKAGE" ]; then
    echo "用法: $0 <新版本号> <包名>"
    echo "示例: $0 2.5 agent.war"
    exit 1
fi

# 更新 version.txt
sed -i "s/\"$PACKAGE\": \"[^\"]*\"/\"$PACKAGE\": \"$NEW_VERSION\"/" /software/version.txt

echo "已更新 $PACKAGE 版本为 $NEW_VERSION"

# 验证
cat /software/version.txt | grep "$PACKAGE"
```

使用示例：
```bash
./update_version.sh 3.0 agent.war
./update_version.sh 1.5 tomcat.zip
```

---

## 配置文件说明

### version.txt 格式

**位置**：Nginx 服务器 `/software/version.txt`

**格式**：标准 JSON，key 为文件名，value 为版本号（字符串）

**示例**：
```json
{
  "tools.zip": "1.5",
  "tomcat.zip": "2.1",
  "jdk17.zip": "1.3",
  "agent.war": "3.0",
  "aps_client_detect_win10.zip": "1.2",
  "aps_client_detect_win7.zip": "1.2",
  "aps_agent.exe": "1.0"
}
```

**版本号规则**：
- 版本号可以是任意字符串，如 `1.0`、`20240408`、`v2.1.3`
- 探针只做字符串比对，版本号变化即触发更新
- 建议使用语义化版本号便于管理

### agent_info 文件

**位置**：探针本地 `C:\apsAgent\agent_info`（或 `{exe所在目录}\apsAgent\agent_info`）

**格式**：每行 `key=value`

**示例**：
```
probe_id=550e8400-e29b-41d4-a716-446655440000
probe_name=北京机房探针01
dns=221.6.4.66,114.114.114.114
```

**说明**：
- `probe_id`：探针唯一标识，首次自动生成，后续保持不变
- `probe_name`：探针名称，部署时由用户输入
- `dns`：探针使用的 DNS 服务器地址

### 本地 version.txt

**位置**：`{WorkDir}\version.txt`

**用途**：记录已安装的各包版本号，用于比对是否需要更新

**示例**：
```json
{
  "tools.zip": "1.5",
  "tomcat.zip": "2.1",
  "jdk17.zip": "1.3",
  "agent.war": "2.9",
  "aps_client_detect_win10.zip": "1.2",
  "aps_client_detect_win7.zip": "1.2",
  "aps_agent.exe": "1.0"
}
```

---

## 程序目录结构

### 探针运行目录

```
{exe所在目录}/
├── aps_agent.exe          # 部署工具主程序
└── apsAgent/              # 工作目录（自动创建）
    ├── run.log             # 运行日志
    ├── version.txt         # 本地版本记录
    ├── agent_info          # 探针配置
    ├── daemon.pid          # 后台进程PID文件
    ├── agent.war           # Web应用包
    ├── tomcat/             # Tomcat服务器
    │   ├── bin/
    │   │   ├── startup.bat
    │   │   └── shutdown.bat
    │   ├── webapps/
    │   │   └── agent.war   # 已部署的WAR包
    │   └── logs/
    │       └── aps/
    │           └── agent.log  # 应用日志（log4j）
    ├── jdk17/              # JDK运行环境
    │   └── bin/
    │       └── javaw.exe
    └── tools/              # 工具目录
        ├── vcredist_x64.exe
        ├── vcredist_x86.exe
        ├── npcap-1.87.exe
        ├── WinPcap_4_1_3.exe
        ├── Windows6.1-KB2533623-x64.msu  # Win7补丁
        └── dig/                # dig工具
```

### 日志输出位置

| 日志类型 | 路径 | 说明 |
|----------|------|------|
| 程序日志 | `apsAgent/run.log` | 所有操作记录 |
| 应用日志 | `apsAgent/tomcat/logs/aps/agent.log` | Tomcat应用日志 |
| Tomcat日志 | `apsAgent/tomcat/logs/catalina.out` | Tomcat启动日志 |

---

## 运行模式

### 模式一：交互模式（正常启动）

```
点击 aps_agent.exe 或命令行运行
```

- 有控制台窗口
- 显示详细操作日志
- 支持交互输入（探针名称、DNS选择）
- 完成后可关闭窗口，后台 daemon 继续运行

### 模式二：后台模式（自动启动）

```
aps_agent.exe --daemon --workdir=C:\xxx\apsAgent
```

- 无控制台窗口
- 日志只写入 run.log
- 每分钟检查版本更新
- 检测到更新时弹出交互窗口

### 模式三：开机自启

- 程序注册到 Windows 注册表
- 开机自动运行交互模式
- 完成后启动后台 daemon

---

## 完整流程详解

### 1. 路径初始化

```
exe所在目录 + apsAgent = WorkDir
```

- 程序自动检测 exe 所在目录
- 在该目录下创建 `apsAgent` 子目录
- 所有后续操作都在 WorkDir 下进行

### 2. 清理旧进程

```
killExistingDaemon()
killTomcat()
```

- 检查是否有旧的 daemon 进程（通过 daemon.pid）
- 执行 Tomcat shutdown.bat 正常停止
- 按端口 8080 强制杀掉残留进程

### 3. DNS 获取

```
handleDNSSelection()
```

- 检查 agent_info 是否已有 DNS 配置
- 无则自动获取本地 DNS
- 优先使用 wmic（最准确）
- 降级使用 netsh
- 过滤虚拟网卡和 DHCP 分配的 DNS
- 多次获取自动去重

### 4. 探针信息初始化

```
ensureProbeInfo()
```

- 检查 agent_info 中的 probe_id
- 不存在则自动生成 UUID v4
- 检查 probe_name
- 不存在则提示用户输入

### 5. 探针注册

```
registerProbe()
```

- 向中心服务器注册探针
- HTTP POST 请求
- 支持探针名称冲突重试
- 失败则中止流程

### 6. 开机自启设置

```
setupAutoStart()
```

- 写入注册表 `HKCU\...\Run`
- 值为 exe 的绝对路径
- 开机自动运行

### 7. 版本检查与下载

```
checkAndUpdate()
```

**比对逻辑**：
- 比对 nginx 上 **所有** 包（不限于预定义列表）
- 本地不存在的包（新增）：自动下载并记录版本
- 版本不一致的包：下载并更新
- 版本一致的包：跳过
- `aps_agent.exe`：跳过下载，但记录版本号（因为正在运行无法替换）

**首次运行**：下载 nginx 上所有包
**非首次运行**：下载新增或版本不一致的包

### 8. 运行环境安装

```
installEnvironment()
```

按顺序安装：
1. **VC++ 2012 运行库**（注册表检测，已安装跳过）
2. **Npcap/WinPcap**（根据系统版本选择）
3. **dig 工具**（复制到 System32）
4. **探头检测工具**（重命名目录）
5. **KB2533623 补丁**（仅 Win7）

### 9. WAR 部署

```
deployWar()
```

- 读取 `apsAgent/agent.war`
- 复制到 `apsAgent/tomcat/webapps/`
- 覆盖旧版本

### 10. Tomcat 启动

```
startTomcat()
```

- 使用 javaw 后台运行（关闭窗口不影响）
- 设置 IPv6 优先
- 工作目录设为 apsAgent（log4j 日志写到正确位置）

### 11. 启动后台 Daemon

```
startDaemon()
```

- 杀掉已有 daemon
- 启动新的无窗口进程
- 写入 daemon.pid

---

## 模块说明

### main.go - 主入口

| 函数 | 说明 |
|------|------|
| `main()` | 程序入口，协调各模块执行 |
| `startDaemon()` | 启动后台守护进程 |
| `runDaemon()` | 后台循环，检测版本更新 |
| `killExistingDaemon()` | 杀掉已有 daemon 进程 |
| `logMsg()` | 输出日志到控制台和文件 |
| `logFileOnly()` | 仅输出日志到文件 |

### probe.go - 探针管理

| 函数 | 说明 |
|------|------|
| `readAgentInfo()` | 读取 agent_info 配置 |
| `writeAgentInfo()` | 写入 agent_info 配置 |
| `generateUUID()` | 生成 UUID v4 |
| `ensureProbeInfo()` | 确保探针信息完整 |
| `promptProbeName()` | 交互输入探针名称 |
| `registerProbe()` | 向服务器注册探针 |

### tomcat.go - Tomcat 管理

| 函数 | 说明 |
|------|------|
| `setupAutoStart()` | 设置开机自启 |
| `killTomcat()` | 停止 Tomcat |
| `deployWar()` | 部署 WAR 包 |
| `startTomcat()` | 启动 Tomcat |

### install_env.go - 环境安装

| 函数 | 说明 |
|------|------|
| `isVCRedistInstalled()` | 检测 VC++ 2012 |
| `isNpcapInstalled()` | 检测 Npcap |
| `isWinPcapInstalled()` | 检测 WinPcap |
| `isDigInstalled()` | 检测 dig 工具 |
| `isKBInstalled()` | 检测 KB2533623 |
| `installVCRedist()` | 安装 VC++ |
| `installPcap()` | 安装 Npcap/WinPcap |
| `installDig()` | 安装 dig |
| `installClientDetect()` | 安装探头检测工具 |
| `installWin7Patch()` | 安装 Win7 补丁 |
| `installEnvironment()` | 安装所有环境 |

### download.go - 版本管理

| 函数 | 说明 |
|------|------|
| `checkAndUpdate()` | 检查并更新版本 |
| `downloadFile()` | 下载文件 |
| `unzip()` | 解压 zip |
| `getRemoteVersions()` | 获取远程版本 |
| `getLocalVersions()` | 获取本地版本 |
| `saveLocalVersions()` | 保存本地版本 |

### dns.go - DNS 获取

| 函数 | 说明 |
|------|------|
| `getLocalDNS()` | 获取本地 DNS |
| `getDNSByWmic()` | wmic 方式获取 |
| `getDNSByNetsh()` | netsh 方式获取 |
| `handleDNSSelection()` | 处理 DNS 选择 |

---

## 日志说明

### 日志级别

程序使用简单的时间戳日志格式：

```
[2026-04-08 14:30:15] 日志内容
```

### 日志查看

**Windows 命令行查看**：
```cmd
type C:\run.log
type D:\deploy\run.log
```

**实时查看**：
```cmd
powershell Get-Content C:\run.log -Wait -Tail 50
```

### 日志输出位置

| 函数 | 控制台 | run.log | 说明 |
|------|--------|---------|------|
| `logMsg()` | ✅ | ✅ | 正常模式 |
| `logFileOnly()` | ❌ | ✅ | daemon 模式 |

---

## 常见问题

### Q1：探针无法注册

**症状**：`注册失败` 或网络错误

**排查步骤**：
1. 检查网络是否畅通：`ping ip`
2. 检查端口是否可达：`telnet ip 9090`
3. 检查探针名称是否重复
4. 查看 run.log 中的具体错误信息

**解决方案**：
- 修复网络后重试
- 使用新的探针名称
- 联系中心服务器管理员

### Q2：VC++ 安装失败

**症状**：`[错误] VC++ 安装失败`

**原因**：没有管理员权限或安装包损坏

**解决方案**：
1. 右键以管理员身份运行 aps_agent.exe
2. 检查 tools 目录下 vcredist_x64.exe 是否完整
3. 手动安装 VC++ 后重试

### Q3：Tomcat 无法启动

**症状**：`Tomcat 启动失败` 或进程立即退出

**排查步骤**：
1. 检查 jdk17/bin/javaw.exe 是否存在
2. 检查 tomcat/bin/bootstrap.jar 是否存在
3. 查看 Tomcat 日志：`type apsAgent\tomcat\logs\catalina.out`

**解决方案**：
1. 重新下载 jdk17.zip 和 tomcat.zip
2. 检查端口 8080 是否被占用：`netstat -ano | findstr :8080`

### Q4：版本不更新

**症状**：修改了 version.txt 但探针没有更新

**排查步骤**：
1. 检查探针是否在线
2. 检查 daemon 进程是否运行：`tasklist | findstr aps_agent`
3. 查看 run.log 中的版本检查记录

**解决方案**：
1. 重启探针程序
2. 删除本地 version.txt（会触发首次运行流程）
3. 手动启动探针检查版本

### Q5：多个 daemon 进程

**症状**：后台有多个 aps_agent.exe 进程

**原因**：之前启动时没有正确清理

**解决方案**：
```cmd
taskkill /f /im aps_agent.exe
```
杀掉所有进程后重新启动

### Q6：如何查看探针状态

**方法一：查看进程**
```cmd
tasklist | findstr aps_agent
```

**方法二：查看端口**
```cmd
netstat -ano | findstr :8080
```

**方法三：查看日志**
```cmd
type C:\run.log
```

### Q7：如何手动重启 Tomcat

```cmd
# 正常停止
C:\apsAgent\tomcat\bin\shutdown.bat

# 强制停止
taskkill /f /im java.exe

# 启动
C:\apsAgent\jdk17\bin\javaw.exe -Dcatalina.home=C:\apsAgent\tomcat -Dcatalina.base=C:\apsAgent\tomcat -Djava.net.preferIPv6Addresses=true -classpath C:\apsAgent\tomcat\bin\bootstrap.jar;C:\apsAgent\tomcat\bin\tomcat-juli.jar org.apache.catalina.startup.Bootstrap start
```

---

## 编译说明

### 开发环境编译

```bash
# 编译探针部署工具
cd D:\code\CDNSystem\aps\go
go build -o aps_agent.exe .

# 编译清理工具
cd D:\code\CDNSystem\aps\go\uninstall
go build -o aps_uninstall.exe .
```

### 交叉编译（在 Linux/Mac 编译 Windows 版本）

```bash
# 编译探针部署工具
cd D:\code\CDNSystem\aps\go
GOOS=windows GOARCH=amd64 go build -o aps_agent.exe .

# 编译清理工具
cd D:\code\CDNSystem\aps\go\uninstall
GOOS=windows GOARCH=amd64 go build -o aps_uninstall.exe .
```

### 一键打包脚本

在 `go/` 目录下创建 `build.bat`：

```bat
@echo off
chcp 65001 >nul

echo 编译探针部署工具...
cd /d %~dp0
go build -o aps_agent.exe .

echo 编译清理工具...
cd uninstall
go build -o aps_uninstall.exe .

echo.
echo 编译完成！
echo 生成文件：
dir /b *.exe
pause
```

### 生产环境部署

1. 运行 `build.bat` 编译得到 `aps_agent.exe` 和 `aps_uninstall.exe`
2. 将两个 exe 放在同级目录
3. 将 `aps_agent.exe` 上传到 Nginx：`scp aps_agent.exe root@nginx:/software/`
4. 更新 version.txt 中的版本号
5. 探针自动检测并更新

---

## 清理工具（aps_uninstall.exe）

`aps_uninstall.exe` 用于完全卸载探针，清理所有相关文件和进程。

### 功能

- 删除开机自启动注册表
- 杀掉 daemon 进程
- 停止 Tomcat 服务
- 删除 `apsAgent` 工作目录及所有文件

### 使用方法

1. 双击运行 `aps_uninstall.exe`
2. 等待清理完成
3. 按回车键退出

### 注意事项

- 运行前请确保已关闭其他正在使用探针的程序
- 清理操作不可逆，删除的文件无法恢复
- 如需重新部署，运行 `aps_agent.exe` 即可

---

## 文件清单

```
go/
├── main.go           # 主入口、daemon管理
├── probe.go          # 探针注册
├── tomcat.go         # Tomcat管理
├── install_env.go    # 运行环境安装
├── download.go       # 版本检查与下载
├── dns.go            # DNS获取
├── go.mod            # Go模块定义（探针工具）
├── README.md         # 本文档
├── build.bat         # 一键编译脚本
└── uninstall/       # 清理工具（独立模块）
    ├── main.go
    └── go.mod
```

部署后目录结构：
```
{部署目录}/
├── aps_agent.exe      # 探针部署工具
├── aps_uninstall.exe  # 清理工具
└── apsAgent/         # 工作目录（运行时创建）
    ├── run.log
    ├── version.txt
    ├── agent_info
    ├── daemon.pid
    ├── agent.war
    ├── tomcat/
    ├── jdk17/
    └── tools/
```

---

## 联系支持

如遇问题，请提供以下信息：

1. `run.log` 完整日志
2. 系统版本：`winver`
3. 探针进程列表：`tasklist | findstr aps`
4. Tomcat 状态：`netstat -ano | findstr :8080`
