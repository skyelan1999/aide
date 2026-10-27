@echo off
if not exist "%~dp0aide-launcher.exe" (
  echo Missing aide-launcher.exe. Build it with scripts/build-windows-launcher.py or restore the complete package.
  pause
  exit /b 1
)
"%~dp0aide-launcher.exe" clean-cache %*
if errorlevel 1 (
  echo Action failed. Press any key to close...
  pause >nul
  exit /b 1
)
