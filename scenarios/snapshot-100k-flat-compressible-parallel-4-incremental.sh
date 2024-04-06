#!/bin/bash
set -e
rm -rf "$REPO_PATH"
KOPIA_PASSWORD=dummy $KOPIA_EXE --config-file=benchmark.config repository create filesystem --path "$REPO_PATH"

# we create 2 backups from 2 different physical directories sharing 100k files
# with 2nd one having additional 50k more files
$KOPIA_EXE --config-file=benchmark.config snapshot create $HOME/backup-sources/100k-flat-compressible --parallel=4 --no-auto-maintenance --override-source /src1
$KOPIA_EXE --config-file=benchmark.config policy set --global --ignore-identical-snapshots true
[ -z "COLLECT_METRICS" ] && $KOPIA_EXE --config-file=benchmark.config snapshot create $HOME/backup-sources/150k-flat-compressible --parallel=4 --no-auto-maintenance --override-source /src1
echo OK.