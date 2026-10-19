$ErrorActionPreference='Stop'
# Exercise the production argument builder without starting Docker or the bridge.
$parseErrors=$null
$tokens=$null
$ast=[System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'message-lan-control.ps1'),[ref]$tokens,[ref]$parseErrors)
if ($parseErrors.Count) { throw 'LAN launcher syntax error.' }
$query=$ast.Find({param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Get-LanCompose'},$true)
if (-not $query) { throw 'Compose query function missing.' }
. ([scriptblock]::Create($query.Extent.Text))
function Test-LanDocker {
    $script:capturedArgs=@($args)
    $global:LASTEXITCODE=0
    'container-id'
}
foreach ($case in @(
    @{Command=@('Test-LanDocker','compose'); Expected=@('compose','ps','-q','aide')},
    @{Command=@('Test-LanDocker'); Expected=@('ps','-q','aide')},
    @{Command=@('Test-LanDocker','compose','--project-name','aide'); Expected=@('compose','--project-name','aide','ps','-q','aide')}
)) {
    $ComposeCommand=$case.Command
    $result=Get-LanCompose -Items @('ps','-q','aide')
    if ($result -ne 'container-id' -or ($script:capturedArgs -join '|') -cne ($case.Expected -join '|')) {
        throw ('Incorrect Compose arguments: '+($script:capturedArgs -join '|'))
    }
}
Write-Host 'PASS: Docker Compose plugin, standalone, and prefixed command arguments.'
