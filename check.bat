@echo off
call "%~dp0build.cmd" check %*
exit /b %errorlevel%
