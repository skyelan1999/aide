@echo off
REM Explicit opt-in while native A/B/monitor/migration parity is pending.
if /I "%AIDE_WINDOWS_LAUNCHER_MODE%"=="native" goto native
if not exist "%~dp0start.ps1" goto native
REM aide Windows double-click entry: bypass execution policy and call start.ps1.
REM powershell.exe may not be on PATH (stripped PATH env), so use the absolute
REM system path; prefer pwsh 7 when available.
set "PS_EXE=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
where pwsh >nul 2>nul
if not errorlevel 1 set "PS_EXE=pwsh"
"%PS_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1" %*
set "START_EXIT=%ERRORLEVEL%"
if not "%START_EXIT%"=="0" (
  echo.
  echo Startup failed. Check the errors above. Press any key to close...
  pause >nul
)
exit /b %START_EXIT%
:native
if not exist "%~dp0aide-launcher.exe" (
  echo Missing aide-launcher.exe. Build scripts/build-windows-launcher.py or restore a complete package.
  exit /b 1
)
"%~dp0aide-launcher.exe" start %*
exit /b %ERRORLEVEL%
