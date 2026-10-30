@echo off
REM Called within the entrypoint SETLOCAL, before the first native EXE exists.
REM Explicit environment is opaque: never interpolate or print its values.
if defined HTTP_PROXY goto explicit
if defined HTTPS_PROXY goto explicit
if defined ALL_PROXY goto explicit
setlocal DisableDelayedExpansion
set "AIDE_PROXY_ENABLED="
set "AIDE_PROXY_SERVER="
set "AIDE_PROXY_BYPASS="
set "AIDE_PROXY_PAC="
where reg.exe >nul 2>nul
if errorlevel 1 goto registry_missing
for /f "tokens=1,2,*" %%A in ('reg.exe query "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings" /v ProxyEnable 2^>nul') do if /i "%%A"=="ProxyEnable" if "%%C"=="0x1" set "AIDE_PROXY_ENABLED=1"
for /f "tokens=1,2,*" %%A in ('reg.exe query "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings" /v ProxyServer 2^>nul') do if /i "%%A"=="ProxyServer" set "AIDE_PROXY_SERVER=%%C"
for /f "tokens=1,2,*" %%A in ('reg.exe query "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings" /v ProxyOverride 2^>nul') do if /i "%%A"=="ProxyOverride" set "AIDE_PROXY_BYPASS=%%C"
for /f "tokens=1,2,*" %%A in ('reg.exe query "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings" /v AutoConfigURL 2^>nul') do if /i "%%A"=="AutoConfigURL" set "AIDE_PROXY_PAC=%%C"
setlocal EnableDelayedExpansion
if defined AIDE_PROXY_ENABLED goto manual
if defined AIDE_PROXY_PAC goto pac
echo [Aide] Build client proxy: no explicit or manual proxy; Docker Desktop networking is unchanged.
endlocal
endlocal
exit /b 0
:manual
if not defined AIDE_PROXY_SERVER goto unsupported
if not "!AIDE_PROXY_SERVER:~2048,1!"=="" goto unsupported
REM Only this small manual-address grammar is exported through ENDLOCAL.
REM Do not CALL/ECHO raw registry contents or proxy URLs containing credentials.
set "AIDE_PROXY_CHECK=!AIDE_PROXY_SERVER!"
call :strip_address_chars
set "AIDE_PROXY_UNSAFE="
for /f "tokens=* delims=;= " %%A in ("!AIDE_PROXY_CHECK!") do set "AIDE_PROXY_UNSAFE=1"
if defined AIDE_PROXY_UNSAFE goto unsupported
set "AIDE_PROXY_HTTP="
set "AIDE_PROXY_HTTPS="
set "AIDE_PROXY_REST=!AIDE_PROXY_SERVER!"
:server_loop
set "AIDE_PROXY_ITEM="
for /f "tokens=1,* delims=;" %%A in ("!AIDE_PROXY_REST!") do (set "AIDE_PROXY_ITEM=%%A" & set "AIDE_PROXY_REST=%%B")
set "AIDE_PROXY_TRIM=!AIDE_PROXY_ITEM!"
call :trim_spaces
set "AIDE_PROXY_ITEM=!AIDE_PROXY_TRIM!"
if not defined AIDE_PROXY_ITEM if defined AIDE_PROXY_REST goto server_loop
if not defined AIDE_PROXY_ITEM goto server_done
call :server_item
if errorlevel 1 goto unsupported
if defined AIDE_PROXY_REST goto server_loop
:server_done
if not defined AIDE_PROXY_HTTP if not defined AIDE_PROXY_HTTPS goto unsupported
set "AIDE_PROXY_NO="
set "AIDE_PROXY_WARN="
if defined NO_PROXY goto export
if not defined AIDE_PROXY_BYPASS goto export
if not "!AIDE_PROXY_BYPASS:~2048,1!"=="" goto bypass_warning
set "AIDE_PROXY_REST=!AIDE_PROXY_BYPASS!"
:bypass_loop
set "AIDE_PROXY_ITEM="
for /f "tokens=1,* delims=;" %%A in ("!AIDE_PROXY_REST!") do (set "AIDE_PROXY_ITEM=%%A" & set "AIDE_PROXY_REST=%%B")
set "AIDE_PROXY_TRIM=!AIDE_PROXY_ITEM!"
call :trim_spaces
set "AIDE_PROXY_ITEM=!AIDE_PROXY_TRIM!"
if not defined AIDE_PROXY_ITEM if defined AIDE_PROXY_REST goto bypass_loop
if not defined AIDE_PROXY_ITEM goto export
if /i "!AIDE_PROXY_ITEM!"=="<local>" (
  set "AIDE_PROXY_ITEM=localhost,127.0.0.1,[::1]"
  set "AIDE_PROXY_WARN=1"
  goto bypass_append
)
set "AIDE_PROXY_CHECK=!AIDE_PROXY_ITEM!"
if "!AIDE_PROXY_ITEM!"=="*" set "AIDE_PROXY_CHECK="
if "!AIDE_PROXY_ITEM:~0,2!"=="*." set "AIDE_PROXY_CHECK=!AIDE_PROXY_ITEM:~2!"
call :strip_address_chars
if defined AIDE_PROXY_CHECK (set "AIDE_PROXY_WARN=1" & goto bypass_next)
:bypass_append
if defined AIDE_PROXY_NO set "AIDE_PROXY_NO=!AIDE_PROXY_NO!,"
set "AIDE_PROXY_NO=!AIDE_PROXY_NO!!AIDE_PROXY_ITEM!"
:bypass_next
if defined AIDE_PROXY_REST goto bypass_loop
goto export
:bypass_warning
set "AIDE_PROXY_WARN=1"
:export
echo [Aide] Build client proxy: Windows manual settings (values hidden).
if defined AIDE_PROXY_WARN echo [Aide] Windows bypass syntax differs from NO_PROXY. Set NO_PROXY explicitly for short hostnames or unsupported wildcard rules.
REM Only validated address/bypass characters can reach this expansion.
endlocal & endlocal & set "HTTP_PROXY=%AIDE_PROXY_HTTP%" & set "HTTPS_PROXY=%AIDE_PROXY_HTTPS%" & if not defined NO_PROXY set "NO_PROXY=%AIDE_PROXY_NO%"
exit /b 0
:server_item
set "AIDE_PROXY_PROTOCOL="
set "AIDE_PROXY_VALUE="
for /f "tokens=1,* delims==" %%A in ("!AIDE_PROXY_ITEM!") do (set "AIDE_PROXY_PROTOCOL=%%A" & set "AIDE_PROXY_VALUE=%%B")
if not defined AIDE_PROXY_VALUE (
  if defined AIDE_PROXY_HTTP exit /b 1
  if defined AIDE_PROXY_HTTPS exit /b 1
  set "AIDE_PROXY_VALUE=!AIDE_PROXY_ITEM!"
  call :endpoint
  if errorlevel 1 exit /b 1
  set "AIDE_PROXY_HTTP=!AIDE_PROXY_VALUE!"
  set "AIDE_PROXY_HTTPS=!AIDE_PROXY_VALUE!"
  exit /b 0
)
set "AIDE_PROXY_TRIM=!AIDE_PROXY_PROTOCOL!"
call :trim_spaces
set "AIDE_PROXY_PROTOCOL=!AIDE_PROXY_TRIM!"
if /i "!AIDE_PROXY_PROTOCOL!"=="http" (
  if defined AIDE_PROXY_HTTP exit /b 1
  call :endpoint
  if errorlevel 1 exit /b 1
  set "AIDE_PROXY_HTTP=!AIDE_PROXY_VALUE!"
)
if /i "!AIDE_PROXY_PROTOCOL!"=="https" (
  if defined AIDE_PROXY_HTTPS exit /b 1
  call :endpoint
  if errorlevel 1 exit /b 1
  set "AIDE_PROXY_HTTPS=!AIDE_PROXY_VALUE!"
)
exit /b 0
:endpoint
set "AIDE_PROXY_TRIM=!AIDE_PROXY_VALUE!"
call :trim_spaces
set "AIDE_PROXY_VALUE=!AIDE_PROXY_TRIM!"
if not defined AIDE_PROXY_VALUE exit /b 1
set "AIDE_PROXY_HOST=!AIDE_PROXY_VALUE!"
if /i "!AIDE_PROXY_HOST:~0,7!"=="http://" (set "AIDE_PROXY_HOST=!AIDE_PROXY_HOST:~7!" & goto host)
if /i "!AIDE_PROXY_HOST:~0,8!"=="https://" (set "AIDE_PROXY_HOST=!AIDE_PROXY_HOST:~8!" & goto host)
set "AIDE_PROXY_VALUE=http://!AIDE_PROXY_VALUE!"
:host
if "!AIDE_PROXY_HOST:~-1!"=="/" set "AIDE_PROXY_HOST=!AIDE_PROXY_HOST:~0,-1!"
if not defined AIDE_PROXY_HOST exit /b 1
set "AIDE_PROXY_CHECK=!AIDE_PROXY_HOST!"
call :strip_address_chars
REM A slash is allowed only in the scheme and an optional final slash.
if not "!AIDE_PROXY_HOST!"=="!AIDE_PROXY_HOST:/=!" exit /b 1
if defined AIDE_PROXY_CHECK exit /b 1
exit /b 0
:strip_address_chars
for %%H in (0 1 2 3 4 5 6 7 8 9 a b c d e f g h i j k l m n o p q r s t u v w x y z A B C D E F G H I J K L M N O P Q R S T U V W X Y Z - . _ : [ ] /) do if defined AIDE_PROXY_CHECK set "AIDE_PROXY_CHECK=!AIDE_PROXY_CHECK:%%H=!"
exit /b 0
:trim_spaces
if not defined AIDE_PROXY_TRIM exit /b 0
for /f "tokens=* delims= " %%A in ("!AIDE_PROXY_TRIM!") do set "AIDE_PROXY_TRIM=%%A"
:trim_tail
if not defined AIDE_PROXY_TRIM exit /b 0
if "!AIDE_PROXY_TRIM:~-1!"==" " (set "AIDE_PROXY_TRIM=!AIDE_PROXY_TRIM:~0,-1!" & goto trim_tail)
exit /b 0
:pac
echo [Aide] Windows PAC proxy detected. PAC/WPAD is not evaluated by the build client.
goto instructions
:unsupported
echo [Aide] Windows manual proxy format is unsupported; no registry value was executed or printed.
:instructions
echo [Aide] Set HTTP_PROXY and HTTPS_PROXY to your HTTP proxy in this CMD, then call build.bat.
echo [Aide] Use your actual proxy address and port. No system settings were changed.
endlocal
endlocal
exit /b 1
:registry_missing
echo [Aide] Cannot query Windows proxy settings. Set HTTP_PROXY and HTTPS_PROXY explicitly before build.bat.
endlocal
exit /b 1
:explicit
echo [Aide] Build client proxy: explicit environment (values hidden).
exit /b 0
