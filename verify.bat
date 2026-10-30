@echo off
setlocal DisableDelayedExpansion
call "%~dp0scripts\windows-entrypoint.bat" verify %*
exit /b %ERRORLEVEL%
