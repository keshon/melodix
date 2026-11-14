@echo off
setlocal
cd /d "%~dp0" || exit /b 1
where node >nul 2>nul
if errorlevel 1 (
    echo ERROR: Install Node.js 22.12+ or 24 LTS first.
    exit /b 1
)
if not exist "node_modulesiteinite.js" (
    call npm ci
    if errorlevel 1 exit /b 1
)
call npm run dev -- --open %*
exit /b %errorlevel%
