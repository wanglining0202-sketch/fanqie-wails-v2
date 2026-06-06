@echo off
REM 注册码生成器构建
set PATH=C:\Program Files\Go\bin;%PATH%
cd /d "%~dp0keygen"
go build -o keygen.exe .
echo.
echo keygen.exe 已构建
echo 用法: keygen.exe [数量]
