@echo off
setlocal
cd /d "%~dp0"

set "ACTION=%~1"
if "%ACTION%"=="" set "ACTION=check"

if /i "%ACTION%"=="check" goto :check
if /i "%ACTION%"=="accept" goto :accept

echo Usage: %~nx0 [check^|accept]
echo   check  - run the convention checks, print the scorecard (default)
echo   accept - rewrite baseline.json from the current tree, then show it
echo.
echo The checks ratchet: a rule fails only when a file gets worse, and a new
echo file starts at zero. Use 'accept' after fixing violations, to lock the
echo gain in - or after knowingly adding one you have read and accepted.
echo A number going up is the finding; the baseline is only bookkeeping.
echo See docs/conventions.md.
exit /b 1

:check
go test ./internal/conventions/ -count=1 -v
exit /b %ERRORLEVEL%

:accept
echo Rewriting internal\conventions\baseline.json from the current tree...
echo.
set "CONVENTIONS_UPDATE=1"
go test ./internal/conventions/ -count=1
set "CONVENTIONS_UPDATE="
echo.
echo The FAIL above is expected - an update run never passes, so a stray
echo environment variable cannot disable the checks in silence. Re-checking:
echo.
go test ./internal/conventions/ -count=1 -v
exit /b %ERRORLEVEL%
