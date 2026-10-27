@echo off
rem Windows entry point for start, like ./start.sh on macOS and Linux.
rem Builds and launches Pob detached. Pass -Count N to start several instances.
rem Forwards to win\start.ps1; any arguments are passed through.

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0win\start.ps1" %*
exit /b %ERRORLEVEL%
