@echo off
setlocal DisableDelayedExpansion
call "%~dp0scripts\windows-entrypoint.bat" clean %*
exit /b %ERRORLEVEL%
