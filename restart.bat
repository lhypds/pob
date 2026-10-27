@echo off
rem Windows entry point for restart, like ./restart.sh on macOS and Linux.
rem Rebuilds and relaunches Pob in the background.
rem Forwards to win\restart.ps1; any arguments are passed through.

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0win\restart.ps1" %*
exit /b %ERRORLEVEL%
