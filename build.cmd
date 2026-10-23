@echo off
setlocal

rem Run build.sh through Git Bash on Windows (cmd / PowerShell friendly).
set "BASH="
if defined BASH_EXE if exist "%BASH_EXE%" set "BASH=%BASH_EXE%"

if not defined BASH if exist "%ProgramFiles%\Git\bin\bash.exe" set "BASH=%ProgramFiles%\Git\bin\bash.exe"
if not defined BASH if exist "%ProgramFiles(x86)%\Git\bin\bash.exe" set "BASH=%ProgramFiles(x86)%\Git\bin\bash.exe"
if not defined BASH if exist "%LocalAppData%\Programs\Git\bin\bash.exe" set "BASH=%LocalAppData%\Programs\Git\bin\bash.exe"
if not defined BASH (
	for %%I in (bash.exe) do if not "%%~$PATH:I"=="" set "BASH=%%~$PATH:I"
)

if not defined BASH (
	echo [x] Git Bash not found.
	echo     Install Git for Windows: https://git-scm.com/download/win
	echo     Or set BASH_EXE to the full path of bash.exe.
	exit /b 1
)

echo %BASH% | findstr /I "\\System32\\bash.exe" >nul
if not errorlevel 1 (
	echo [x] Found WSL bash instead of Git Bash.
	echo     Windows desktop packaging needs Git Bash; install Git for Windows.
	exit /b 1
)

pushd "%~dp0" >nul
"%BASH%" ./build.sh %*
set "RC=%ERRORLEVEL%"
popd
exit /b %RC%
