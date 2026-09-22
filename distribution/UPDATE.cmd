@echo off
setlocal
cd /d "%~dp0"
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0Install-OpenNoxHD.ps1" -Upgrade %*
set "result=%errorlevel%"
pause
exit /b %result%
