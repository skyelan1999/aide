@echo off
if /I "%AIDE_WINDOWS_LAUNCHER_MODE%"=="native" goto native
if not exist "%~dp0stop.ps1" goto native
set "PS_EXE=%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
where pwsh >nul 2>nul
if not errorlevel 1 set "PS_EXE=pwsh"
"%PS_EXE%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0stop.ps1" %*
set "MAINTENANCE_EXIT=%ERRORLEVEL%"
if not "%MAINTENANCE_EXIT%"=="0" pause
exit /b %MAINTENANCE_EXIT%
:native
if not exist "%~dp0aide-launcher.exe" exit /b 1
"%~dp0aide-launcher.exe" stop %*
exit /b %ERRORLEVEL%
