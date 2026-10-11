# Asks the night supervisor to stop (it stops the tunnels and the backend).
New-Item -ItemType File -Force (Join-Path $PSScriptRoot 'STOP') | Out-Null
Write-Output 'Stop requested; the supervisor exits within a minute.'
