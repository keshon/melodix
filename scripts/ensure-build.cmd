@echo off
setlocal
pushd "%~dp0.." || exit /b 1
set "TARGET=%~1"
set "BINARY=melodix-%TARGET%.exe"
if exist go.mod goto :build
if exist "%BINARY%" goto :success
echo ERROR: %BINARY% is missing. Extract the complete release archive.
set "BUILD_EXIT=1"
goto :done

:build
go build -trimpath -o "%BINARY%" "./cmd/%TARGET%"
set "BUILD_EXIT=%errorlevel%"
goto :done

:success
set "BUILD_EXIT=0"
:done
popd
exit /b %BUILD_EXIT%
