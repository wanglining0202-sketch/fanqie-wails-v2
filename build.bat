@echo off
REM 番茄小说下载器 — Wails v2 构建脚本
REM 需要: Go 1.18+, Wails CLI, gcc (mingw64)

set PATH=C:\Program Files\Go\bin;%USERPROFILE%\go\bin;J:\AIClear-Mod\mingw64\bin;%PATH%

echo === 检查 Wails CLI ===
wails version >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo 安装 Wails CLI...
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
)

echo === 构建桌面应用 ===
echo   策略: -webview2 download (首次运行自动下载运行时)
echo   如需内嵌WebView2: 安装NSIS后运行 wails build -nsis -webview2 embed
echo.
wails build -webview2 download

if %ERRORLEVEL% EQU 0 (
    echo.
    echo === 构建成功 ===
    echo 输出: build\bin\fanqie-novel-downloader.exe
    echo.
    echo 分发: 直接发送 exe 即可。Win10/11 自带 WebView2.
    echo       老系统首次运行会自动下载运行时 (~100MB).
    echo       如需要完全离线安装包: 先装 NSIS 再 wails build -nsis -webview2 embed
) else (
    echo.
    echo === 构建失败 ===
    exit /b 1
)
