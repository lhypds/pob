#!/bin/bash
# Deploys a Pob release into a Windows VM running under VMware Fusion on this
# Mac — the nearest thing to running the Linux shell in a container, since a
# Windows container has no desktop for the shell to capture or inject into
# (see docs/15_VM.md). Builds the zip with build_docker.sh, starts the VM
# headless if it is down, copies the zip in over SSH, installs it, and restarts
# the app through the Task Scheduler.
#
# That last part is the one that matters: `pob launch` starts the app in the
# session it is called from, and an SSH session has no desktop — an app
# started that way captures black frames and injects input nowhere. A task
# registered against the logged-on user runs in that user's console session
# instead, which is where the desktop is.
#
# Assumes the one-time guest setup in docs/15_VM.md: autologon, no lock/sleep,
# OpenSSH server, and port 8033 through the firewall.
#
# Env:
#   POB_VM          .vmwarevm bundle    (default: ~/VMs/PobWin.vmwarevm)
#   POB_VM_SSH      ssh destination     (default: pobwin)
#   WIN_ARCH        amd64|arm64         (default: arm64 — an M-series Mac
#                                        runs ARM guests only)
#   POB_SKIP_BUILD  1 to deploy the zip already in the project root
#   VMRUN           path to vmrun, if Fusion is somewhere unusual

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

VM="${POB_VM:-$HOME/VMs/PobWin.vmwarevm}"
SSH_HOST="${POB_VM_SSH:-pobwin}"
ARCH="${WIN_ARCH:-arm64}"
VMRUN="${VMRUN:-/Applications/VMware Fusion.app/Contents/Public/vmrun}"
VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo '0.0.1')"
ZIP_PATH="$ROOT_DIR/Pob-${VERSION}-windows-${ARCH}.zip"

case "$ARCH" in
    amd64|arm64) ;;
    *) echo "❌ Unknown WIN_ARCH '$ARCH' (use amd64 or arm64)"; exit 1 ;;
esac

if [ ! -x "$VMRUN" ]; then
    if command -v vmrun &> /dev/null; then
        VMRUN="$(command -v vmrun)"
    else
        echo "❌ vmrun not found — install VMware Fusion, or set VMRUN."
        exit 1
    fi
fi
if [ ! -e "$VM" ]; then
    echo "❌ No VM at $VM — create it first (docs/15_VM.md), or set POB_VM."
    exit 1
fi

# `vmrun list` answers in .vmx paths, so the running check needs the .vmx
# inside the bundle rather than the bundle itself.
VMX="$VM"
if [ -d "$VM" ]; then
    VMX="$(find "$VM" -maxdepth 1 -name '*.vmx' | head -1)"
fi
if [ -z "$VMX" ]; then
    echo "❌ No .vmx inside $VM — is that a Fusion VM?"
    exit 1
fi

# ── build (unless we were told to reuse the zip in the root) ─────────────────
if [ "${POB_SKIP_BUILD:-0}" = "1" ]; then
    if [ ! -f "$ZIP_PATH" ]; then
        echo "❌ POB_SKIP_BUILD=1 but $ZIP_PATH does not exist."
        exit 1
    fi
    echo "Using existing $(basename "$ZIP_PATH")…"
else
    WIN_ARCHS="$ARCH" "$SCRIPT_DIR/build_docker.sh"
fi

# ── make sure the guest is up ───────────────────────────────────────────────
if "$VMRUN" list | grep -qF "$VMX"; then
    echo "VM is already running."
else
    echo "Starting VM headless…"
    "$VMRUN" start "$VM" nogui
fi

echo "Waiting for SSH on $SSH_HOST…"
SSH_READY=0
for _ in $(seq 1 60); do
    if ssh -o ConnectTimeout=5 -o BatchMode=yes "$SSH_HOST" exit 2>/dev/null; then
        SSH_READY=1
        break
    fi
    sleep 3
done
if [ "$SSH_READY" != "1" ]; then
    echo "❌ No SSH on $SSH_HOST after 3 minutes."
    echo "   Check the sshd service and administrators_authorized_keys (docs/15_VM.md)."
    exit 1
fi

# ── copy in ─────────────────────────────────────────────────────────────────
echo "Copying $(basename "$ZIP_PATH")…"
scp -q "$ZIP_PATH" "$SSH_HOST:pob-release.zip"

# ── stop, install, restart in the console session ───────────────────────────
# The heredoc goes to PowerShell on stdin, which keeps the quoting out of
# bash's reach — `ssh host powershell -Command -` reads the script from there.
echo "Installing in the guest…"
ssh "$SSH_HOST" powershell -NoProfile -ExecutionPolicy Bypass -Command - <<'REMOTE'
$ErrorActionPreference = 'Stop'
trap { Write-Host "REMOTE FAILED: $_"; exit 1 }

$zip   = Join-Path $env:USERPROFILE 'pob-release.zip'
$stage = Join-Path $env:USERPROFILE 'pob-release'
$app   = Join-Path $env:LOCALAPPDATA 'Programs\Pob'
$cli   = Join-Path $app 'Helpers\pob.exe'

# Stop what is running first: install.ps1 refuses to overwrite a live install.
# `pob kill` is the way to do it — it ends the shell and lets core exit on the
# closing pipe, which is when core writes the instance's end time. Killing
# pob-core outright would lose that, so Stop-Process is only the fallback for a
# guest with no CLI yet, or one that stopped answering.
if (Test-Path $cli) { & $cli kill 2>$null | Out-Null }
schtasks /end /tn Pob 2>$null | Out-Null
Stop-Process -Name Pob,pob-core -Force -ErrorAction SilentlyContinue
foreach ($i in 1..20) {
    if (-not (Get-Process -Name Pob,pob-core -ErrorAction SilentlyContinue)) { break }
    Start-Sleep -Milliseconds 500
}

Remove-Item -Recurse -Force $stage -ErrorAction SilentlyContinue
Expand-Archive -LiteralPath $zip -DestinationPath $stage -Force

# install.ps1 reports failure by exit code and its success path never calls
# exit, so $LASTEXITCODE has to be cleared or the schtasks above answers for it.
$global:LASTEXITCODE = 0
& (Join-Path $stage 'Pob\install.ps1')
if ($LASTEXITCODE -ne 0) { throw "install.ps1 failed (exit $LASTEXITCODE)" }

# The scheduled task is what puts the app on the console desktop. Registering
# it here rather than by hand keeps a fresh guest one command away — an
# Interactive principal runs in the logged-on user's session, unlike anything
# started from this SSH session. RunLevel Highest because UIPI blocks
# synthesized input from a normal process into an elevated window.
if (-not (Get-ScheduledTask -TaskName Pob -ErrorAction SilentlyContinue)) {
    Write-Host "Registering the Pob scheduled task…"
    Register-ScheduledTask -TaskName Pob -Force `
        -Action    (New-ScheduledTaskAction -Execute (Join-Path $app 'Pob.exe')) `
        -Trigger   (New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME) `
        -Principal (New-ScheduledTaskPrincipal `
                       -UserId "$env:COMPUTERNAME\$env:USERNAME" `
                       -LogonType Interactive -RunLevel Highest) `
        -Settings  (New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries `
                       -DontStopIfGoingOnBatteries `
                       -ExecutionTimeLimit ([TimeSpan]::Zero)) | Out-Null
}

$global:LASTEXITCODE = 0
schtasks /run /tn Pob | Out-Null
$started = ($LASTEXITCODE -eq 0)
Remove-Item -Recurse -Force $zip, $stage -ErrorAction SilentlyContinue
if (-not $started) {
    Write-Host "⚠️  schtasks could not start Pob — is anyone logged on at the console?"
    exit 0
}

# Give the shell time to spawn pob-core and the core time to bind the server,
# then let the CLI report the address — it talks to the loopback Control API,
# so it only works from inside the guest.
Start-Sleep -Seconds 5
if (Test-Path $cli) { & $cli status } else { Write-Host "CLI not at $cli" }
exit 0
REMOTE

echo ""
echo "Done: Pob $VERSION ($ARCH) deployed to $SSH_HOST."
echo "If no address printed above, nobody is logged on at the console —"
echo "a task with an Interactive principal has no session to start in."
