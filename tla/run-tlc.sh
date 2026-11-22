#!/usr/bin/env bash
# Run TLC over every config under tla/ and hold each run to its RUNS.tsv row.
# The pairing, the verdicts and the count are tools/tlcrun's; see its header.
#   bash tla/run-tlc.sh            every config
#   bash tla/run-tlc.sh -controls  prove the runner refuses three broken trees
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$here/.."
exec go run ./tools/tlcrun -tla "$here" "$@"
