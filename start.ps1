# aide Windows 启动脚本（PowerShell 5.1+ / 7）。
# 前置：Docker Desktop（推荐 WSL2 后端）；Windows 10/11。
# 注意：compose.macos-root.yaml 仅适用于 macOS，Windows 不加载；默认 /local 绑定到用户主目录。
# 该脚本未在 Windows 真机验证（NOT_RUN），仅保证逻辑可用。
$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Error "未找到 docker，请先安装 Docker Desktop（WSL2 后端）。"
}

if (-not $env:COMPOSE_FILE) { $env:COMPOSE_FILE = 'compose.yaml' }
$Port = if ($env:AIDE_PORT) { $env:AIDE_PORT } else { '8097' }

function Test-Docker {
    docker info 2>$null | Out-Null
    return $?
}
if (-not (Test-Docker)) {
    Write-Host "Docker 未运行，尝试启动 Docker Desktop…"
    Start-Process "Docker Desktop" -ErrorAction SilentlyContinue
    for ($i = 0; $i -lt 60; $i++) { if (Test-Docker) { break }; Start-Sleep -Seconds 1 }
    if (-not (Test-Docker)) { Write-Error "Docker 引擎未能启动，请手动启动 Docker Desktop 后重试。" }
}

Write-Host "构建并启动 aide…"
docker compose up -d --build --pull never

Write-Host "等待健康检查…"
$ready = $false
for ($i = 0; $i -lt 60; $i++) {
    docker compose exec -T aide curl -fsS http://127.0.0.1:8080/healthz 2>$null | Out-Null
    if ($?) { $ready = $true; break }
    Start-Sleep -Seconds 1
}
if (-not $ready) { Write-Warning "健康检查未通过，请用 'docker compose logs aide' 排查。" }

$Token = (docker compose exec -T aide cat /data/access-token 2>$null | Out-String).Trim()
$Url = "http://localhost:$Port/#token=$Token"
Write-Host "aide 已就绪：$Url"
Start-Process $Url
