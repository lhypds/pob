@echo off
rem Windows entry point for setup, like ./setup.sh on macOS and Linux.
rem Checks toolchains, then builds the Go core and the Windows shell.
rem Forwards to win\setup.ps1; any arguments are passed through.

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0win\setup.ps1" %*
exit /b %ERRORLEVEL%
