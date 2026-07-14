@echo off
setlocal

cd /d "%~dp0"

echo ============================================================
echo  NE-PULSE Unified Development Stack
echo ============================================================

rem Self-heal a stale PATH: on some machines the Go installer updates the
rem system PATH but any shell already open (or spawned from one) keeps its
rem old environment until a fresh login/reboot. If "go" isn't reachable,
rem fall back to its standard install location rather than failing here
rem and leaving the user staring at "'go' is not recognized".
where go >nul 2>&1
if errorlevel 1 (
    echo   "go" not found on PATH — falling back to C:\Program Files\Go\bin
    set "PATH=%PATH%;C:\Program Files\Go\bin"
)
where go >nul 2>&1
if errorlevel 1 (
    echo   ERROR: go.exe still not found. Install Go or add it to PATH, then re-run.
    exit /b 1
)

rem Belt-and-suspenders cleanup: if a previous run was stopped by closing
rem its windows (X button) instead of Ctrl+C, its server/web/load
rem processes never got a chance to shut down and would otherwise keep
rem :8080/:3000/:50051 locked for this run. Killing by window title is
rem silent and harmless if nothing matches.
echo Clearing any leftover NE-PULSE processes from a previous run...
taskkill /FI "WINDOWTITLE eq NE-PULSE-Server*" /T /F >nul 2>&1
taskkill /FI "WINDOWTITLE eq NE-PULSE-Web*" /T /F >nul 2>&1
taskkill /FI "WINDOWTITLE eq NE-PULSE-Load*" /T /F >nul 2>&1

echo Starting Go server (memory mode, no Redis required)...
start "NE-PULSE-Server" cmd /k "go run ./cmd/server -sim-mode=memory"

echo Starting Next.js dashboard...
cd /d "%~dp0web"
start "NE-PULSE-Web" cmd /k "npm run dev"
cd /d "%~dp0"

echo Waiting 6 seconds for Next.js to compile and the WebSocket to come up...
timeout /t 6 /nobreak >nul

rem A one-shot burst finishes in well under a second and then disconnects
rem entirely — great for a quick throughput check, useless for "the map
rem should have live dots on it," since a fresh page load has nothing left
rem to see by the time it connects. This is a continuous background
rem stream instead: 300 devices is enough to keep the map populated
rem without hammering a dev machine the way the full 5000-device default
rem would. It runs until this script's cleanup step kills it.
echo Starting continuous background traffic generator (300 simulated devices)...
start "NE-PULSE-Load" cmd /k "go run ./cmd/loadclient -mode=chaos -devices=300 -conns=10"

echo.
echo ============================================================
echo  NE-PULSE stack is running.
echo    Dashboard : http://localhost:3000
echo    Health    : http://localhost:8080/api/health
echo    Server log window : "NE-PULSE-Server"
echo    Web log window    : "NE-PULSE-Web"
echo    Load log window   : "NE-PULSE-Load"
echo ============================================================
echo  Press CTRL+C (then Y), or any key, to stop and clean up.
echo ============================================================
echo.

call :waitloop

:cleanup
echo.
echo Shutting down NE-PULSE-Server, NE-PULSE-Web, and NE-PULSE-Load...
taskkill /FI "WINDOWTITLE eq NE-PULSE-Server*" /T /F >nul 2>&1
taskkill /FI "WINDOWTITLE eq NE-PULSE-Web*" /T /F >nul 2>&1
taskkill /FI "WINDOWTITLE eq NE-PULSE-Load*" /T /F >nul 2>&1
echo Done — ports released.
endlocal
exit /b 0

:waitloop
pause >nul
goto :eof
