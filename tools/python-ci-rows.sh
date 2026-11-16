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
support=$(make -s python-ci-support-list)
printf '%s\n' "$support" | while IFS= read -r target; do
  if [ "$target" = python-ci-compiler ]; then
    # Group matching compiler tests into deterministic namespace shards.
    # Keep the local/nightly aggregate, but bound every per-commit row.
    listed=$(go test -tags schema_leg_python -list Python ./compiler)
    names=$(printf '%s\n' "$listed" | sed -n '/^\(Test\|Example\|Fuzz\)[A-Za-z0-9_]*$/p')
    [ -n "$names" ] || { echo "no Python compiler tests discovered" >&2; exit 1; }
    selected=$(printf '%s\n' "$names" | while IFS= read -r test; do
      if [ "$test" = TestPythonRequiredPublicShapes ] && printf '%s\n' "$support" | grep -qx python-ci-required-shape-0; then
        for part in 0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
          printf '%s\n' "$support" | grep -qx "python-ci-required-shape-$part" || { echo "missing required shape shard $part" >&2; exit 1; }
        done
        continue
      fi
      printf '%s\n' "$test"
    done)
    printf '%s\n' "$selected" | paste -d+ - - - - - | sed 's/+*$//' | while IFS= read -r group; do
      [ -n "$group" ] || continue
      jq -cn --arg name "python compiler $group" --arg target "python-ci-compiler-shard-$group" '{name:$name,target:$target,package:"",test:""}' >> "$rows"
    done
    continue
  fi
  jq -cn --arg name "$target" --arg target "$target" '{name:$name,target:$target,package:"",test:""}' >> "$rows"
done
go list -tags schema_leg_python ./internal/codegen/python/... ./internal/codegen/pythontable/... > "$packages"
while IFS= read -r package; do
  tests=$(go test -tags schema_leg_python -list '^(Test|Example|Fuzz)' "$package")
  names=$(printf '%s\n' "$tests" | sed -n '/^\(Test\|Example\|Fuzz\)[A-Za-z0-9_]*$/p')
  # Complete array-widening cases and pointer-array proof phases have their
  # own support rows; keeping either parent here would rerun the monolith.
  case "$package" in */internal/codegen/pythontable)
    names=$(printf '%s\n' "$names" | sed '/^TestPythonArrayWidening$/d; /^TestPythonVtPointerArrays$/d') ;;
  esac
  skip=
  if [ "$package" = "github.com/mas-bandwidth/schema/v2/internal/codegen/python" ]; then
    printf '%s\n' "$names" | grep -qx TestPythonPacketCore || { echo "missing TestPythonPacketCore" >&2; exit 1; }
    for part in 0 1 2 3 4 5 6 7; do
      [ "$(printf '%s\n' "$support" | grep -cx "python-ci-packet-core-$part")" -eq 1 ] || { echo "missing or duplicate packet core shard $part" >&2; exit 1; }
    done
    names=$(printf '%s\n' "$names" | sed '/^TestPythonPacketCore$/d')
  fi
  if [ "$package" = "github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable" ]; then
    printf '%s\n' "$names" | grep -qx TestPythonVtFixedClassBuildVersionImports || { echo "missing TestPythonVtFixedClassBuildVersionImports" >&2; exit 1; }
    for delegated in TestPythonScalarWidenHistorical TestPythonContainerWidenHistorical; do
      printf '%s\n' "$names" | grep -qx "$delegated" || { echo "missing $delegated" >&2; exit 1; }
    done
    for target in python-ci-widen-container python-ci-widen-scalar-0 python-ci-widen-scalar-1 python-ci-widen-scalar-2 python-ci-widen-scalar-3; do
      [ "$(printf '%s\n' "$support" | grep -cx "$target")" -eq 1 ] || { echo "missing or duplicate support row $target" >&2; exit 1; }
    done
    names=$(printf '%s\n' "$names" | sed '/^TestPythonScalarWidenHistorical$/d; /^TestPythonContainerWidenHistorical$/d')
    skip=TestPythonVtFixedClassBuildVersionImports
    units=$(awk '$1 == "unit" { print $2 }' testdata/conformance/tables/MANIFEST.txt)
    [ -n "$units" ] || { echo "missing table corpus units" >&2; exit 1; }
    if printf '%s\n' "$units" | grep -qv '^[A-Za-z0-9_]*$'; then echo "invalid table corpus unit name" >&2; exit 1; fi
    part=0
    while [ "$part" -lt 8 ]; do
      selected=$(printf '%s\n' "$units" | awk -v part="$part" '(NR-1)%8 == part' | paste -sd '|' -)
      if [ -n "$selected" ]; then
        jq -cn --arg name "python-build-version-imports-$part" --arg package "$package" --arg test "TestPythonVtFixedClassBuildVersionImports$/($selected)" '{name:$name,target:"",package:$package,test:$test}' >> "$rows"
      fi
      part=$((part+1))
    done
  fi
  if [ -z "$names" ]; then
    jq -cn --arg name "$package (compile)" --arg package "$package" '{name:$name,target:"",package:$package,test:""}' >> "$rows"
  else
    shards=8
    case "$package" in */internal/codegen/python) shards=16 ;; */internal/codegen/pythontable) shards=32 ;; esac
    shard=0
    while [ "$shard" -lt "$shards" ]; do
      selected=$(printf '%s\n' "$names" | awk -v shard="$shard" -v shards="$shards" -v skip="$skip" '(NR-1)%shards == shard && $0 != skip' | paste -sd '|' -)
      if [ -n "$selected" ]; then
        jq -cn --arg name "$package shard $((shard+1))/$shards" --arg package "$package" --arg test "^($selected)$" '{name:$name,target:"",package:$package,test:$test}' >> "$rows"
      fi
      shard=$((shard+1))
    done
  fi
done < "$packages"
jq -cs '.' "$rows"
