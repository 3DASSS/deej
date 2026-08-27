#!/bin/sh

# Thin wrapper around the Task build system (see Taskfile.yml). Kept so the
# existing entrypoints and CI keep working. Prefers the standalone `task` CLI
# and falls back to the copy bundled with the wails3 CLI.

case "$1" in
    dev)     TASK_TARGET=linux:build:dev ;;
    release) TASK_TARGET=linux:build ;;
    *)       echo "Usage: $0 [dev|release]"; exit 1 ;;
esac

DEEJ_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$DEEJ_ROOT" || exit 1

if command -v task >/dev/null 2>&1; then
    exec task "$TASK_TARGET"
else
    exec wails3 task "$TASK_TARGET"
fi
