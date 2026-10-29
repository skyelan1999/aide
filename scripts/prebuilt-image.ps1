# Manifest fields are inert data; never dot-source downloaded metadata.
# Extract one regular tar member to an owned file, never arbitrary archive paths.
# Uses APIs present in Windows PowerShell 5.1; no external tar.exe is executed.
function Read-AideTarBytes($Stream, [byte[]]$Buffer, [int]$Count) {
    $done=0
    while ($done -lt $Count) {
        $read=$Stream.Read($Buffer,$done,$Count-$done)
        if ($read -eq 0) { if ($done -eq 0) { return 0 }; throw 'Truncated prebuilt tar data.' }
        $done+=$read
    }
    return $done
}
function Get-AideTarNumber([byte[]]$Header, [int]$Offset, [int]$Length) {
    if ($Header[$Offset] -band 128) {
        if ($Header[$Offset] -band 64) { throw 'Negative prebuilt tar size.' }
        [long]$number=$Header[$Offset] -band 63
        for ($i=1;$i -lt $Length;$i++) {
            if ($number -gt 36028797018963967) { throw 'Prebuilt tar size overflow.' }
            $number=($number -shl 8) -bor $Header[$Offset+$i]
        }
        return $number
    }
    $text=[Text.Encoding]::ASCII.GetString($Header,$Offset,$Length).Trim([char[]]@([char]0,[char]32))
    if (-not $text) { return [long]0 }
    if ($text -notmatch '^[0-7]+$') { throw 'Invalid prebuilt tar number.' }
    return [Convert]::ToInt64($text,8)
}
function Expand-AidePrebuiltImage([string]$Archive, [string]$Destination) {
    $inputFile=$null;$gzip=$null
    try {
        $inputFile=[IO.File]::OpenRead($Archive)
        $gzip=New-Object IO.Compression.GZipStream($inputFile,[IO.Compression.CompressionMode]::Decompress)
        $header=New-Object byte[] 512
        $buffer=New-Object byte[] 1048576
        $globalPax=@{};$nextPax=@{}
        while ((Read-AideTarBytes $gzip $header 512) -ne 0) {
            [long]$sum=0;[long]$rawSum=0
            for ($i=0;$i -lt 512;$i++) { $rawSum+=$header[$i];if ($i -ge 148 -and $i -lt 156) { $sum+=32 } else { $sum+=$header[$i] } }
            if ($rawSum -eq 0) { break }
            if ((Get-AideTarNumber $header 148 8) -ne $sum) { throw 'Prebuilt tar header checksum mismatch.' }
            [long]$size=Get-AideTarNumber $header 124 12
            $name=[Text.Encoding]::UTF8.GetString($header,0,100).Split([char]0)[0]
            $prefix=''
            if ([Text.Encoding]::ASCII.GetString($header,257,6) -ceq ('ustar'+[char]0)) { $prefix=[Text.Encoding]::UTF8.GetString($header,345,155).Split([char]0)[0] }
            if ($prefix) { $name=$prefix+'/'+$name }
            $type=$header[156]
            $metadata=$type -in @(120,103,76,75) # PAX local/global, GNU path/link
            if ($metadata) {
                if ($size -gt 65536) { throw 'Prebuilt tar metadata exceeds 64KiB.' }
                $bytes=New-Object byte[] ([int]$size)
                if ($size -and (Read-AideTarBytes $gzip $bytes ([int]$size)) -ne $size) { throw 'Truncated prebuilt tar metadata.' }
                if ($type -eq 76) { $nextPax['path']=[Text.Encoding]::UTF8.GetString($bytes).TrimEnd([char[]]@([char]0,[char]10)) }
                elseif ($type -in @(120,103)) {
                    $pax=if ($type -eq 103) { $globalPax } else { $nextPax }
                    $offset=0
                    while ($offset -lt $bytes.Length) {
                        $space=$offset
                        while ($space -lt $bytes.Length -and $bytes[$space] -ne 32 -and $space-$offset -lt 8) { $space++ }
                        $digits=[Text.Encoding]::ASCII.GetString($bytes,$offset,$space-$offset)
                        $length=0
                        if ($space -ge $bytes.Length -or $digits -notmatch '^[0-9]+$' -or -not [int]::TryParse($digits,[ref]$length) -or $length -le $space-$offset+2 -or $length -gt $bytes.Length-$offset -or $bytes[$offset+$length-1] -ne 10) { throw 'Invalid prebuilt PAX record.' }
                        $record=[Text.Encoding]::UTF8.GetString($bytes,$space+1,$offset+$length-$space-2)
                        $equals=$record.IndexOf('=')
                        if ($equals -lt 1) { throw 'Invalid prebuilt PAX attribute.' }
                        $key=$record.Substring(0,$equals)
                        if ($key -in @('path','size')) { $pax[$key]=$record.Substring($equals+1) }
                        $offset+=$length
                    }
                }
            } else {
                foreach ($pax in @($globalPax,$nextPax)) {
                    if ($pax.ContainsKey('path')) { $name=$pax['path'] }
                    if ($pax.ContainsKey('size')) {
                        $parsed=[long]0
                        if ($pax['size'] -notmatch '^[0-9]+$' -or -not [long]::TryParse($pax['size'],[ref]$parsed)) { throw 'Invalid prebuilt PAX size.' }
                        $size=$parsed
                    }
                }
                $nextPax=@{}
                while ($name.StartsWith('./')) { $name=$name.Substring(2) }
                $wanted=$name -ceq 'aide-offline/docker-images/aide-local.tar'
                if ($wanted -and $type -notin @(0,48)) { throw 'Prebuilt image member must be a regular file.' }
                $output=$null
                try {
                    if ($wanted) { $output=[IO.File]::Open($Destination,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None) }
                    [long]$remaining=$size
                    while ($remaining -gt 0) {
                        $read=$gzip.Read($buffer,0,[int][Math]::Min([long]$buffer.Length,$remaining))
                        if ($read -eq 0) { throw 'Truncated prebuilt image member.' }
                        if ($null -ne $output) { $output.Write($buffer,0,$read) }
                        $remaining-=$read
                    }
                } finally { if ($null -ne $output) { $output.Dispose() } }
                if ($wanted) { return }
            }
            $padding=[int]((512-($size % 512)) % 512)
            if ($padding -and (Read-AideTarBytes $gzip $buffer $padding) -ne $padding) { throw 'Truncated prebuilt tar padding.' }
        }
        throw 'Prebuilt image member was not found.'
    } finally { if ($null -ne $gzip) { $gzip.Dispose() };if ($null -ne $inputFile) { $inputFile.Dispose() } }
}
function Get-AideDockerImageInfo([string]$Docker, [string]$Reference) {
    # PS5.1 can drop quotes inside a Go-template argument passed to native
    # Docker. Parse one JSON object; label keys stay inside PowerShell instead.
    $result=Invoke-Native $Docker @('image','inspect',$Reference,'--format','{{json .}}') -Capture
    if ($result.Code -ne 0) { return [pscustomobject]@{Code=$result.Code;Image=$null} }
    try {
        $text=$result.Text.Trim()
        if (-not $text.StartsWith('{') -or -not $text.EndsWith('}')) { throw 'Expected object' }
        $metadata=ConvertFrom-Json -InputObject $text -ErrorAction Stop
        return [pscustomobject]@{Code=0;Image=$metadata}
    } catch { return [pscustomobject]@{Code=1;Image=$null} }
}
function Get-AideDockerImageIdentity([string]$Docker, [string]$Reference) {
    $result=Get-AideDockerImageInfo $Docker $Reference
    if ($result.Code -ne 0) { return [pscustomobject]@{Code=1;Text=''} }
    $m=$result.Image
    if ($m.Id -isnot [string] -or $m.Os -isnot [string] -or $m.Architecture -isnot [string]) { return [pscustomobject]@{Code=1;Text=''} }
    $source=$m.Config.Labels.'aide.srcsha';$cad=$m.Config.Labels.'aide.cad-runtime'
    if (($null -ne $source -and $source -isnot [string]) -or ($null -ne $cad -and $cad -isnot [string])) { return [pscustomobject]@{Code=1;Text=''} }
    return [pscustomobject]@{Code=0;Text=($m.Id+'|'+$m.Os+'/'+$m.Architecture+'|'+$source+'|'+$cad)}
}
function Confirm-AidePrebuiltIdentity($Metadata, [string]$Platform, [string]$Source) {
    if ($Metadata.Code -ne 0) { throw 'Imported runtime identity unavailable; activation refused.' }
    $parts=$Metadata.Text -split '\|'
    if ($parts.Count -ne 4 -or $parts[0] -cnotmatch '^sha256:[a-f0-9]{64}$') { throw 'Imported runtime identity malformed; activation refused.' }
    if ($parts[1] -cne $Platform) { throw 'Imported runtime platform mismatch; activation refused.' }
    if ($parts[2] -cne $Source) { throw 'Imported runtime source fingerprint mismatch; activation refused.' }
    if ($parts[3] -cne '8.0.1.1.0') { throw 'Imported legacy runtime CAD mismatch; activation refused.' }
    return $parts[0]
}
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
    if ($Target -cnotmatch '^[A-Za-z0-9][A-Za-z0-9._/:@-]*$') { throw 'Invalid prebuilt target image.' }
    $identity=Get-AideDockerImageIdentity $Docker $image
    $loaded=$null
    if ($identity.Code -ne 0) {
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
        $temporary=Join-Path $cache ('import-'+[Guid]::NewGuid().ToString('N')); New-Item -ItemType Directory $temporary | Out-Null
        try {
            $inner=Join-Path $temporary 'aide-local.tar'
            Expand-AidePrebuiltImage $archive $inner
            $loaded=Invoke-Native $Docker @('image','load','-i',$inner) -Capture
            if ($loaded.Code -ne 0) { throw 'Prebuilt image import failed.' }
        } finally { Remove-Item -LiteralPath $temporary -Recurse -Force }
    }
    if ($null -ne $loaded) {
        $identity=Get-AideDockerImageIdentity $Docker $image
        if ($identity.Code -ne 0) {
            # A locator is trusted only when emitted by this successful load
            # of the SHA256-verified archive. Never scan unrelated local tags.
            $references=@($loaded.Text -split '\r?\n' | ForEach-Object {
                if ($_ -cmatch '^Loaded image(?: ID)?: ([A-Za-z0-9][A-Za-z0-9._:/@-]*)\s*$') { $Matches[1] }
            } | Sort-Object -Unique)
            if ($references.Count -ne 1) { throw 'Cannot uniquely resolve this verified prebuilt import; activation refused.' }
            $identity=Get-AideDockerImageIdentity $Docker $references[0]
        }
    }
    # Docker Desktop/containerd and classic engines can expose different image
    # identities for the same pinned manifest. Retain the canonical .Id only
    # after resolving the pinned immutable reference (or this verified load).
    $resolvedImage=Confirm-AidePrebuiltIdentity $identity $platform $source
    $old=Invoke-Native $Docker @('image','inspect',$Target,'--format','{{.Id}}') -Capture
    if ($old.Code -eq 0 -and $old.Text -and $old.Text -ne $resolvedImage) {
        $rollback='aide:rollback-'+(Get-Date -Format yyyyMMddHHmmss)+'-'+($old.Text -replace '^sha256:','')
        if ((Invoke-Native $Docker @('image','tag',$old.Text,$rollback)).Code -ne 0) { throw 'Cannot retain previous image for rollback.' }
    }
    if ((Invoke-Native $Docker @('image','tag',$resolvedImage,$Target)).Code -ne 0) { throw 'Cannot activate prebuilt image tag.' }
    Write-Host "Prebuilt runtime ready: $Target ($platform). Source compilation disabled."
}

function Test-AideLocalRuntime([string]$Docker, [string]$Target) {
    $platform = Invoke-Native $Docker @('info','--format','{{.OSType}}/{{.Architecture}}') -Capture
    if ($platform.Code -ne 0) { return $false }
    $native = $platform.Text -replace '/aarch64$', '/arm64' -replace '/x86_64$', '/amd64'
    $metadata = Get-AideDockerImageIdentity $Docker $Target
    if ($metadata.Code -ne 0) { return $false }
    $parts = $metadata.Text -split '\|'
    return ($parts.Count -eq 4 -and $parts[0] -cmatch '^sha256:[a-f0-9]{64}$' -and $parts[1] -ceq $native -and $parts[2] -cmatch '^[a-f0-9]{64}$')
}
