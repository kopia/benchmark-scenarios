#!/bin/bash
set -e
rm -rf "$REPO_PATH"
KOPIA_PASSWORD=dummy $KOPIA_EXE --config-file=benchmark.config repository create filesystem --path "$REPO_PATH"
[ -z "COLLECT_METRICS" ] && $KOPIA_EXE --config-file=benchmark.config snapshot create $HOME/backup-sources/1mfiles-flat --parallel=4 --no-auto-maintenance
echo OK.