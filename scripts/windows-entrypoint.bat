@echo off
setlocal DisableDelayedExpansion
pushd "%~dp0.."
if errorlevel 1 exit /b 1
set "AIDE_ENTRY_RC=1"
echo [Aide] %~1
if /i "%~1"=="build" goto source_build
if /i "%~1"=="clean" goto source_build
goto launch
:source_build
if not exist "go.mod" goto source_required
if not exist "Dockerfile" goto source_required
for %%A in (%*) do if /i "%%~A"=="--dry-run" goto build_dry_run
call "%~dp0windows-build-proxy.bat"
if errorlevel 1 goto finish
call "%~dp0windows-bootstrap.bat"
if errorlevel 1 goto finish
goto launch
:source_required
echo This command requires a Git source checkout. Release packages provide start, stop, docker_clean and verify.
goto finish
:build_dry_run
echo Dry run: compile the current native host in Docker, then build the source runtime. No commands executed.
set "AIDE_ENTRY_RC=0"
goto finish
:launch
if not exist "aide-launcher.exe" goto missing_host
"%CD%\aide-launcher.exe" --launcher-contract <nul 2>nul | findstr.exe /x /c:"aide-launcher-contract=2" >nul
if errorlevel 1 goto stale_host
"%CD%\aide-launcher.exe" %*
set "AIDE_ENTRY_RC=%ERRORLEVEL%"
goto finish
:missing_host
echo Missing aide-launcher.exe. In a Git checkout run build.bat; otherwise restore the complete Release package.
goto finish
:stale_host
echo The existing aide-launcher.exe is outdated. In this Git checkout run build.bat to rebuild it from current source.
echo Release users must restore a complete matching Release package. No legacy script was executed.
:finish
echo [Aide] %~1 finished with exit code %AIDE_ENTRY_RC%.
if not "%AIDE_ENTRY_RC%"=="0" if exist ".aide-data\native\launcher.log" echo Diagnostics: %CD%\.aide-data\native\launcher.log
call :wait_if_double_clicked
popd
exit /b %AIDE_ENTRY_RC%
:wait_if_double_clicked
if defined AIDE_LAUNCHER_NO_PAUSE exit /b 0
REM Delayed expansion makes CMDCMDLINE inspection safe even for quoted paths.
setlocal EnableDelayedExpansion
set "AIDE_ENTRY_CMDLINE=!cmdcmdline!"
set AIDE_ENTRY_CMDLINE | "%SystemRoot%\System32\findstr.exe" /i /c:" /c " >nul
if not errorlevel 1 pause
endlocal
exit /b 0
