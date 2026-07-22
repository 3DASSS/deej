@ECHO OFF

REM Thin wrapper around the Task build system (see Taskfile.yml). Kept so the
REM existing entrypoints and CI keep working. Prefers the standalone `task` CLI
REM and falls back to the copy bundled with the wails3 CLI.

IF "%1"=="dev" (
    SET "TASK_TARGET=windows:build:dev"
) ELSE IF "%1"=="release" (
    SET "TASK_TARGET=windows:build"
) ELSE (
    ECHO Usage: build.bat [dev^|release]
    EXIT /B 1
)

SET "DEEJ_ROOT=%~dp0..\.."
PUSHD "%DEEJ_ROOT%"

WHERE task >NUL 2>&1
IF %ERRORLEVEL%==0 (
    task %TASK_TARGET%
) ELSE (
    wails3 task %TASK_TARGET%
)

SET "BUILD_ERR=%ERRORLEVEL%"
POPD
EXIT /B %BUILD_ERR%
