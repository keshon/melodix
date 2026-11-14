@echo off
setlocal
cd /d "%~dp0" || exit /b 1
call "%~dp0scripts\ensure-build.cmd" discord
if errorlevel 1 goto :failed
"%~dp0melodix-discord.exe" %*
if errorlevel 1 goto :failed
exit /b 0

:failed
set "APP_EXIT=%errorlevel%"
echo.
echo The bot exited with an error. Read the diagnostic above.
pause
exit /b %APP_EXIT%
