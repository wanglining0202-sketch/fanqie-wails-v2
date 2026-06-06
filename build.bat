@echo off
REM 番茄小说下载器 — Wails 构建脚本
REM 需要: Go 1.18+, Node.js, gcc (mingw64)

set PATH=C:\Program Files\Go\bin;J:\AIClear-Mod\mingw64\bin;%PATH%

echo === 清理旧构建 ===
if exist build\bin rmdir /s /q build\bin

echo === 下载依赖 ===
go mod tidy

echo === 构建 Wails 桌面应用 ===
go build -tags desktop -ldflags "-H windowsgui -s -w" -o build\bin\fanqie-novel-downloader.exe .

if %ERRORLEVEL% EQU 0 (
    echo.
    echo === 构建成功 ===
    echo 输出: build\bin\fanqie-novel-downloader.exe
) else (
    echo.
    echo === 构建失败 ===
    exit /b 1
)
