# Builds a distributable Windows release natively (run on a Windows machine):
# Go core (pob-core.exe) + WPF shell, assembled side by side (the shell looks
# for pob-core.exe next to its own binary, like the macOS bundle layout).
# Produces: .\win\dist\Pob\  and  Pob-<version>-windows-<arch>.zip

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir = (Resolve-Path (Join-Path $ScriptDir "..")).Path

$Version = "0.0.1"
$VersionFile = Join-Path $RootDir "VERSION"
if (Test-Path $VersionFile) { $Version = (Get-Content $VersionFile -Raw).Trim() }

switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { $Arch = "amd64"; $Rid = "win-x64" }
    "ARM64" { $Arch = "arm64"; $Rid = "win-arm64" }
    default { $Arch = "amd64"; $Rid = "win-x64" }
}

$DistDir = Join-Path $ScriptDir "dist\Pob"
$ZipPath = Join-Path $RootDir "Pob-$Version-windows-$Arch.zip"

# ── build core (Go) ──────────────────────────────────────────────────────────
Write-Host "Building pob-core (Go)…"
Push-Location (Join-Path $RootDir "core")
try {
    go build -trimpath -ldflags="-s -w" -o bin\pob-core.exe .\cmd\pob-core
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}

# ── build shell (C#/WPF, self-contained single file) ─────────────────────────
Write-Host "Building Windows shell (release)…"
$PublishDir = Join-Path $ScriptDir "publish"
dotnet publish (Join-Path $ScriptDir "Pob.csproj") -c Release -r $Rid `
    --self-contained true -p:PublishSingleFile=true `
    -p:IncludeNativeLibrariesForSelfExtract=true -o $PublishDir
if ($LASTEXITCODE -ne 0) { exit 1 }

# ── assemble ─────────────────────────────────────────────────────────────────
Write-Host "Assembling dist\Pob…"
Remove-Item -Recurse -Force (Join-Path $ScriptDir "dist") -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
Copy-Item (Join-Path $PublishDir "Pob.exe") $DistDir
Copy-Item (Join-Path $RootDir "core\bin\pob-core.exe") $DistDir
if (Test-Path $VersionFile) { Copy-Item $VersionFile (Join-Path $DistDir "VERSION") }

Write-Host "Creating $ZipPath…"
Remove-Item $ZipPath -ErrorAction SilentlyContinue
Compress-Archive -Path (Join-Path $ScriptDir "dist\Pob") -DestinationPath $ZipPath

Write-Host ""
Write-Host "Done: $DistDir"
Write-Host "  Version : $Version"
Write-Host "  Zip     : $ZipPath"
Write-Host ""
Write-Host "Run with:  $DistDir\Pob.exe"
