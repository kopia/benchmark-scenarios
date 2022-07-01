#!/bin/bash
set -e
rm -rf "$REPO_PATH"
KOPIA_PASSWORD=dummy $KOPIA_EXE --config-file=benchmark.config repository create filesystem --path "$REPO_PATH"
$KOPIA_EXE --config-file=benchmark.config policy set --global --compression=zstd-fastest
[ -z "COLLECT_METRICS" ] && $KOPIA_EXE --config-file=benchmark.config snapshot create $HOME/backup-sources/vmdisk-sparse --parallel=2 --no-auto-maintenance
echo OK.