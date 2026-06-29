@echo off
setlocal

REM Build Micman2 as a Windows GUI app so it does not spawn a console window.
go test ./...
if errorlevel 1 exit /b %errorlevel%

go build -ldflags="-H=windowsgui" -o micman2.exe .
