@echo off
REM Windows lifecycle is implemented by the single native executable.
if not exist "%~dp0aide-launcher.exe" (
  echo Missing aide-launcher.exe. Restore the complete Release package or run build.bat in a Git checkout.
  exit /b 1
)
"%~dp0aide-launcher.exe" stop %*
exit /b %ERRORLEVEL%
