@echo off
setlocal

set "SCRIPT_DIR=%~dp0"
set "SCRIPT_DIR=%SCRIPT_DIR:~0,-1%"

set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=discord"
if /i "%TARGET%"=="discord" goto :discord
if /i "%TARGET%"=="cli" goto :cli
echo Usage: %~nx0 [discord^|cli]
echo   discord - build and run Discord bot (default)
echo   cli     - build and run CLI player
exit /b 1

:discord
set "MAIN_PKG=%SCRIPT_DIR%\cmd\discord"
set "OUTPUT=%SCRIPT_DIR%\melodix-discord.exe"
set "DESC=Discord music bot that allows you to play music from YouTube, SoundCloud and internet radio streams."
goto :build

:cli
set "MAIN_PKG=%SCRIPT_DIR%\cmd\cli"
set "OUTPUT=%SCRIPT_DIR%\melodix-cli.exe"
set "DESC=CLI music player - same playback engine as the Discord bot."
goto :build

:build
if not exist "%MAIN_PKG%\main.go" (
    echo ERROR: main.go not found in %MAIN_PKG%
    exit /b 1
)

echo [1/3] Gathering build info [%TARGET%]...

for /f "tokens=*" %%a in ('powershell -NoProfile -Command "Get-Date -Format yyyy-MM-ddTHH-mm-ssZ"') do set "BUILD_DATE=%%a"

for /f "tokens=*" %%c in ('git -C "%SCRIPT_DIR%" rev-parse --short HEAD 2^>nul') do set "GIT_COMMIT=%%c"
if "%GIT_COMMIT%"=="" set "GIT_COMMIT=none"

set "LD=-X github.com/keshon/buildinfo.Version=dev"
set "LD=%LD% -X github.com/keshon/buildinfo.Commit=%GIT_COMMIT%"
set "LD=%LD% -X github.com/keshon/buildinfo.BuildTime=%BUILD_DATE%"
set "LD=%LD% -X github.com/keshon/buildinfo.Project=Melodix"
set "LD=%LD% -X 'github.com/keshon/buildinfo.Description=%DESC%'"

echo [2/3] Building %OUTPUT%...

go build -o "%OUTPUT%" -ldflags "%LD%" "%MAIN_PKG%"
if errorlevel 1 (
    echo Build failed!
    exit /b 1
)

echo [3/3] Running %OUTPUT%...

"%OUTPUT%"
exit /b %errorlevel%