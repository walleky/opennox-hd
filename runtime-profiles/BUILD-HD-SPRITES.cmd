@echo off
setlocal
cd /d "%~dp0"
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0Build-HD-Sprites.ps1" %*
set "result=%errorlevel%"
if not "%result%"=="0" echo Sprite generation failed. See the message above.
pause
exit /b %result%
