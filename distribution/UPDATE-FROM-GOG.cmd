@echo off
setlocal
cd /d "%~dp0"
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0Install-OpenNoxHD.ps1" -Upgrade -RefreshGameData -GenerateSprites %*
set "result=%errorlevel%"
if not "%result%"=="0" echo Update failed. See the message above.
pause
exit /b %result%
