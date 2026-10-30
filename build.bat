@echo off
setlocal DisableDelayedExpansion
call "%~dp0scripts\windows-entrypoint.bat" build %*
exit /b %ERRORLEVEL%
