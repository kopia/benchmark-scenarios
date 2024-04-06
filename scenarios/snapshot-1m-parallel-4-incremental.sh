#!/bin/bash
set -e
rm -rf "$REPO_PATH"
KOPIA_PASSWORD=dummy $KOPIA_EXE --config-file=benchmark.config repository create filesystem --path "$REPO_PATH"

# we create 2 backups from 2 different physical directories:
# - first one has 1M files
# - second one has 0.5M more files, about 40K original files deleted and 0.5M updated in-place.
$KOPIA_EXE --config-file=benchmark.config snapshot create $HOME/backup-sources/1mfiles-flat --parallel=4 --no-auto-maintenance --override-source /src1
$KOPIA_EXE --config-file=benchmark.config policy set --global --ignore-identical-snapshots true
[ -z "COLLECT_METRICS" ] && $KOPIA_EXE --config-file=benchmark.config snapshot create $HOME/backup-sources/1_5mfiles-flat --parallel=4 --no-auto-maintenance --override-source /src1
echo OK.