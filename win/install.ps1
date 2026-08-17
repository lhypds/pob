# Installs Pob on this machine: the app tree goes somewhere it can stay, the
# `pob` command lands on the PATH, and Pob turns up in the Start menu — the
# Windows counterpart of the macOS app's "Install 'pob' Command…" menu item.
#
# Runs from either end of the release: inside an unzipped Pob\ folder (what a
# user downloads), or from the repository, where it builds dist\Pob first.
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File install.ps1
#   powershell -ExecutionPolicy Bypass -File install.ps1 -Uninstall
#
# Everything is per-user (no administrator prompt): the app goes under
# %LOCALAPPDATA%\Programs\Pob and the PATH entry is the user's, not the
# machine's. -InstallDir puts it somewhere else.
#
# Keep this file saved as UTF-8 *with* a BOM. Windows PowerShell 5.1 reads a
# BOM-less script in the machine's ANSI codepage, where the third byte of an
# em dash or an emoji decodes to a curly quote — which the parser accepts as a
# string delimiter, so the script dies on a syntax error before it runs.

param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "Programs\Pob"),
    [switch]$Uninstall
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# The CLI sits in Helpers\ beside the app, as it does on macOS and Linux: on a
# case-insensitive filesystem its pob.exe and the app's Pob.exe are the same
# filename, so they cannot share a directory. Helpers\ is what goes on the PATH.
$HelpersDir = Join-Path $InstallDir "Helpers"
$StartMenuDir = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs"
$Shortcut = Join-Path $StartMenuDir "Pob.lnk"

# ── PATH (the user's, in the registry — not just this session) ───────────────

function Get-UserPath {
    [Environment]::GetEnvironmentVariable("Path", "User")
}

# @() around the pipeline on purpose: a one-entry PATH would come back as a
# bare string, and + on a string concatenates instead of appending.
function Add-ToUserPath($Dir) {
    $current = Get-UserPath
    $entries = @()
    if ($current) { $entries = @($current -split ';' | Where-Object { $_ -ne '' }) }
    if ($entries -contains $Dir) { return $false }
    [Environment]::SetEnvironmentVariable("Path", (($entries + $Dir) -join ';'), "User")
    return $true
}

function Remove-FromUserPath($Dir) {
    $current = Get-UserPath
    if (-not $current) { return $false }
    $entries = @($current -split ';' | Where-Object { $_ -ne '' })
    if ($entries -notcontains $Dir) { return $false }
    $kept = @($entries | Where-Object { $_ -ne $Dir })
    [Environment]::SetEnvironmentVariable("Path", ($kept -join ';'), "User")
    return $true
}

# ── uninstall ────────────────────────────────────────────────────────────────

if ($Uninstall) {
    if (Remove-FromUserPath $HelpersDir) {
        Write-Host "✅ Removed $HelpersDir from your PATH"
    }
    if (Test-Path $Shortcut) {
        Remove-Item $Shortcut -Force
        Write-Host "✅ Removed the Start menu shortcut"
    }
    if (Test-Path $InstallDir) {
        Remove-Item $InstallDir -Recurse -Force
        Write-Host "✅ Removed $InstallDir"
    }
    Write-Host ""
    Write-Host "Done. %USERPROFILE%\.pob is untouched — settings, instances and logs are still there."
    exit 0
}

# ── what to install from ─────────────────────────────────────────────────────

# Either this script sits inside an unzipped release beside the binaries, or it
# is the copy in the repository and the release has to be built first.
$Src = $null
if ((Test-Path (Join-Path $ScriptDir "Pob.exe")) -and (Test-Path (Join-Path $ScriptDir "pob-core.exe"))) {
    $Src = $ScriptDir
} elseif (Test-Path (Join-Path $ScriptDir "dist\Pob\Pob.exe")) {
    $Src = Join-Path $ScriptDir "dist\Pob"
} elseif (Test-Path (Join-Path $ScriptDir "build.ps1")) {
    Write-Host "🔨 No dist\Pob yet — building it…"
    & (Join-Path $ScriptDir "build.ps1")
    if ($LASTEXITCODE -ne 0) { exit 1 }
    $Src = Join-Path $ScriptDir "dist\Pob"
} else {
    Write-Host "❌ Nothing to install from — run this inside an unzipped Pob folder or the repository."
    exit 1
}

foreach ($f in @("Pob.exe", "pob-core.exe", "Helpers\pob.exe")) {
    if (-not (Test-Path (Join-Path $Src $f))) {
        Write-Host "❌ $(Join-Path $Src $f) is missing — the build is incomplete."
        exit 1
    }
}

# ── install ──────────────────────────────────────────────────────────────────

# A running Pob holds its own .exe open, and Windows will not let it be
# replaced — better to say so than to fail halfway through the copy.
#
# Match on the executable's path, not its name. The app is Pob.exe and the CLI
# beside it is pob.exe, and Windows process names are case-insensitive: a bare
# `-Name "Pob"` also matched the `pob update` that runs this installer, so
# every update refused itself with "Pob is running" — while `pob kill`, which
# asks the instance rather than the process table, correctly said it was not.
# Only a process running the copy about to be overwritten is in the way; the
# same app running from some other install is not.
$AppPath = Join-Path $InstallDir "Pob.exe"
$RunningApp = @(
    Get-Process -Name "Pob" -ErrorAction SilentlyContinue | Where-Object {
        # .Path throws for a process this user cannot open — not ours, then.
        try { $_.Path -eq $AppPath } catch { $false }
    }
)
if ($RunningApp.Count -gt 0) {
    Write-Host "⚠️  Pob is running — quit it first (or ``pob kill``) so the files can be replaced."
    exit 1
}

Write-Host "📦 Installing to $InstallDir…"
New-Item -ItemType Directory -Force -Path $HelpersDir | Out-Null

Copy-Item (Join-Path $Src "Pob.exe") (Join-Path $InstallDir "Pob.exe") -Force
Copy-Item (Join-Path $Src "pob-core.exe") (Join-Path $InstallDir "pob-core.exe") -Force
Copy-Item (Join-Path $Src "Helpers\pob.exe") (Join-Path $HelpersDir "pob.exe") -Force
if (Test-Path (Join-Path $Src "VERSION")) {
    Copy-Item (Join-Path $Src "VERSION") (Join-Path $InstallDir "VERSION") -Force
}

# The installer, the guide and the license go along too, so uninstalling later
# — or reading what any of this was, or what it may be used for — does not mean
# finding the zip again.
if ($Src -ne $InstallDir) {
    Copy-Item $PSCommandPath (Join-Path $InstallDir "install.ps1") -Force
    if (Test-Path (Join-Path $Src "README.txt")) {
        Copy-Item (Join-Path $Src "README.txt") (Join-Path $InstallDir "README.txt") -Force
    }
    if (Test-Path (Join-Path $Src "LICENSE")) {
        Copy-Item (Join-Path $Src "LICENSE") (Join-Path $InstallDir "LICENSE") -Force
    }
}

# Files that came out of a downloaded zip carry a mark-of-the-web that makes
# Windows refuse to run them; the copies keep it, so take it off here.
Get-ChildItem $InstallDir -Recurse -File | Unblock-File -ErrorAction SilentlyContinue

Write-Host "✅ Installed:"
Write-Host "   app  $(Join-Path $InstallDir 'Pob.exe')"
Write-Host "   core $(Join-Path $InstallDir 'pob-core.exe')"
Write-Host "   cli  $(Join-Path $HelpersDir 'pob.exe')"

# ── Start menu ───────────────────────────────────────────────────────────────

$shell = New-Object -ComObject WScript.Shell
$link = $shell.CreateShortcut($Shortcut)
$link.TargetPath = (Join-Path $InstallDir "Pob.exe")
$link.WorkingDirectory = $InstallDir
$link.Description = "Pob — Perception and Operation Bridge"
$link.Save()
Write-Host "✅ Added Pob to the Start menu"

# ── the pob command ──────────────────────────────────────────────────────────

# The command on the PATH is the CLI, not the app: `pob` typed in a terminal
# inspects and drives the instance, and `pob launch` is what starts the window.
$added = Add-ToUserPath $HelpersDir
# A PATH set in the registry reaches processes started from now on, so put it
# in this session too — otherwise the very terminal that ran the installer is
# the one place `pob` does not work.
if ($env:Path -notlike "*$HelpersDir*") { $env:Path = "$env:Path;$HelpersDir" }

Write-Host ""
if ($added) {
    Write-Host "✅ Added $HelpersDir to your PATH"
    Write-Host "Done — open a new terminal and try ``pob`` (or ``pob launch`` to start the app)."
} else {
    Write-Host "Done — try ``pob`` (or ``pob launch`` to start the app)."
}
