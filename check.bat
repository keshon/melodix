@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

rem The full gate, in the order docs/conventions.md states it: formatting, vet,
rem the curated linter set, then race tests - which include the convention
rem checks, since those are an ordinary Go test. Run this before a commit.

echo [1/4] gofmt...
set "UNFORMATTED="
for /f "delims=" %%f in ('gofmt -l cmd internal pkg\music 2^>nul') do (
    set "UNFORMATTED=1"
    echo    %%f
)
if defined UNFORMATTED (
    echo.
    echo   not formatted. fix with: gofmt -w cmd internal pkg\music
    exit /b 1
)
echo    clean

echo [2/4] go vet...
go vet ./...
if errorlevel 1 exit /b 1
echo    clean

echo [3/4] golangci-lint...
where golangci-lint >nul 2>&1
if errorlevel 1 (
    echo    not installed, skipped - CI still runs it
) else (
    golangci-lint run ./...
    if errorlevel 1 exit /b 1
)

echo [4/4] go test -race ./... ^(includes the convention checks^)...
go test -race ./... -count=1 -timeout=300s
if errorlevel 1 exit /b 1

echo.
echo All gates passed. Scorecard: conventions.bat
exit /b 0
