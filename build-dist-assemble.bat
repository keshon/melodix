@echo off

rem
rem ASSEMBLE DISTRIBUTION PACKAGE
rem

rem Set the target platform and the path to UPX
set TARGET_PLATFORM=melodix-win
set UPX_PATH=tools\upx-3.96-win64\upx.exe

rem Get the current date in the format YYYY-MM-DD
for /f "tokens=1-3 delims=-" %%a in ('powershell -command "Get-Date -Format 'yyyy-MM-dd'"') do set CURRENT_DATE=%%a-%%b-%%c
set OUTPUT_ARCHIVE=dist\melodix-win-v%CURRENT_DATE%.zip

rem Run build-release.bat
call build-release.bat
if errorlevel 1 (
    echo "Build process failed."
    exit /b 1
)

rem Create or clear the "dist" directory for the target platform
if exist dist\%TARGET_PLATFORM% rmdir /s /q dist\%TARGET_PLATFORM%
mkdir dist\%TARGET_PLATFORM%

rem Copy specified files and folders
copy melodix.exe dist\%TARGET_PLATFORM%
copy README.md dist\%TARGET_PLATFORM%
copy LICENSE dist\%TARGET_PLATFORM%
copy .env.example dist\%TARGET_PLATFORM%\.env
xcopy /E /I /Y assets dist\%TARGET_PLATFORM%\assets

rem Use UPX packer if available, otherwise download and use it
if not exist %UPX_PATH% (
    echo "UPX not found. Downloading UPX..."
    mkdir tools
    powershell -command "& { Invoke-WebRequest -Uri 'https://github.com/upx/upx/releases/download/v3.96/upx-3.96-win64.zip' -OutFile 'tools\upx.zip' }"
    powershell -command "& { Expand-Archive -Path 'tools\upx.zip' -DestinationPath 'tools' }"
    del tools\upx.zip
)

rem Pack the binary with UPX
%UPX_PATH% --best dist\%TARGET_PLATFORM%\melodix.exe

rem Create a zip archive with version and current date
powershell -command "& { Compress-Archive -Path 'dist\%TARGET_PLATFORM%' -DestinationPath '%OUTPUT_ARCHIVE%' -Force}"