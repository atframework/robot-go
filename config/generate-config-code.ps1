#Requires -Version 7.0
param(
  [Parameter(Mandatory = $true)]
  [string]$PythonBin,

  [Parameter(Mandatory = $true)]
  [string]$GenScript,

  [Parameter(Mandatory = $true)]
  [string]$TemplateDir,

  [Parameter(Mandatory = $true)]
  [string]$ResConfigPb,

  [Parameter(Mandatory = $true)]
  [string]$ImportPath,

  [Parameter(Mandatory = $true)]
  [string]$OutputDir,

  [Parameter(Mandatory = $true)]
  [string]$StagingDir
)

$ErrorActionPreference = 'Stop'

# Guards: refuse empty values and filesystem roots before any recursive delete.
foreach ($path in @($OutputDir, $StagingDir)) {
  if ([string]::IsNullOrWhiteSpace($path)) {
    Write-Error 'OutputDir/StagingDir must not be empty'
  }
  $full = [IO.Path]::GetFullPath($path)
  if ($full -eq [IO.Path]::GetPathRoot($full)) {
    Write-Error "Refusing unsafe directory: $path"
  }
}

$staging = [IO.Path]::GetFullPath($StagingDir)
if (Test-Path $staging) {
  Remove-Item -Recurse -Force $staging
}
New-Item -ItemType Directory -Force $staging | Out-Null

$listRule = 'config_set.go.mako:${"config_set_{0}.go".format(loader.get_go_pb_name())}'
& $PythonBin $GenScript -i $TemplateDir -p $ResConfigPb -o $staging '--set' "config_protocol_import_path=$ImportPath" -g 'config_group.go.mako:config_group.go' -g 'config_manager.go.mako:config_manager.go' -l $listRule -t server
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

& (Join-Path $PSScriptRoot 'publish-directory.ps1') -SourceDir $staging -TargetDir $OutputDir
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
