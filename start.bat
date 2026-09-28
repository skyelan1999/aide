@echo off
REM aide Windows double-click entry: bypass execution policy and call start.ps1.
REM powershell.exe may not be on PATH (stripped PATH env), so use the absolute
REM system path; prefer pwsh 7 when available.
set "PS_EXE=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
where pwsh >nul 2>nul
if not errorlevel 1 set "PS_EXE=pwsh"
"%PS_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1" %*
if errorlevel 1 (
  echo.
  echo Startup failed. Check the errors above. Press any key to close...
  pause >nul
)
