# Setup script for the Pob Windows shell.
# Checks toolchain dependencies, then builds the Go core and the shell.
# Run from anywhere:  powershell -ExecutionPolicy Bypass -File win\setup.ps1

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = (Resolve-Path (Join-Path $ScriptDir "..")).Path

Write-Host "🚀 Setting up Pob (Windows) development environment..."

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "❌ Go is not installed. Install it from https://go.dev/dl/ or:  winget install GoLang.Go"
    exit 1
}
Write-Host "✅ Go found: $(go version)"

if (-not (Get-Command dotnet -ErrorAction SilentlyContinue)) {
    Write-Host "❌ .NET SDK not found. Install it with:  winget install Microsoft.DotNet.SDK.8"
    exit 1
}
Write-Host "✅ .NET SDK found: $(dotnet --version)"

# Initialize settings.json from example when available
if (-not (Test-Path (Join-Path $RootDir "settings.json")) -and
    (Test-Path (Join-Path $RootDir "settings.json.example"))) {
    Copy-Item (Join-Path $RootDir "settings.json.example") (Join-Path $RootDir "settings.json")
    Write-Host "✅ Created settings.json from settings.json.example"
}

Write-Host "🔨 Building core (Go)..."
Push-Location (Join-Path $RootDir "core")
try {
    go mod download
    go build -o bin\pob-core.exe .\cmd\pob-core
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}
Write-Host "✅ core build successful"

Write-Host "🔨 Building Windows shell (C#/WPF)..."
dotnet build (Join-Path $ScriptDir "Pob.csproj") -c Debug
if ($LASTEXITCODE -ne 0) { exit 1 }
Write-Host "✅ Windows shell build successful"

Write-Host ""
Write-Host "Done. Start the app with win\start.ps1 (foreground) or win\restart.ps1 (background)."
