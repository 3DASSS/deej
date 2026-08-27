@ECHO OFF

REM Thin wrapper around the Task build system (see Taskfile.yml). Builds the
REM release exe and the Inno Setup installer via `windows:package`. Prefers the
REM standalone `task` CLI and falls back to the copy bundled with wails3.

SET "DEEJ_ROOT=%~dp0..\.."
PUSHD "%DEEJ_ROOT%"

WHERE task >NUL 2>&1
IF %ERRORLEVEL%==0 (
    task windows:package
) ELSE (
    wails3 task windows:package
)

SET "BUILD_ERR=%ERRORLEVEL%"
POPD
EXIT /B %BUILD_ERR%
