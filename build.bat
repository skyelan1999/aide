@echo off
setlocal
cd /d "%~dp0"
if not exist "%~dp0.git" (
  if not exist "%~dp0go.mod" (echo build requires a Git source package. & exit /b 1)
  if not exist "%~dp0Dockerfile" (echo build requires a Git source package. & exit /b 1)
)
set "AIDE_BOOT_DRY_RUN="
for %%A in (%*) do if /i "%%~A"=="--dry-run" set "AIDE_BOOT_DRY_RUN=1"
if defined AIDE_BOOT_DRY_RUN if not exist "%~dp0aide-launcher.exe" (echo Dry run: compile the native bootstrap and source runtime in owned Docker builders, then start. & exit /b 0)
if not exist "%~dp0aide-launcher.exe" call :bootstrap
if errorlevel 1 exit /b 1
"%~dp0aide-launcher.exe" build %*
exit /b %ERRORLEVEL%

:bootstrap
REM First Git build bootstraps the native C++ host inside Docker.
set "AIDE_BOOT_CID="
where docker.exe >nul 2>nul
if errorlevel 1 (echo Docker Desktop with Buildx is required. & exit /b 1)
docker.exe info >nul 2>nul
if errorlevel 1 (echo Start Docker Desktop with Linux containers before the first source build. & exit /b 1)
set "AIDE_BOOT_BUILDER=aide-bootstrap-%RANDOM%-%RANDOM%"
set "AIDE_BOOT_IMAGE=aide:%AIDE_BOOT_BUILDER%"
set "AIDE_BOOT_ARCH=amd64"
if /i "%PROCESSOR_ARCHITECTURE%"=="ARM64" set "AIDE_BOOT_ARCH=arm64"
if /i "%PROCESSOR_ARCHITEW6432%"=="ARM64" set "AIDE_BOOT_ARCH=arm64"
if not exist ".agent-state" mkdir ".agent-state"
set "AIDE_BOOT_CIDFILE=%~dp0.agent-state\%AIDE_BOOT_BUILDER%.cid"
if exist "%AIDE_BOOT_CIDFILE%" exit /b 1
docker.exe image inspect "%AIDE_BOOT_IMAGE%" >nul 2>nul
if not errorlevel 1 (echo Bootstrap image name already exists; retry. & exit /b 1)
docker.exe buildx create --name "%AIDE_BOOT_BUILDER%" --driver docker-container --driver-opt memory=4g,memory-swap=4g,cpu-period=100000,cpu-quota=200000,restart-policy=no
if errorlevel 1 exit /b 1
docker.exe buildx build --builder "%AIDE_BOOT_BUILDER%" --load --target native-launcher --file Dockerfile --build-arg "AIDE_LAUNCHER_ARCH=%AIDE_BOOT_ARCH%" --tag "%AIDE_BOOT_IMAGE%" .
if errorlevel 1 goto bootstrap_failed
docker.exe create --cidfile "%AIDE_BOOT_CIDFILE%" --network none --entrypoint /bin/true "%AIDE_BOOT_IMAGE%" >nul
if errorlevel 1 goto bootstrap_failed
REM Validate every line and the total size before expanding CID into any command.
for %%Z in ("%AIDE_BOOT_CIDFILE%") do if %%~zZ GTR 66 goto bootstrap_failed
findstr.exe /r /x /v "[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]" "%AIDE_BOOT_CIDFILE%" >nul
if not errorlevel 1 goto bootstrap_failed
findstr.exe /r /x "[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]" "%AIDE_BOOT_CIDFILE%" >nul
if errorlevel 1 goto bootstrap_failed
set /p AIDE_BOOT_CID=<"%AIDE_BOOT_CIDFILE%"
docker.exe cp "%AIDE_BOOT_CID%:/out/aide-launcher.exe" "%~dp0aide-launcher.exe.bootstrap"
if errorlevel 1 goto bootstrap_failed
move /y "%~dp0aide-launcher.exe.bootstrap" "%~dp0aide-launcher.exe" >nul
if errorlevel 1 goto bootstrap_failed
call :bootstrap_cleanup
if errorlevel 1 exit /b 1
exit /b 0

:bootstrap_failed
call :bootstrap_cleanup
echo Native bootstrap failed. No application start or Release substitution was performed.
exit /b 1

:bootstrap_cleanup
if not defined AIDE_BOOT_CID goto cleanup_builder
docker.exe container rm "%AIDE_BOOT_CID%" >nul 2>nul
if errorlevel 1 (echo Owned bootstrap container cleanup pending; preserve the CID receipt. & exit /b 1)
if exist "%AIDE_BOOT_CIDFILE%" del "%AIDE_BOOT_CIDFILE%"
:cleanup_builder
docker.exe image rm "%AIDE_BOOT_IMAGE%" >nul 2>nul
docker.exe buildx rm "%AIDE_BOOT_BUILDER%" >nul 2>nul
exit /b 0
