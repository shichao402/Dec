@echo off
setlocal

set SCRIPT_DIR=%~dp0

where py >nul 2>nul
if %ERRORLEVEL%==0 (
  py -3 "%SCRIPT_DIR%self_host_test.py" %*
  exit /b %ERRORLEVEL%
)

where python >nul 2>nul
if %ERRORLEVEL%==0 (
  python "%SCRIPT_DIR%self_host_test.py" %*
  exit /b %ERRORLEVEL%
)

echo Python interpreter not found (py/python).
exit /b 1
