@echo off
setlocal EnableExtensions
cd /d "%~dp0"

set "SRC=%~dp0backup\cup-latest"
if not exist "%SRC%\advisor.exe" (
  echo [rollback] missing %SRC%\advisor.exe
  exit /b 1
)

echo [rollback] restoring from %SRC%
if exist "%SRC%\advisor.exe"  copy /Y "%SRC%\advisor.exe"  "%~dp0advisor.exe"  >nul
if exist "%SRC%\bundle.json"  copy /Y "%SRC%\bundle.json"  "%~dp0bundle.json"  >nul
if exist "%SRC%\snapshot.json" copy /Y "%SRC%\snapshot.json" "%~dp0snapshot.json" >nul
if exist "%SRC%\config.yaml"  copy /Y "%SRC%\config.yaml"  "%~dp0config.yaml"  >nul
REM cmnet.db is optional: large; restore only if present in backup
if exist "%SRC%\cmnet.db" (
  echo [rollback] restoring cmnet.db ^(may take a moment^)
  copy /Y "%SRC%\cmnet.db" "%~dp0cmnet.db" >nul
)

echo [rollback] done. Stop any running advisor.exe, then start again from this folder.
echo [rollback] backup source: %SRC%
exit /b 0
