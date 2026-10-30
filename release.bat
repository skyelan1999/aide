@echo off
setlocal DisableDelayedExpansion
call "%~dp0scripts\windows-entrypoint.bat" release %*
exit /b %ERRORLEVEL%
