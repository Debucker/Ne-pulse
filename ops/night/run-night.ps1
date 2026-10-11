# NE-PULSE night supervisor (Windows PowerShell 5.1).
#
# Keeps the research network running on this laptop overnight:
#   - the backend (bin\nepulse-server.exe, args from bin\night-args.json), restarted if it exits
#   - $Tunnels Cloudflare quick tunnels (each quick tunnel carries at most ~200
#     open connections, every listening phone holds one), health-checked every
#     minute and replaced when dead
#   - whenever the tunnel addresses change: writes web\public\backend.json and
#     redeploys the site to Vercel (phones re-read backend.json and reconnect)
#   - keeps the computer from sleeping while it runs
#
# Start:  powershell -ExecutionPolicy Bypass -File ops\night\start-night.ps1
# Stop:   powershell -ExecutionPolicy Bypass -File ops\night\stop-night.ps1
# Logs:   ops\night\logs\

$ErrorActionPreference = 'Continue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$Root    = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$Ops     = $PSScriptRoot
$Logs    = Join-Path $Ops 'logs'
$Server  = Join-Path $Root 'bin\nepulse-server.exe'
$ArgFile = Join-Path $Root 'bin\night-args.json'
$Web     = Join-Path $Root 'web'
$Stage   = Join-Path $Ops 'stage\ne-pulse-preview'
$Cf      = 'C:\Program Files (x86)\cloudflared\cloudflared.exe'
$Tunnels = 3
$Scope   = 'd-craft'
New-Item -ItemType Directory -Force $Logs | Out-Null
Set-Content (Join-Path $Ops 'supervisor.pid') $PID

Add-Type -Namespace NePulse -Name Power -MemberDefinition '[System.Runtime.InteropServices.DllImport("kernel32.dll")] public static extern uint SetThreadExecutionState(uint flags);'
function Keep-Awake { [NePulse.Power]::SetThreadExecutionState([uint32]'0x80000001') | Out-Null } # continuous + system required

function Log([string]$m) {
  $line = "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') $m"
  Add-Content -Path (Join-Path $Logs 'supervisor.log') -Value $line
}

function Quote([string]$a) { if ($a -match '[\s"]') { '"' + ($a -replace '"', '\"') + '"' } else { $a } }

function Start-Backend {
  $argList = (Get-Content $ArgFile -Raw | ConvertFrom-Json) | ForEach-Object { Quote $_ }
  $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
  $p = Start-Process -FilePath $Server -ArgumentList ($argList -join ' ') -WorkingDirectory $Root -WindowStyle Hidden -PassThru `
        -RedirectStandardOutput (Join-Path $Logs "server-$stamp.out.log") -RedirectStandardError (Join-Path $Logs "server-$stamp.log")
  Log "backend started (pid $($p.Id))"
  return $p
}

function Test-Url([string]$url) {
  try { (Invoke-WebRequest -UseBasicParsing -TimeoutSec 15 -Uri $url).StatusCode -eq 200 } catch { $false }
}

function Start-Tunnel([int]$i) {
  $log = Join-Path $Logs "tunnel$i.log"
  if (Test-Path $log) { Remove-Item $log -Force -ErrorAction SilentlyContinue }
  $p = Start-Process -FilePath $Cf -ArgumentList 'tunnel --no-autoupdate --protocol http2 --url http://localhost:8080' -WindowStyle Hidden -PassThru `
        -RedirectStandardError $log -RedirectStandardOutput (Join-Path $Logs "tunnel$i.out.log")
  $url = $null
  for ($k = 0; $k -lt 60 -and -not $url; $k++) {
    Start-Sleep -Seconds 1
    if (Test-Path $log) {
      $m = Select-String -Path $log -Pattern 'https://[a-z0-9-]+\.trycloudflare\.com' -ErrorAction SilentlyContinue | Select-Object -First 1
      if ($m) { $url = $m.Matches[0].Value }
    }
  }
  Log "tunnel $i started (pid $($p.Id)) $url"
  return @{ Proc = $p; Url = $url; Fails = 0 }
}

function Publish([string[]]$urls) {
  $urls = @($urls | Where-Object { $_ })
  if (-not $urls.Count) { Log 'publish skipped: no tunnel urls'; return $false }
  $json = '{"api":[' + (($urls | ForEach-Object { '"' + $_ + '"' }) -join ',') + '],"updated":"' + (Get-Date).ToUniversalTime().ToString('o') + '"}'
  Set-Content -Path (Join-Path $Web 'public\backend.json') -Value $json -Encoding Ascii
  # Stage a clean copy (no node_modules / build output / env files) and deploy it.
  New-Item -ItemType Directory -Force $Stage | Out-Null
  & robocopy $Web $Stage /MIR /XD node_modules .next .vercel /XF tsconfig.tsbuildinfo .env* /NFL /NDL /NJH /NJS /NP | Out-Null
  Log "deploying site with $($urls -join ', ')"
  $out = & cmd /c "cd /d `"$Stage`" && npx --yes vercel@latest deploy --prod -y --scope $Scope --build-env NEXT_PUBLIC_API_URL=$($urls[0]) 2>&1"
  $out | Out-File -FilePath (Join-Path $Logs 'deploy.log') -Append
  $ok = ($out | Select-String -Pattern 'Aliased' -Quiet)
  Log ("deploy " + $(if ($ok) { 'ok' } else { 'FAILED (see deploy.log)' }))
  return $ok
}

# ---------------------------------------------------------------- start
Log '=== supervisor starting ==='
Keep-Awake
$backend = Start-Backend
Start-Sleep -Seconds 3
$tun = @()
for ($i = 0; $i -lt $Tunnels; $i++) { $tun += ,(Start-Tunnel $i) }
Start-Sleep -Seconds 5
$published = ''
$pending = $true

# ---------------------------------------------------------------- loop
while ($true) {
  Keep-Awake
  if (Test-Path (Join-Path $Ops 'STOP')) { Log 'STOP file found, exiting'; break }

  if ($backend.HasExited) {
    Log "backend exited (code $($backend.ExitCode)); restarting"
    $backend = Start-Backend
    Start-Sleep -Seconds 3
  }
  $localOk = Test-Url 'http://localhost:8080/api/health'
  $netOk = Test-Url 'https://www.cloudflare.com/cdn-cgi/trace'

  if ($localOk -and $netOk) {
    for ($i = 0; $i -lt $tun.Count; $i++) {
      $t = $tun[$i]
      $alive = (-not $t.Proc.HasExited) -and $t.Url -and (Test-Url ($t.Url + '/api/health'))
      if ($alive) { $t.Fails = 0 } else { $t.Fails++ }
      if ($t.Fails -ge 3 -or $t.Proc.HasExited -or -not $t.Url) {
        Log "tunnel $i dead ($($t.Url)); replacing"
        try { if (-not $t.Proc.HasExited) { Stop-Process -Id $t.Proc.Id -Force } } catch {}
        $tun[$i] = Start-Tunnel $i
        $pending = $true
      }
    }
  } elseif (-not $netOk) {
    Log 'internet unreachable; waiting (tunnels left as they are)'
  }

  $urls = @($tun | ForEach-Object { $_.Url } | Where-Object { $_ })
  $key = ($urls | Sort-Object) -join ' '
  if ($pending -or $key -ne $published) {
    if ($netOk -and (Publish $urls)) { $published = $key; $pending = $false }
  }
  Set-Content (Join-Path $Ops 'status.txt') ("{0}`nbackend pid {1} ok={2}`ninternet ok={3}`ntunnels:`n{4}" -f (Get-Date), $backend.Id, $localOk, $netOk, (($tun | ForEach-Object { "  $($_.Url) fails=$($_.Fails)" }) -join "`n"))
  Start-Sleep -Seconds 60
}

# ---------------------------------------------------------------- stop
foreach ($t in $tun) { try { Stop-Process -Id $t.Proc.Id -Force } catch {} }
try { Stop-Process -Id $backend.Id -Force } catch {}
[NePulse.Power]::SetThreadExecutionState([uint32]'0x80000000') | Out-Null
Remove-Item (Join-Path $Ops 'STOP') -ErrorAction SilentlyContinue
Remove-Item (Join-Path $Ops 'supervisor.pid') -ErrorAction SilentlyContinue
Log '=== supervisor stopped ==='
