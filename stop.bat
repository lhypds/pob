@echo off
rem Windows entry point for stop, like ./stop.sh on macOS and Linux.
rem Stops the Pob shell and the core process.
rem Forwards to win\stop.ps1; any arguments are passed through.

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0win\stop.ps1" %*
exit /b %ERRORLEVEL%
