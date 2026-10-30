@echo off
setlocal DisableDelayedExpansion
call "%~dp0scripts\windows-entrypoint.bat" docker_clean %*
exit /b %ERRORLEVEL%
