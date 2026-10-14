@echo off
set "PS_EXE=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
where pwsh >nul 2>nul
if not errorlevel 1 set "PS_EXE=pwsh"
"%PS_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0stop.ps1" %*
set "MAINTENANCE_EXIT=%ERRORLEVEL%"
if not "%MAINTENANCE_EXIT%"=="0" pause
exit /b %MAINTENANCE_EXIT%
