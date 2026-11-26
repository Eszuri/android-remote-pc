@echo off
setlocal
cd /d "%~dp0"

echo Building and signing ARPC.apk...
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0android\build-apk.ps1"
if errorlevel 1 (
  echo [FAILED] APK build failed!
  pause
  exit /b 1
)

echo [OK] bin\ARPC.apk built successfully.
if "%~1"=="" timeout /t 2 >nul
