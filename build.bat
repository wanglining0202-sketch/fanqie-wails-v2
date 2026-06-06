@echo off
REM 老王下载 — Wails v2 构建脚本
REM 需要: Go 1.18+, Wails CLI, gcc (mingw64), NSIS (makensis)

set PATH=C:\Program Files\Go\bin;%USERPROFILE%\go\bin;J:\AIClear-Mod\mingw64\bin;"C:\Program Files (x86)\NSIS";%PATH%

echo === 检查 Wails CLI ===
wails version >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo 安装 Wails CLI...
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
)

echo === 检查 NSIS ===
makensis /VERSION >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo NSIS 未安装。请从 https://nsis.sourceforge.io/Download 下载安装。
    echo 没有 NSIS 将生成普通 exe (不带 WebView2 离线包)。
    echo.
    echo === 构建普通 exe ===
    wails build -webview2 download
    goto :done
)

echo === 构建完整离线安装包 (包含 WebView2) ===
wails build -nsis -webview2 embed

:done
if %ERRORLEVEL% EQU 0 (
    echo.
    echo === 构建成功 ===
    if exist "build\bin\老王下载-amd64-installer.exe" (
        echo 安装包: build\bin\老王下载-amd64-installer.exe
    ) else (
        echo 应用:   build\bin\fanqie-novel-downloader.exe
    )
) else (
    echo.
    echo === 构建失败 ===
    exit /b 1
)
