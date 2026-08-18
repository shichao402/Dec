@echo off
REM ensure_relkit_sparse entry (Windows). Prefer .venv, else system Python.
setlocal
set "SCRIPT_DIR=%~dp0"
if exist "%SCRIPT_DIR%..\.venv\Scripts\python.exe" (
    set "PYTHON=%SCRIPT_DIR%..\.venv\Scripts\python.exe"
) else (
    set "PYTHON=python"
)
"%PYTHON%" "%SCRIPT_DIR%ensure_relkit_sparse.py" %*
exit /b %ERRORLEVEL%
