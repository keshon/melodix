@echo off
setlocal
cd /d "%~dp0"

REM Runs the convention checks. They are absolute: any violation fails.
REM See docs/conventions.md for what each rule is and why.

go test ./internal/conventions/ -count=1 -v
exit /b %ERRORLEVEL%
