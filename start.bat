@echo off
setlocal DisableDelayedExpansion
call "%~dp0scripts\windows-entrypoint.bat" start %*
exit /b %ERRORLEVEL%
