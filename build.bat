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
wails build

if %ERRORLEVEL% EQU 0 (
    echo.
    echo === 构建成功 ===
    echo 输出: build\bin\fanqie-novel-downloader.exe
) else (
    echo.
    echo === 构建失败 ===
    exit /b 1
)
