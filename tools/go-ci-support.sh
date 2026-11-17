#!/bin/sh
# Generated support artifacts are usable only by the exact source that emitted
# them. The manifest is also the complete local aggregate generation recipe.
set -eu
manifest=make/go-support-units.txt
command=${1:-}
group=${2:-}
build_dir=${3:-build}
case "$group" in base|evolution|collections) ;; *) echo 'unknown Go support group' >&2; exit 2 ;; esac
[ "$#" -le 3 ] || exit 2
pin=$(awk '$1 == "../serialize.go" { print $4 }' SIBLINGS)
[ -n "$pin" ] || { echo 'missing Go runtime pin' >&2; exit 1; }
revision="$build_dir/go-support-$group.revision"
runtime_record="$build_dir/go-support-$group.runtime"
inventory="$build_dir/go-support-$group.sha256"
units=$(awk -v group="$group" '$1 == group { print $0 }' "$manifest")
[ -n "$units" ] || { echo 'empty Go support group' >&2; exit 1; }
case "$command" in
  generate)
    [ "$build_dir" = build ] || exit 2
    printf '%s\n' "$units" | xargs -P 8 -n 5 sh -eu -c '
      group=$1; out=$2; module=$3; schema=$4; mode=$5
      if [ "$mode" = variable-r1 ]; then
        mkdir -p build/tables-r1-variable
        sed "s/^fixed table Cfg$/table Cfg/" "$schema" > build/tables-r1-variable/R1.schema
        grep -q "^table Cfg$" build/tables-r1-variable/R1.schema || { echo "R1 variable twin was not made variable" >&2; exit 1; }
        schema=build/tables-r1-variable/R1.schema
      else
        [ "$mode" = plain ] || exit 2
      fi
      ./bin/schema generate --lang go --out "$out" "$schema"
      printf '\''module %s\n\ngo 1.26\n\nrequire (\n\t// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => ../../../%s\n\n// the shared Go runtime is the default (docs/SPEC-TABLES.md §8.4)\nreplace github.com/mas-bandwidth/schema/v2 => ../../..\n'\'' "$module" "${SERIALIZE_GO:-../serialize.go}" > "$out/go.mod"
    ' sh
    exit 0 ;;
  produce)
    [ "$build_dir" = build ] || exit 2
    git diff --quiet HEAD -- || { echo 'Go support producer requires clean tracked source' >&2; exit 1; }
    # Always regenerate this group's units rather than package stale workspace
    # files. The producer emits checked schemas; it runs no native tests.
    printf '%s\n' "$units" | while read -r ignored out rest; do rm -rf "$out"; done
    rm -f "build/tables-generated-go/.support-$group.stamp"
    make "tables-go-support-$group"
    git rev-parse HEAD > "$revision"
    printf '%s\n' "$pin" > "$runtime_record"
    ;;
  verify) ;;
  *) echo 'usage: go-ci-support.sh generate|produce|verify group [build-dir]' >&2; exit 2 ;;
esac
[ -f "$revision" ] && [ "$(cat "$revision")" = "$(git rev-parse HEAD)" ] || { echo 'Go support revision mismatch' >&2; exit 1; }
[ -f "$runtime_record" ] && [ "$(cat "$runtime_record")" = "$pin" ] || { echo 'Go support runtime pin mismatch' >&2; exit 1; }
snapshot=$(mktemp)
trap 'rm -f "$snapshot"' EXIT HUP INT TERM
# Snapshot only this group's complete directories, including go.mod and every
# emitted file. Group directories are disjoint, so merged artifacts cannot
# silently supply a missing group's files from another producer.
printf '%s\n' "$units" | while read -r ignored out module schema mode; do
  relative=${out#build/}
  dir="$build_dir/$relative"
  [ -f "$dir/go.mod" ] && grep -Fxq "module $module" "$dir/go.mod" || { echo "Go support module missing: $module" >&2; exit 1; }
  find "$dir" -maxdepth 1 -type f -name '*.go' | grep -q . || { echo "Go support source missing: $module" >&2; exit 1; }
  (cd "$build_dir" && find "$relative" -type f ! -name .stamp -print0 | LC_ALL=C sort -z | xargs -0 sha256sum)
done > "$snapshot"
if [ "$command" = produce ]; then cp "$snapshot" "$inventory"; fi
[ -s "$inventory" ] && cmp -s "$snapshot" "$inventory" || { echo 'Go support inventory or bytes differ from its producer' >&2; exit 1; }
if [ "$command" = produce ]; then
  artifact="build/go-support-artifact-$group"
  rm -rf "$artifact"
  mkdir -p "$artifact"
  printf '%s\n' "$units" | while read -r ignored out rest; do
    relative=${out#build/}
    mkdir -p "$artifact/$(dirname "$relative")"
    cp -R "$out" "$artifact/$relative"
  done
  cp "$revision" "$runtime_record" "$inventory" "$artifact/"
fi
printf 'Go support %s verified for %s\n' "$group" "$(cat "$revision")"
