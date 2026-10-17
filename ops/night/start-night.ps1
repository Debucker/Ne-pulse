# Starts the night supervisor in its own hidden window, independent of any
# terminal or app, so closing them does not stop the network.
$run = Join-Path $PSScriptRoot 'run-night.ps1'
$pidFile = Join-Path $PSScriptRoot 'supervisor.pid'
if (Test-Path $pidFile) {
  $old = Get-Content $pidFile
  # After a reboot the old pid may belong to another program.
  $p = Get-Process -Id $old -ErrorAction SilentlyContinue
  if ($p -and $p.ProcessName -eq 'powershell') { Write-Output "Already running (pid $old)."; exit 0 }
}
Remove-Item (Join-Path $PSScriptRoot 'STOP') -ErrorAction SilentlyContinue
Start-Process powershell -ArgumentList "-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"$run`"" -WindowStyle Hidden
Write-Output 'Night supervisor started. Status: ops\night\status.txt  Logs: ops\night\logs\'
