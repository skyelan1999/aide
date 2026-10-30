@echo off
setlocal DisableDelayedExpansion
call "%~dp0scripts\windows-entrypoint.bat" stop %*
exit /b %ERRORLEVEL%
