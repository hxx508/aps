@echo off
chcp 65001 >nul

echo 编译 APS Agent 探针工具...
cd /d "%~dp0"

go build -o aps_agent.exe .

if errorlevel 1 (
    echo [失败] 探针工具编译失败
    pause
    exit /b 1
)

echo [成功] aps_agent.exe 编译完成

echo.
echo 编译清理工具...
cd uninstall

go build -o aps_uninstall.exe .

if errorlevel 1 (
    echo [失败] 清理工具编译失败
    pause
    exit /b 1
)

echo [成功] aps_uninstall.exe 编译完成

echo.
echo ========================================
echo  编译完成！
echo ========================================
echo.
echo 生成文件：
cd /d "%~dp0"
dir /b *.exe
echo.
echo 部署目录结构：
echo   xxx.exe          - 探针部署工具
echo   uninstall/       - 清理工具目录
echo     aps_uninstall.exe - 清理工具
echo.
pause
