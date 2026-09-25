@echo off
REM aide Windows 双击启动入口：绕过 PowerShell 执行策略，调用 start.ps1。
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1" %*
