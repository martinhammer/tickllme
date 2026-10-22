@echo off
rem SPDX-License-Identifier: GPL-3.0-or-later
rem Starts the Windows build of TickLLMe with the same arguments. It is x64;
rem Windows on Arm runs it too.
"%~dp0..\builds\windows-amd64\tickllme.exe" %*
exit /b %ERRORLEVEL%
