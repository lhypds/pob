# Stops the Pob shell and the core process.
# The Go core exits on its own when the shell dies (stdin EOF), but clean up
# any stragglers.

$Found = $false
foreach ($Name in @("Pob", "pob-core")) {
    $Procs = Get-Process -Name $Name -ErrorAction SilentlyContinue
    if ($Procs) {
        $Found = $true
        Write-Host "Stopping $Name process: $($Procs.Id -join ', ')"
        $Procs | Stop-Process -Force -ErrorAction SilentlyContinue
    }
}

if (-not $Found) {
    Write-Host "No running Pob process found."
} else {
    Write-Host "Pob stopped."
}
