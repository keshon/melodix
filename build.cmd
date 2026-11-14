@echo off
setlocal
cd /d "%~dp0" || exit /b 1
where go >nul 2>nul
if errorlevel 1 (
    echo ERROR: Go was not found. Install the Go version required by go.mod.
    exit /b 1
)
go run ./cmd/build %*
exit /b %errorlevel%
