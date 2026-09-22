@echo off
setlocal
cd /d "%~dp0"
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0OpenNox-Launcher.ps1" -Resolution prompt -SpriteMode prompt -NoLaunch
set "result=%errorlevel%"
if "%result%"=="0" echo Settings saved. Start the game with START-OPENNOX.cmd.
pause
exit /b %result%
