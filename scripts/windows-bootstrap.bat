@echo off
setlocal DisableDelayedExpansion
cd /d "%~dp0.."
REM First Git build bootstraps the native C++ host inside Docker.
set "AIDE_BOOT_CID="
set "AIDE_BOOT_STAGE=prepare"
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
set "AIDE_BOOT_CIDFILE=%~dp0..\.agent-state\%AIDE_BOOT_BUILDER%.cid"
if exist "%AIDE_BOOT_CIDFILE%" exit /b 1
set "AIDE_BOOT_EXE=%~dp0..\.agent-state\%AIDE_BOOT_BUILDER%.exe"
if exist "%AIDE_BOOT_EXE%" exit /b 1
docker.exe image inspect "%AIDE_BOOT_IMAGE%" >nul 2>nul
if not errorlevel 1 (echo Bootstrap image name already exists; retry. & exit /b 1)
set "AIDE_BOOT_STAGE=builder-create"
docker.exe buildx create --name "%AIDE_BOOT_BUILDER%" --driver docker-container --driver-opt memory=4g,memory-swap=4g,cpu-period=100000,cpu-quota=200000,restart-policy=no
if errorlevel 1 exit /b 1
set "AIDE_BOOT_STAGE=native-build"
echo [Aide] Compiling current native host in Docker...
docker.exe buildx build --builder "%AIDE_BOOT_BUILDER%" --load --target native-launcher --file Dockerfile --build-arg "AIDE_LAUNCHER_ARCH=%AIDE_BOOT_ARCH%" --tag "%AIDE_BOOT_IMAGE%" .
if errorlevel 1 goto bootstrap_failed
set "AIDE_BOOT_STAGE=extract-container"
docker.exe create --cidfile "%AIDE_BOOT_CIDFILE%" --network none --entrypoint /bin/true "%AIDE_BOOT_IMAGE%" >nul
if errorlevel 1 goto bootstrap_failed
REM Validate the stored CID before it can enter a command. FINDSTR patterns
REM repeating 64 character classes exceed Windows FINDSTR's search budget.
if not exist "%AIDE_BOOT_CIDFILE%" goto bootstrap_failed
for %%Z in ("%AIDE_BOOT_CIDFILE%") do if %%~zZ GTR 66 goto bootstrap_failed
set /p AIDE_BOOT_CID=<"%AIDE_BOOT_CIDFILE%"
set "AIDE_BOOT_STAGE=validate-cid"
call :validate_cid
if errorlevel 1 goto bootstrap_failed
set "AIDE_BOOT_STAGE=extract-exe"
docker.exe cp "%AIDE_BOOT_CID%:/out/aide-launcher.exe" "%AIDE_BOOT_EXE%"
if errorlevel 1 goto bootstrap_failed
set "AIDE_BOOT_STAGE=stop-owned-controller"
"%AIDE_BOOT_EXE%" host-stop --root "%CD%"
if errorlevel 1 goto bootstrap_failed
set "AIDE_BOOT_STAGE=activate-exe"
move /y "%AIDE_BOOT_EXE%" "%~dp0..\aide-launcher.exe" >nul
if errorlevel 1 goto bootstrap_failed
call :bootstrap_cleanup
if errorlevel 1 exit /b 1
exit /b 0

:bootstrap_failed
call :bootstrap_cleanup
if exist "%AIDE_BOOT_EXE%" del "%AIDE_BOOT_EXE%"
echo Native bootstrap failed at %AIDE_BOOT_STAGE%. No application start or Release substitution was performed.
if "%AIDE_BOOT_STAGE%"=="native-build" (
  echo [Aide] For network/authentication timeouts, check that your proxy is running and HTTPS_PROXY/NO_PROXY permit auth.docker.io.
  echo [Aide] Set HTTP_PROXY and HTTPS_PROXY in this CMD before build.bat, or check the Windows manual proxy settings.
  echo [Aide] BuildKit and Dockerfile RUN networking use Docker Desktop's container proxy. A host 127.0.0.1 proxy must not be copied unchanged into Linux containers.
)
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

:validate_cid
setlocal EnableDelayedExpansion
if not defined AIDE_BOOT_CID (endlocal & exit /b 1)
set "AIDE_CID_INVALID=!AIDE_BOOT_CID!"
for %%H in (0 1 2 3 4 5 6 7 8 9 a b c d e f) do if defined AIDE_CID_INVALID set "AIDE_CID_INVALID=!AIDE_CID_INVALID:%%H=!"
if defined AIDE_CID_INVALID (endlocal & exit /b 1)
if "!AIDE_BOOT_CID:~63,1!"=="" (endlocal & exit /b 1)
if not "!AIDE_BOOT_CID:~64,1!"=="" (endlocal & exit /b 1)
endlocal
exit /b 0
