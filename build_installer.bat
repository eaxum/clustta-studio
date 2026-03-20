@echo off
REM Build Clustta Studio Windows installer using Inno Setup
REM Prerequisites:
REM   1. Go toolchain installed
REM   2. Inno Setup 6 installed
REM   3. go-winres installed (go install github.com/tc-hib/go-winres@latest)
REM   4. Windows SDK installed (for signtool.exe)
REM
REM Version is automatically determined from the latest git tag.
REM Override: build_installer.bat 1.2.3

setlocal enabledelayedexpansion

if not "%~1"=="" (
    set VERSION=%~1
) else (
    for /f "tokens=*" %%i in ('git describe --tags --abbrev^=0 2^>nul') do set RAW=%%i
    if defined RAW (
        set VERSION=!RAW:~1!
    ) else (
        set VERSION=0.0.0
    )
)

echo === Generating Windows resources (v!VERSION!) ===
pushd cmd\studio_server
go-winres make --product-version !VERSION! --file-version !VERSION!
if %ERRORLEVEL% neq 0 (
    echo Resource generation failed!
    popd
    exit /b 1
)
popd

echo === Building clustta-studio-server.exe ===
go build -ldflags "-s -w -H windowsgui -X main.Version=!VERSION! -X main.DesktopMode=true" -o .\tmp\clustta-studio-server.exe .\cmd\studio_server
if %ERRORLEVEL% neq 0 (
    echo Build failed!
    exit /b 1
)

echo === Signing server binary ===
powershell -ExecutionPolicy Bypass -File .\windows-server-sign.ps1 .\tmp\clustta-studio-server.exe
if %ERRORLEVEL% neq 0 (
    echo Server binary signing failed!
    exit /b 1
)

echo === Creating installer (v!VERSION!) ===

REM Try common Inno Setup install locations
set ISCC=iscc.exe
where iscc >nul 2>&1
if %ERRORLEVEL% neq 0 (
    if exist "C:\Program Files (x86)\Inno Setup 6\ISCC.exe" (
        set "ISCC=C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
    ) else if exist "C:\Program Files\Inno Setup 6\ISCC.exe" (
        set "ISCC=C:\Program Files\Inno Setup 6\ISCC.exe"
    ) else (
        echo ERROR: Inno Setup not found. Install from https://jrsoftware.org/isdownload.php
        exit /b 1
    )
)

REM Configure SignTool for Inno Setup to sign the uninstaller
REM The $f placeholder is replaced by Inno Setup with the file to sign
set "SIGN_SCRIPT=%CD%\windows-server-sign.ps1"
"%ISCC%" /DMyAppVersion=!VERSION! /Ssigntool="powershell -ExecutionPolicy Bypass -File %SIGN_SCRIPT% $f" .\build\windows\installer.iss
if %ERRORLEVEL% neq 0 (
    echo Installer creation failed!
    exit /b 1
)

echo === Done! ===
