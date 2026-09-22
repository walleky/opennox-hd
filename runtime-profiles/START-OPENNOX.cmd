@echo off
setlocal
cd /d "%~dp0"
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0OpenNox-Launcher.ps1" -SpriteMode prompt
if errorlevel 1 pause
