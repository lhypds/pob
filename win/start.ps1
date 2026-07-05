# Builds the Go core + Windows shell and runs the app in the foreground.

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

Write-Host "▶️  Launching Pob..."
$Exe = Join-Path $ScriptDir "bin\Debug\net8.0-windows\Pob.exe"
Start-Process -FilePath $Exe -WorkingDirectory $RootDir -Wait
