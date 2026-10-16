# Dot-sourced by start.ps1. Configuration is data, never an executable script.
function Assert-AideRealPath([string]$Path) {
    if ((Test-Path -LiteralPath $Path) -and ((Get-Item -Force -LiteralPath $Path).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Persistent configuration cannot be a link or junction.' }
}
function Restore-AideLauncherEnvironment([string]$Root) {
    $persistentRoot = Join-Path $Root '.aide-data'
    Assert-AideRealPath $persistentRoot
    if (-not (Test-Path -LiteralPath $persistentRoot)) {
        New-Item -ItemType Directory -Path $persistentRoot | Out-Null
        if ($env:OS -eq 'Windows_NT') {
            $acl = New-Object Security.AccessControl.DirectorySecurity
            $acl.SetAccessRuleProtection($true, $false)
            $identity = [Security.Principal.WindowsIdentity]::GetCurrent().User
            $rule = New-Object Security.AccessControl.FileSystemAccessRule($identity, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
            $acl.AddAccessRule($rule)
            Set-Acl -LiteralPath $persistentRoot -AclObject $acl
        }
    }
    foreach ($name in @('.env','.aide-data/launcher.env','.aide-data/compose-project')) { Assert-AideRealPath (Join-Path $Root $name) }
    $environmentPath = Join-Path $Root '.env'
    $backup = Join-Path $persistentRoot 'launcher.env'
    if (-not (Test-Path -LiteralPath $environmentPath) -and (Test-Path -LiteralPath $backup)) { Copy-Item -LiteralPath $backup -Destination $environmentPath }
    foreach ($name in @('.aide-image','.aide-data/image')) { Assert-AideRealPath (Join-Path $Root $name) }
    if (-not (Test-Path -LiteralPath (Join-Path $Root '.aide-image')) -and (Test-Path -LiteralPath (Join-Path $persistentRoot 'image'))) { Copy-Item -LiteralPath (Join-Path $persistentRoot 'image') -Destination (Join-Path $Root '.aide-image') }
    $projectFile = Join-Path $persistentRoot 'compose-project'
    if (-not $env:COMPOSE_PROJECT_NAME -and (Test-Path -LiteralPath $projectFile)) {
        $project = ([IO.File]::ReadAllText($projectFile)).Trim()
        if ($project -notmatch '^[a-z0-9][a-z0-9_-]*$') { throw 'Invalid stored Compose identity.' }
        $env:COMPOSE_PROJECT_NAME = $project
    }
}
function Save-AideLauncherEnvironment([string]$Root) {
    $environmentPath = Join-Path $Root '.env'
    if (Test-Path -LiteralPath $environmentPath) {
        $temporary = Join-Path $Root ('.aide-data/launcher-env.' + [Guid]::NewGuid().ToString('N'))
        Copy-Item -LiteralPath $environmentPath -Destination $temporary
        Move-Item -LiteralPath $temporary -Destination (Join-Path $Root '.aide-data/launcher.env') -Force
    }
    if (Test-Path -LiteralPath (Join-Path $Root '.aide-image')) { Copy-Item -LiteralPath (Join-Path $Root '.aide-image') -Destination (Join-Path $Root '.aide-data/image') -Force }
}
function Invoke-AideDataDocker([string]$Docker, [string[]]$Arguments) {
    $previous = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    try { $text = ((& $Docker @Arguments 2>$null) | Out-String).Trim(); return [pscustomobject]@{Code=$LASTEXITCODE;Text=$text} }
    finally { $ErrorActionPreference = $previous }
}
function Select-AideLegacyOwner([string]$Docker, [string[]]$Owners, [string]$Project, [bool]$ExplicitProject) {
    $eligibleOwners = @(); $selectedOwners = @()
    foreach ($candidateId in $Owners) {
        $result = Invoke-AideDataDocker $Docker @('inspect','--format','{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.oneoff"}}',$candidateId)
        if ($result.Code -ne 0) { throw 'Cannot inspect legacy container identity.' }
        $fields = $result.Text -split '\|', 2
        if ($fields.Count -eq 2 -and $fields[1] -in @('True','true','1')) { continue }
        $eligibleOwners += $candidateId
        if ($fields[0] -eq $Project) { $selectedOwners += $candidateId }
    }
    if ($selectedOwners.Count -eq 1) { return $selectedOwners[0] }
    if ($selectedOwners.Count -eq 0 -and $eligibleOwners.Count -eq 1 -and -not $ExplicitProject) { return $eligibleOwners[0] }
    if ($eligibleOwners.Count -eq 0) { return '' }
    throw "Cannot select legacy data owner for Compose project '$Project' ($($selectedOwners.Count) matching, $($eligibleOwners.Count) eligible containers). No container stopped or data imported. Check COMPOSE_PROJECT_NAME and container labels."
}
function Prepare-AideLauncherData([string]$Root, [string]$Docker, [string]$Image) {
    $persistentRoot = Join-Path $Root '.aide-data'
    $mountArguments = @('-v', "${persistentRoot}:/persist", '-v', ((Join-Path $Root 'scripts/launcher-data.py') + ':/tool.py:ro'))
    $importArguments = @()
    $ready = Test-Path -LiteralPath (Join-Path $persistentRoot 'storage/migration.json')
    if (-not $ready -and (Test-Path -LiteralPath (Join-Path $persistentRoot 'storage'))) { throw 'Existing storage has no receipt; import refused before stopping anything.' }
    $project = $env:COMPOSE_PROJECT_NAME
    $stopped = $false
    if (-not $ready) {
        $explicitProject = [bool]$project
        if (-not $project -and (Test-Path -LiteralPath (Join-Path $Root '.env'))) {
            $line = Select-String -LiteralPath (Join-Path $Root '.env') -Pattern '^COMPOSE_PROJECT_NAME=(.*)$' | Select-Object -First 1
            if ($line) { $project = $line.Matches[0].Groups[1].Value.Trim().Trim('"').Trim("'") }
        }
        if ($project) { $explicitProject = $true }
        if (-not $project) {
            $line = Select-String -LiteralPath (Join-Path $Root 'compose.yaml') -Pattern '^name:\s*(\S+)' | Select-Object -First 1
            $project = if ($line) { $line.Matches[0].Groups[1].Value } else { 'aide' }
        }
        $found = Invoke-AideDataDocker $Docker @('ps','-aq','--filter',"label=com.docker.compose.project.working_dir=$Root",'--filter','label=com.docker.compose.service=aide')
        if ($found.Code -ne 0) { throw 'Cannot inspect legacy containers.' }
        $owners = @($found.Text -split '\r?\n' | Where-Object { $_ })
        $owner = Select-AideLegacyOwner $Docker $owners $project $explicitProject
        if ($owner) {
            $result = Invoke-AideDataDocker $Docker @('inspect','--format','{{index .Config.Labels "com.docker.compose.project"}}',$owner)
            if ($result.Code -ne 0) { throw 'Cannot inspect legacy project.' }; $project = $result.Text
        }
        if ($project -notmatch '^[a-z0-9][a-z0-9_-]*$') { throw 'Invalid Compose identity.' }
        $env:COMPOSE_PROJECT_NAME = $project
        $mounts = @()
        if ($owner) {
            $result = Invoke-AideDataDocker $Docker @('inspect','--format','{{json .Mounts}}',$owner)
            if ($result.Code -ne 0) { throw 'Cannot inspect legacy mounts.' }; $mounts = @($result.Text | ConvertFrom-Json)
        }
        foreach ($target in @('data','home')) {
            $destination = if ($target -eq 'data') { '/data' } else { '/home/aide' }
            $mount = @($mounts | Where-Object { $_.Destination -eq $destination }) | Select-Object -First 1
            $legacySource = ''; $legacyType = ''
            if ($mount) { $legacyType=$mount.Type; $legacySource=if ($mount.Type -eq 'volume') { $mount.Name } else { $mount.Source } }
            else {
                $volume = "${project}_aide-$target"
                if ((Invoke-AideDataDocker $Docker @('volume','inspect',$volume)).Code -eq 0) { $legacyType='volume';$legacySource=$volume }
            }
            if (-not $legacySource -and $target -eq 'data' -and (Test-Path -LiteralPath (Join-Path $Root '.data'))) { $legacyType='bind';$legacySource=Join-Path $Root '.data' }
            if ($legacySource) {
                if ($legacyType -eq 'volume') {
                    $users = Invoke-AideDataDocker $Docker @('ps','-q','--filter',"volume=$legacySource")
                    if ($users.Code -ne 0 -or ($users.Text -and $users.Text -ne $owner)) { throw 'Legacy volume is used by another container; import refused.' }
                }
                $mountArguments += @('-v',"${legacySource}:/legacy/${target}:ro");$importArguments += @("--$target","/legacy/$target")
            }
        }
        if ($owner) {
            $running = Invoke-AideDataDocker $Docker @('inspect','--format','{{.State.Running}}',$owner)
            if ($running.Code -ne 0) { throw 'Cannot inspect legacy runtime.' }
            if ($running.Text -eq 'true') {
                $preflight = 'import json,pathlib,sys; terminal={"completed","failed","cancelled","canceled","stopped","error","done","resumed",""}; active=any(r and r.get("status","") not in terminal for p in pathlib.Path("/data/sessions").rglob("*.json") for r in (json.loads(p.read_text()).get("runs") or [])); sys.exit("Active or suspended work exists; finish it before data migration" if active else 0)'
                if ((Invoke-AideDataDocker $Docker @('exec','-w','/tmp',$owner,'python3','-c',$preflight)).Code -ne 0) { throw 'Active or unreadable legacy sessions; import refused.' }
                New-Item -ItemType Directory -Path (Join-Path $Root '.cache') -Force | Out-Null
                New-Item -ItemType File -Path (Join-Path $Root '.cache/aide-manual-stop') -Force | Out-Null
                if ((Invoke-AideDataDocker $Docker @('stop','--time','30',$owner)).Code -ne 0) { throw 'Could not stop legacy runtime.' }
                $stopped = $true
            }
        }
    }
    $arguments = @('run','--rm','--network','none','--user','0:0') + $mountArguments + @('--entrypoint','python3',$Image,'/tool.py','--platform','windows','--seed-home') + $importArguments
    $result = Invoke-AideDataDocker $Docker $arguments
    if ($result.Code -ne 0 -or -not (Test-Path -LiteralPath (Join-Path $persistentRoot 'storage/migration.json'))) {
        if ($stopped) {
            $rollback = Invoke-AideDataDocker $Docker @('start',$owner)
            if ($rollback.Code -eq 0) { Remove-Item -LiteralPath (Join-Path $Root '.cache/aide-manual-stop') -ErrorAction SilentlyContinue }
            else { Write-Warning 'Previous container could not restart; inspect it before retrying.' }
        }
        throw 'Persistent import failed; old data and staging retained.'
    }
    if ($project) { [IO.File]::WriteAllText((Join-Path $persistentRoot 'compose-project'), $project + "`n", (New-Object Text.UTF8Encoding $false)) }
    Save-AideLauncherEnvironment $Root
}
