#!/bin/bash
set -e
rm -rf "$REPO_PATH"
KOPIA_PASSWORD=dummy $KOPIA_EXE --config-file=benchmark.config repository create filesystem --path "$REPO_PATH"
[ -z "COLLECT_METRICS" ] && $KOPIA_EXE --config-file=benchmark.config snapshot create $HOME/backup-sources/isos --parallel=2 --no-auto-maintenance
echo OK.