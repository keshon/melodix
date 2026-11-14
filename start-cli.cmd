@echo off
setlocal
cd /d "%~dp0" || exit /b 1
call "%~dp0scripts\ensure-build.cmd" cli
if errorlevel 1 exit /b %errorlevel%
"%~dp0melodix-cli.exe" %*
exit /b %errorlevel%
