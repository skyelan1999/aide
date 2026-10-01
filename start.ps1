# aide Windows 启动脚本（PowerShell 5.1+ / 7）。
# 前置：Docker Desktop（推荐 WSL2 后端）；Windows 10/11。
# 与 scripts/aide.sh start 对齐：等待引擎 → compose up -d --build → 健康检查 → 读令牌 → 打开浏览器。
# 兼容性要点：
#   1) 本机常见故障：~/.docker/config.json 缺少 cliPluginsExtraDirs，docker CLI 找不到自带 compose 插件；
#      此时自动补齐该字段（先备份），仍不可用则回退独立版 docker-compose；
#   2) Dockerfile 使用 RUN --mount=type=cache，必须启用 BuildKit（DOCKER_BUILDKIT=1）；
#   3) PS 5.1 下原生命令 stderr 重定向可能触发 NativeCommandError，取值统一走 -Capture 并用退出码判定；
#   4) /context 绑定目录（默认 ../Harness）不存在时 compose 会直接报错，故先创建；
#   5) 本文件必须以 UTF-8 BOM 保存，否则 PS 5.1 按 ANSI 解析会报语法错误。
[CmdletBinding()]
param(
    # 只做环境探测与命令解析，不实际构建/启动（便于无 Docker 环境自检）。
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

# Release bundles contain a prebuilt, architecture-specific Linux image. Use it
# directly and never invoke Compose build/pull for an offline installation.
$offlineMarker = Join-Path $PSScriptRoot '.aide-image'
$offlineBundle = Test-Path -LiteralPath $offlineMarker
if ($offlineBundle) {
    $marker = (Get-Content -Raw -LiteralPath $offlineMarker).Trim() -split '\s+'
    if ($marker.Count -lt 3) { throw '离线包 .aide-image 格式无效。' }
    $env:AIDE_IMAGE = $marker[0]
    $expectedImageId = $marker[1]
    $expectedPlatform = $marker[2]
    $env:COMPOSE_FILE = 'compose.yaml'
    if (-not $env:AIDE_WORKSPACE) { $env:AIDE_WORKSPACE = Join-Path $PSScriptRoot 'workspace' }
    if (-not $env:AIDE_CONTEXT) { $env:AIDE_CONTEXT = Join-Path $PSScriptRoot 'context' }
    if (-not $env:AIDE_LOCAL_ROOT) { $env:AIDE_LOCAL_ROOT = $env:AIDE_WORKSPACE }
}

# Compose 的默认 /local 来源使用 $HOME；Windows PowerShell 有 $HOME 自动变量，
# 但通常没有同名的进程环境变量。补齐它可避免 Compose 将空路径回退到仓库目录，
# 同时不覆盖用户在 .env 或进程环境中设置的 AIDE_LOCAL_ROOT。
if (-not $env:HOME) {
    $env:HOME = if ($env:USERPROFILE) { $env:USERPROFILE } else { $PSScriptRoot }
}

# Windows 下目录选择器需要看到当前盘的完整目录树。未在进程环境或 .env 中
# 显式收窄 AIDE_LOCAL_ROOT 时，默认把系统盘挂到 /local；项目自行配置该变量
# 时保留其边界，不擅自扩大访问范围。
$configuredLocalRoot = $false
if (Test-Path -LiteralPath (Join-Path $PSScriptRoot '.env')) {
    $configuredLocalRoot = [bool](Select-String -LiteralPath (Join-Path $PSScriptRoot '.env') -Pattern '^\s*AIDE_LOCAL_ROOT\s*=' -Quiet)
}
if (-not $env:AIDE_LOCAL_ROOT -and -not $configuredLocalRoot -and $env:USERPROFILE) {
    $systemDrive = [System.IO.Path]::GetPathRoot($env:USERPROFILE)
    if ($systemDrive) { $env:AIDE_LOCAL_ROOT = $systemDrive }
}

# 执行原生命令：-Capture 时只回收 stdout 文本，绝不把输出混进返回值；
# 不加 -Capture 时输出直通控制台（构建进度、报错原文都保留）。
function Invoke-Native {
    param(
        [Parameter(Mandatory)][string]$File,
        [string[]]$Arguments = @(),
        [switch]$Capture
    )
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        if ($Capture) {
            $text = ((& $File @Arguments 2> $null) | Out-String).Trim()
            return [pscustomobject]@{ Code = $LASTEXITCODE; Text = $text }
        }
        & $File @Arguments
        return [pscustomobject]@{ Code = $LASTEXITCODE; Text = '' }
    } finally {
        $ErrorActionPreference = $prev
    }
}

# --- 定位 docker ---
$dockerExe = $null
$dockerCmd = Get-Command docker -ErrorAction SilentlyContinue
if ($dockerCmd) {
    $dockerExe = $dockerCmd.Source
} else {
    $pf0 = if ($env:ProgramFiles) { $env:ProgramFiles } else { 'C:\Program Files' }
    foreach ($candidate in @("$pf0\Docker\Docker\resources\bin\docker.exe")) {
        if (Test-Path $candidate) { $dockerExe = $candidate; break }
    }
}
if (-not $dockerExe) {
    Write-Error "未找到 docker，请先安装 Docker Desktop（WSL2 后端）。"
}

if (-not $env:COMPOSE_FILE) { $env:COMPOSE_FILE = 'compose.yaml' }
$Port = if ($env:AIDE_PORT) { $env:AIDE_PORT } else { '8097' }

# --- 探测 Docker CLI 插件目录（compose / buildx 所在处） ---
function Get-DockerPluginDir {
    $pfp = if ($env:ProgramFiles) { $env:ProgramFiles } else { 'C:\Program Files' }
    $upp = if ($env:USERPROFILE) { $env:USERPROFILE } else { $HOME }
    $pdp = if ($env:ProgramData) { $env:ProgramData } else { 'C:\ProgramData' }
    foreach ($dir in @("$pfp\Docker\cli-plugins", "$upp\.docker\cli-plugins", "$pdp\Docker\cli-plugins")) {
        if ((Test-Path (Join-Path $dir 'docker-compose.exe')) -or (Test-Path (Join-Path $dir 'docker-compose'))) { return $dir }
    }
    return $null
}

# 仅追加 cliPluginsExtraDirs 字段（先备份），不动其它配置；
# 写文件不带 BOM，避免 Docker 的 Go JSON 解析报 invalid character。
function Repair-DockerPluginConfig {
    param([Parameter(Mandatory)][string]$PluginDir)
    $upp = if ($env:USERPROFILE) { $env:USERPROFILE } else { $HOME }
    $cfgDir = Join-Path $upp '.docker'
    $cfg = Join-Path $cfgDir 'config.json'
    if (-not (Test-Path $cfgDir)) { New-Item -ItemType Directory -Path $cfgDir -Force | Out-Null }
    $json = $null
    if (Test-Path $cfg) {
        try {
            $json = Get-Content -Raw -Path $cfg | ConvertFrom-Json
        } catch {
            Write-Warning "无法解析 $cfg（$($_.Exception.Message)），跳过自动修复。"
            return $false
        }
    }
    if (-not $json) { $json = New-Object PSObject }
    $dirs = @()
    if ($json.PSObject.Properties.Name -contains 'cliPluginsExtraDirs') { $dirs = @($json.cliPluginsExtraDirs) }
    if ($dirs -contains $PluginDir) { return $false }
    if (Test-Path $cfg) {
        Copy-Item -Path $cfg -Destination "$cfg.bak-$(Get-Date -Format yyyyMMddHHmmss)" -Force
    }
    if ($json.PSObject.Properties.Name -contains 'cliPluginsExtraDirs') {
        $json.cliPluginsExtraDirs = @($dirs + $PluginDir)
    } else {
        $json | Add-Member -NotePropertyName 'cliPluginsExtraDirs' -NotePropertyValue @($PluginDir)
    }
    $text = $json | ConvertTo-Json -Depth 20
    [System.IO.File]::WriteAllText($cfg, $text, (New-Object System.Text.UTF8Encoding $false))
    return $true
}

# --- 选定 compose 调用方式：docker compose（插件）或 docker-compose（独立版） ---
$ComposeCmd = $null
if ((Invoke-Native $dockerExe @('compose', 'version') -Capture).Code -eq 0) {
    $ComposeCmd = @($dockerExe, 'compose')
} else {
    $pluginDir = Get-DockerPluginDir
    if ($pluginDir -and $DryRun) {
        Write-Host "[DryRun] docker compose 插件未启用；正式运行会自动修复（追加 cliPluginsExtraDirs=$pluginDir）。"
    } elseif ($pluginDir) {
        Write-Host "docker compose 插件未启用，尝试修复 $env:USERPROFILE\.docker\config.json（追加 cliPluginsExtraDirs=$pluginDir）…"
        if (Repair-DockerPluginConfig -PluginDir $pluginDir) {
            Write-Host "配置已修复（原文件已备份为 config.json.bak-<时间戳>）。"
        }
        if ((Invoke-Native $dockerExe @('compose', 'version') -Capture).Code -eq 0) {
            $ComposeCmd = @($dockerExe, 'compose')
            Write-Host "docker compose 插件已恢复可用。"
        }
    }
}
if (-not $ComposeCmd) {
    $standalone = Get-Command docker-compose -ErrorAction SilentlyContinue
    if ($standalone) {
        $ComposeCmd = @($standalone.Source)
        Write-Host "未检测到 docker compose 插件，回退使用独立版 docker-compose。"
    }
}
if (-not $ComposeCmd) {
    Write-Error "未找到可用的 compose（docker compose 插件与 docker-compose 均不可用），请安装 Docker Desktop 或 docker-compose。"
}

# 两个 compose 辅助函数都不带参数，全部通过自动变量 $args 收集：
# 一旦声明了命名参数，compose 的 -c/-d/-T/--build 等短横线参数会被 PowerShell 前缀匹配到
# 同名参数（例如 `sh -c` 命中 `-Capture`）而报“参数被指定了多次”，故此处刻意不声明参数。
function Invoke-Compose {
    $all = if ($ComposeCmd.Count -gt 1) { $ComposeCmd[1..($ComposeCmd.Count - 1)] + $args } else { $args }
    return Invoke-Native -File $ComposeCmd[0] -Arguments $all
}

# 静默执行并回收输出文本与退出码（端口、令牌、健康探测等取值场景）。
function Get-ComposeResult {
    $all = if ($ComposeCmd.Count -gt 1) { $ComposeCmd[1..($ComposeCmd.Count - 1)] + $args } else { $args }
    return Invoke-Native -File $ComposeCmd[0] -Arguments $all -Capture
}

# Dockerfile 的 RUN --mount=type=cache 要求 BuildKit；独立版 compose 默认关闭。
# 双击 .bat 时 Docker 有时拿不到有效的 Windows Console handle，交互式进度渲染会
# 直接失败（"failed to get console: The handle is invalid"）。强制 plain 文本进度，
# 在正常终端和无控制台句柄的启动场景均可工作。
$env:DOCKER_BUILDKIT = '1'
$env:COMPOSE_DOCKER_CLI_BUILD = '1'
$env:BUILDKIT_PROGRESS = 'plain'
$env:COMPOSE_PROGRESS = 'plain'

# --- /context 绑定目录必须存在，否则 compose 创建容器时直接报错 ---
if (-not $env:AIDE_CONTEXT) { $env:AIDE_CONTEXT = Join-Path (Split-Path $PSScriptRoot -Parent) 'Harness' }
if (-not (Test-Path $env:AIDE_CONTEXT)) {
    if ($DryRun) {
        Write-Host "[DryRun] 将创建 context 目录：$env:AIDE_CONTEXT"
    } else {
        Write-Host "context 目录不存在，创建：$env:AIDE_CONTEXT"
        New-Item -ItemType Directory -Path $env:AIDE_CONTEXT -Force | Out-Null
    }
}

# --- 定位 Docker Desktop.exe（引擎未启动时拉起） ---
$pf = if ($env:ProgramFiles) { $env:ProgramFiles } else { 'C:\Program Files' }
$pf86 = if (${env:ProgramFiles(x86)}) { ${env:ProgramFiles(x86)} } else { 'C:\Program Files (x86)' }
$localApp = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { "$env:USERPROFILE\AppData\Local" }
$dockerDesktopExe = @(
    "$pf\Docker\Docker\Docker Desktop.exe",
    "$pf86\Docker\Docker\Docker Desktop.exe",
    "$localApp\Docker\Docker Desktop.exe"
) | Where-Object { Test-Path $_ } | Select-Object -First 1

function Test-Docker {
    return ((Invoke-Native $dockerExe @('info') -Capture).Code -eq 0)
}

if ($DryRun) {
    $pluginDirNow = Get-DockerPluginDir
    Write-Host "[DryRun] docker          = $dockerExe"
    Write-Host "[DryRun] compose         = $($ComposeCmd -join ' ')"
    Write-Host "[DryRun] CLI 插件目录     = $(if ($pluginDirNow) { $pluginDirNow } else { '<未找到>' })"
    Write-Host "[DryRun] COMPOSE_FILE    = $env:COMPOSE_FILE"
    Write-Host "[DryRun] AIDE_CONTEXT    = $env:AIDE_CONTEXT"
    Write-Host "[DryRun] host port       = $Port"
    Write-Host "[DryRun] Docker Desktop  = $(if ($dockerDesktopExe) { $dockerDesktopExe } else { '<未找到>' })"
    Write-Host "[DryRun] 引擎状态        = $(if (Test-Docker) { '运行中' } else { '未运行' })"
    exit 0
}

if (-not (Test-Docker)) {
    Write-Host "Docker 未运行，尝试启动 Docker Desktop…"
    if ($dockerDesktopExe) {
        Start-Process $dockerDesktopExe
    } else {
        Write-Warning "未定位到 Docker Desktop.exe，请手动启动 Docker Desktop。"
    }
    for ($i = 0; $i -lt 60; $i++) { if (Test-Docker) { break }; Start-Sleep -Seconds 1 }
    if (-not (Test-Docker)) { Write-Error "Docker 引擎未能启动，请手动启动 Docker Desktop 后重试。" }
}

Write-Host "构建并启动 aide…"
$startArguments = if ($offlineBundle) { @('up', '-d', '--no-build', '--pull', 'never') } else { @('up', '-d', '--build', '--pull', 'never') }
if ($offlineBundle) {
    $enginePlatform = (Invoke-Native $dockerExe @('info', '--format', '{{.OSType}}/{{.Architecture}}') -Capture).Text -replace '/aarch64$', '/arm64' -replace '/x86_64$', '/amd64'
    if ($enginePlatform -ne $expectedPlatform) { throw "此离线镜像为 $expectedPlatform，当前 Docker 引擎为 $enginePlatform。请下载匹配架构的 Release 包。" }
    $imageId = (Invoke-Native $dockerExe @('image', 'inspect', $env:AIDE_IMAGE, '--format', '{{.Id}}') -Capture).Text
    if ($imageId -ne $expectedImageId) {
        $imageArchive = Join-Path $PSScriptRoot 'docker-images/aide-local.tar.gz'
        if (-not (Test-Path -LiteralPath $imageArchive)) {
            $imageArchive = Get-ChildItem -LiteralPath (Join-Path $PSScriptRoot 'docker-images') -Filter 'aide-v*-linux-*-image.tar.gz' -File | Select-Object -First 1 -ExpandProperty FullName
        }
        if (-not $imageArchive -or -not (Test-Path -LiteralPath $imageArchive)) { throw '缺少镜像归档。请将同版本 Release 镜像附件放入 docker-images 目录。' }
        $sumFile = Join-Path $PSScriptRoot 'docker-images/SHA256SUMS'
        if (Test-Path -LiteralPath $sumFile) {
            $imageName = [System.IO.Path]::GetFileName($imageArchive)
            $expectedHash = ((Get-Content -LiteralPath $sumFile | Where-Object { $_ -match ([regex]::Escape($imageName) + '$') } | Select-Object -First 1) -split '\s+')[0]
            if ($expectedHash -and (Get-FileHash -Algorithm SHA256 -LiteralPath $imageArchive).Hash.ToLowerInvariant() -ne $expectedHash.ToLowerInvariant()) { throw 'Docker 镜像 SHA256 校验失败。' }
        }
        if ((Invoke-Native $dockerExe @('image', 'load', '-i', $imageArchive)).Code -ne 0) { throw '导入 Docker 镜像失败。' }
        $imageId = (Invoke-Native $dockerExe @('image', 'inspect', $env:AIDE_IMAGE, '--format', '{{.Id}}') -Capture).Text
        if ($imageId -ne $expectedImageId) { throw '导入的 Docker 镜像身份与离线包不匹配。' }
    }
    New-Item -ItemType Directory -Path (Join-Path $PSScriptRoot 'workspace'), (Join-Path $PSScriptRoot 'context') -Force | Out-Null
    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot '.env'))) { Copy-Item (Join-Path $PSScriptRoot '.env.example') (Join-Path $PSScriptRoot '.env') }
}
if ((Invoke-Compose @startArguments).Code -ne 0) {
    Write-Error "compose up 失败，请检查上方输出。"
}

Write-Host "等待健康检查…"
$ready = $false
for ($i = 0; $i -lt 60; $i++) {
    if ((Get-ComposeResult exec -T aide curl -fsSk https://127.0.0.1:8080/healthz).Code -eq 0) { $ready = $true; break }
    Start-Sleep -Seconds 1
}
if (-not $ready) {
    Write-Warning "健康检查未通过。容器可能仍在启动，请用 '$($ComposeCmd -join ' ') logs aide' 排查，或重新运行本脚本。"
    exit 1
}
Write-Host "健康检查通过。"

# 宿主端口以 compose 实际映射为准（AIDE_PORT 可能来自 .env）。
$address = (Get-ComposeResult port aide 8080).Text
if ($address -match ':(?<p>\d+)\s*$') { $Port = $Matches['p'] }

# 令牌：#31 数据分层后在 /data/auth/access-token，兼容旧路径；读不到不视为失败。
$token = (Get-ComposeResult exec -T aide sh -c 'cat /data/auth/access-token 2>/dev/null || cat /data/access-token 2>/dev/null').Text

$url = if ($token) { "https://localhost:$Port/#token=$token" } else { "https://localhost:$Port/" }
Write-Host "aide 已就绪：$url （自签证书，浏览器告警请选 继续/高级→仍要访问）"
if (-not $token) {
    Write-Host "未自动读到令牌，已打开登录页；可运行：$($ComposeCmd -join ' ') exec -T aide cat /data/auth/access-token"
}
if ($env:AIDE_OPEN_BROWSER -ne '0') { Start-Process $url }
