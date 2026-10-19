# Manifest fields are inert data; never dot-source downloaded metadata.
function Import-AidePrebuiltImage([string]$Root, [string]$Docker, [string]$Target) {
    $platformResult = Invoke-Native $Docker @('info','--format','{{.OSType}}/{{.Architecture}}') -Capture
    if ($platformResult.Code -ne 0) { throw 'Cannot inspect Docker engine platform.' }
    $platform = $platformResult.Text -replace '/aarch64$', '/arm64' -replace '/x86_64$', '/amd64'
    $manifest = Join-Path $Root 'docker/prebuilt-images.tsv'
    if (-not (Test-Path -LiteralPath $manifest)) { throw 'Prebuilt manifest missing. Download a complete Release package; source builds require AIDE_BUILD_FROM_SOURCE=1.' }
    $rows = @(Get-Content -LiteralPath $manifest | Where-Object { ($_ -split '\s+')[0] -eq $platform })
    if ($rows.Count -ne 1) { throw "No unique prebuilt image for $platform. Use a matching Release package; automatic source builds are disabled." }
    $fields = $rows[0].Trim() -split '\s+'
    if ($fields.Count -ne 6) { throw 'Invalid prebuilt manifest fields.' }
    $image=$fields[1]; $source=$fields[2]; $digest=$fields[3]; $url=$fields[4]
    if ($image -cnotmatch '^sha256:[a-f0-9]{64}$' -or $source -cnotmatch '^[a-f0-9]{64}$' -or $digest -cnotmatch '^[a-f0-9]{64}$') { throw 'Invalid prebuilt manifest identity.' }
    if ($url -cnotmatch '^https://github\.com/skyelan1999/aide/releases/download/[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+\.tar\.gz$' -or $fields[5] -ne 'bundle') { throw 'Invalid prebuilt archive URL/type.' }
    $current = Invoke-Native $Docker @('image','inspect',$image,'--format','{{.Id}}') -Capture
    if ($current.Code -ne 0 -or $current.Text -ne $image) {
        $cache=Join-Path $Root 'docker-images'; New-Item -ItemType Directory -Force $cache | Out-Null
        $archive=Join-Path $cache (($url -split '/')[-1]); $partial="$archive.partial"
        if (-not (Test-Path -LiteralPath $archive)) {
            Write-Host "Downloading complete prebuilt runtime ($platform); no compilation."
            [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
            Invoke-WebRequest -Uri $url -OutFile $partial -UseBasicParsing
            $candidate=$partial
        } else { $candidate=$archive }
        if ((Get-FileHash -LiteralPath $candidate -Algorithm SHA256).Hash.ToLowerInvariant() -ne $digest) { throw 'Prebuilt archive checksum mismatch; import refused.' }
        if ($candidate -eq $partial) { Move-Item -LiteralPath $partial -Destination $archive -Force }
        $tar = Get-Command tar.exe -ErrorAction SilentlyContinue
        if (-not $tar) { throw 'Windows tar.exe is required to import the complete offline bundle.' }
        $temporary=Join-Path $cache ('import-'+[Guid]::NewGuid().ToString('N')); New-Item -ItemType Directory $temporary | Out-Null
        try {
            if ((Invoke-Native $tar.Source @('-xzf',$archive,'-C',$temporary,'aide-offline/docker-images/aide-local.tar')).Code -ne 0) { throw 'Prebuilt image extraction failed.' }
            $inner=Join-Path $temporary 'aide-offline/docker-images/aide-local.tar'
            if ((Invoke-Native $Docker @('image','load','-i',$inner)).Code -ne 0) { throw 'Prebuilt image import failed.' }
        } finally { Remove-Item -LiteralPath $temporary -Recurse -Force }
    }
    $identity=Invoke-Native $Docker @('image','inspect',$image,'--format','{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "aide.srcsha"}}|{{index .Config.Labels "aide.cad-runtime"}}') -Capture
    if ($identity.Code -ne 0 -or $identity.Text -cne "$image|$platform|$source|8.0.1.1.0") { throw 'Imported runtime identity/platform/source/CAD mismatch; activation refused.' }
    $old=Invoke-Native $Docker @('image','inspect',$Target,'--format','{{.Id}}') -Capture
    if ($old.Code -eq 0 -and $old.Text -and $old.Text -ne $image) {
        $rollback='aide:rollback-'+(Get-Date -Format yyyyMMddHHmmss)+'-'+($old.Text -replace '^sha256:','')
        if ((Invoke-Native $Docker @('image','tag',$old.Text,$rollback)).Code -ne 0) { throw 'Cannot retain previous image for rollback.' }
    }
    if ((Invoke-Native $Docker @('image','tag',$image,$Target)).Code -ne 0) { throw 'Cannot activate prebuilt image tag.' }
    Write-Host "Prebuilt runtime ready: $Target ($platform). Source compilation disabled."
}

function Test-AideLocalRuntime([string]$Docker, [string]$Target) {
    $platform = Invoke-Native $Docker @('info','--format','{{.OSType}}/{{.Architecture}}') -Capture
    if ($platform.Code -ne 0) { return $false }
    $native = $platform.Text -replace '/aarch64$', '/arm64' -replace '/x86_64$', '/amd64'
    $metadata = Invoke-Native $Docker @('image','inspect',$Target,'--format','{{.Id}}|{{.Os}}/{{.Architecture}}|{{index .Config.Labels "aide.srcsha"}}|{{index .Config.Labels "aide.cad-runtime"}}') -Capture
    if ($metadata.Code -ne 0) { return $false }
    $parts = $metadata.Text -split '\|'
    return ($parts.Count -eq 4 -and $parts[0] -cmatch '^sha256:[a-f0-9]{64}$' -and $parts[1] -ceq $native -and $parts[2] -cmatch '^[a-f0-9]{64}$' -and $parts[3] -ceq '8.0.1.1.0')
}
