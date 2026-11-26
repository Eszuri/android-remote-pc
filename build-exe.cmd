@echo off
setlocal
cd /d "%~dp0"

set "APP_VER=1.0.0"
if exist "VERSION" set /p APP_VER=<VERSION
set "STAGE_EXE=%TEMP%\ARPC_stage_%RANDOM%.exe"

echo [1/3] Stopping running ARPC (if any)...
taskkill /F /IM ARPC.exe >nul 2>&1

echo [2/3] Compiling ARPC host binary (v%APP_VER%)...
if not exist "bin" mkdir "bin"
if exist "bin\ARPC.exe" del /F /Q "bin\ARPC.exe" >nul 2>&1
go build -ldflags "-s -w -H=windowsgui -X main.Version=%APP_VER%" -o "%STAGE_EXE%" .\cmd\arpc
if errorlevel 1 (
  echo [FAILED] Binary compilation failed!
  pause
  exit /b 1
)

echo [3/3] Packaging bin\ARPC-Setup.exe (v%APP_VER%)...
set "ISCC=C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
if not exist "%ISCC%" set "ISCC=%LOCALAPPDATA%\Programs\Inno Setup 6\ISCC.exe"
if not exist "%ISCC%" (
  del /F /Q "%STAGE_EXE%" >nul 2>&1
  echo [FAILED] Inno Setup 6 not found!
  pause
  exit /b 1
)
"%ISCC%" /Q "/DAppVer=%APP_VER%" "/DStageExe=%STAGE_EXE%" "arpc.iss"
set "ISCC_ERR=%ERRORLEVEL%"
del /F /Q "%STAGE_EXE%" >nul 2>&1
if not "%ISCC_ERR%"=="0" (
  echo [FAILED] Installer build failed!
  pause
  exit /b 1
)

echo [OK] bin\ARPC-Setup.exe (v%APP_VER%) built successfully.
if "%~1"=="" timeout /t 2 >nul
