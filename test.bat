@echo off
setlocal
cd /d "%~dp0"
go test ./... -count=1 -timeout=120s
exit /b %ERRORLEVEL%
