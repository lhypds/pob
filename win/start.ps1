# Builds the Go core + Windows shell and launches Pob detached.
# Run it again — or pass -Count — to start additional instances side by side.
#
# Usage: .\start.ps1 [-Count <n>]

param(
    [ValidateRange(1, 64)]
    [int]$Count = 1
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = (Resolve-Path (Join-Path $ScriptDir "..")).Path

Write-Host "🔨 Building core (Go)..."
Push-Location (Join-Path $RootDir "core")
try {
    go build -o bin\pob-core.exe .\cmd\pob-core
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}

Write-Host "🔨 Building Windows shell (C#/WPF)..."
dotnet build (Join-Path $ScriptDir "Pob.csproj") -c Debug
if ($LASTEXITCODE -ne 0) { exit 1 }

$Exe = Join-Path $ScriptDir "bin\Debug\net8.0-windows\Pob.exe"
for ($i = 0; $i -lt $Count; $i++) {
    $Proc = Start-Process -FilePath $Exe -WorkingDirectory $RootDir -PassThru
    Write-Host "▶️  Pob started (pid $($Proc.Id))."
}
Write-Host "Stop all instances with .\stop.ps1"
