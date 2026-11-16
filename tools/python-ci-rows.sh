#!/bin/sh
# Plan every prerequisite of test-python and every top-level Go test exactly once across bounded shards
# per package. The packet package has sixteen, to keep its heavy array and
# float tests apart; the table package has thirty-two; other packages use eight.
# The table corpus import test is split by its existing unit subtests, so each
# unit still runs both interpreters and strict mypy. New tests are added automatically.
# stale hand-maintained list. The unsplit make target stays the local/nightly
# contract; each support target is one named portion of its prerequisites.
set -eu
rows=$(mktemp)
packages=$(mktemp)
trap 'rm -f "$rows" "$packages"' EXIT HUP INT TERM
: > "$rows"
make -s python-ci-support-list | while IFS= read -r target; do
  corpus=false
  case "$target" in python-fixed-*) corpus=true ;; esac
  jq -cn --arg name "$target" --arg target "$target" --argjson corpus "$corpus" '{name:$name,target:$target,package:"",test:"",corpus:$corpus}' >> "$rows"
done
go list -tags schema_leg_python ./internal/codegen/python/... ./internal/codegen/pythontable/... > "$packages"
while IFS= read -r package; do
  corpus=false
  case "$package" in */internal/codegen/pythontable|*/internal/codegen/pythontable/*) corpus=true ;; esac
  tests=$(go test -tags schema_leg_python -list '^(Test|Example|Fuzz)' "$package")
  names=$(printf '%s\n' "$tests" | sed -n '/^\(Test\|Example\|Fuzz\)[A-Za-z0-9_]*$/p')
  # These fixed proof tests are delegated to the support row shards above.
  # Keeping a monolithic parent in an automatic package shard would
  # still hit the two-minute limit. New test names remain auto-discovered.
  case "$package" in */internal/codegen/pythontable)
    names=$(printf '%s\n' "$names" | sed '/^TestPythonFixedProofShapes$/d; /^TestPythonFixedProofShapesText$/d; /^TestPythonFixedProofShapesArrayBounds$/d; /^TestPythonFixedProofShapesNegativeControl$/d; /^TestPythonFixedProofValues$/d; /^TestPythonFixedProofValuesNegativeControl$/d; /^TestPythonFixedProofValuesTypes$/d; /^TestPythonFixedProofValuesReadRules$/d; /^TestPythonFixedProofValuesFloatBounds$/d; /^TestPythonFixedProofUnionsOptionals$/d; /^TestPythonFixedProofSuite$/d') ;;
  esac
  skip=
  if [ "$package" = "github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable" ]; then
    printf '%s\n' "$names" | grep -qx TestPythonVtFixedClassBuildVersionImports || { echo "missing TestPythonVtFixedClassBuildVersionImports" >&2; exit 1; }
    skip=TestPythonVtFixedClassBuildVersionImports
    units=$(awk '$1 == "unit" { print $2 }' testdata/conformance/tables/MANIFEST.txt)
    [ -n "$units" ] || { echo "missing table corpus units" >&2; exit 1; }
    if printf '%s\n' "$units" | grep -qv '^[A-Za-z0-9_]*$'; then echo "invalid table corpus unit name" >&2; exit 1; fi
    part=0
    while [ "$part" -lt 8 ]; do
      selected=$(printf '%s\n' "$units" | awk -v part="$part" '(NR-1)%8 == part' | paste -sd '|' -)
      if [ -n "$selected" ]; then
        jq -cn --arg name "python-build-version-imports-$part" --arg package "$package" --arg test "TestPythonVtFixedClassBuildVersionImports$/($selected)" --argjson corpus "$corpus" '{name:$name,target:"",package:$package,test:$test,corpus:$corpus}' >> "$rows"
      fi
      part=$((part+1))
    done
  fi
  if [ -z "$names" ]; then
    jq -cn --arg name "$package (compile)" --arg package "$package" --argjson corpus "$corpus" '{name:$name,target:"",package:$package,test:"",corpus:$corpus}' >> "$rows"
  else
    shards=8
    case "$package" in */internal/codegen/python) shards=16 ;; */internal/codegen/pythontable) shards=32 ;; esac
    shard=0
    while [ "$shard" -lt "$shards" ]; do
      selected=$(printf '%s\n' "$names" | awk -v shard="$shard" -v shards="$shards" -v skip="$skip" '(NR-1)%shards == shard && $0 != skip' | paste -sd '|' -)
      if [ -n "$selected" ]; then
        jq -cn --arg name "$package shard $((shard+1))/$shards" --arg package "$package" --arg test "($selected)" --argjson corpus "$corpus" '{name:$name,target:"",package:$package,test:$test,corpus:$corpus}' >> "$rows"
      fi
      shard=$((shard+1))
    done
  fi
done < "$packages"
jq -cs '.' "$rows"
