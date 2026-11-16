#!/bin/sh
# A CI artifact may replace the producer only for the exact checked-out source.
# The ordinary local make target still produces and tests its corpus together.
set -eu
command=${1:-}
build_dir=${2:-build}
corpus="$build_dir/fixedform-corpus"
revision="$build_dir/go-fixed-corpus.revision"
inventory="$build_dir/go-fixed-corpus.sha256"
runtime_record="$build_dir/go-fixed-corpus.runtime"
runtime_pin=$(awk '$1 == "../serialize" { print $4 }' SIBLINGS)
[ -n "$runtime_pin" ] || { echo "missing C++ runtime pin" >&2; exit 1; }
[ "$#" -le 2 ] || { echo 'usage: go-ci-corpus.sh produce|verify [build-dir]' >&2; exit 2; }
case "$command" in
  produce)
    [ "$build_dir" = build ] || { echo 'producer uses the Makefile build directory' >&2; exit 2; }
    git diff --quiet HEAD -- || { echo 'fixed corpus producer requires clean tracked source' >&2; exit 1; }
    [ "$(git -C ../serialize rev-parse HEAD)" = "$runtime_pin" ] && [ -z "$(git -C ../serialize status --porcelain)" ] || { echo 'C++ reference runtime differs from its pin' >&2; exit 1; }
    make -j2 -B tables-fixedform-corpus
    git rev-parse HEAD > "$revision"
    printf '%s\n' "$runtime_pin" > "$runtime_record"
    ;;
  verify) ;;
  *) echo 'usage: go-ci-corpus.sh produce|verify [build-dir]' >&2; exit 2 ;;
esac
[ -s "$corpus/manifest.txt" ] || { echo 'fixed corpus manifest is missing' >&2; exit 1; }
find "$corpus" -maxdepth 1 -name '*.bin' -type f | grep -q . || { echo 'fixed corpus has no wire files' >&2; exit 1; }
[ -f "$revision" ] && [ "$(cat "$revision")" = "$(git rev-parse HEAD)" ] || { echo 'fixed corpus revision does not match this checkout' >&2; exit 1; }
[ -f "$runtime_record" ] && [ "$(cat "$runtime_record")" = "$runtime_pin" ] || { echo "fixed corpus runtime pin mismatch" >&2; exit 1; }
snapshot=$(mktemp)
trap 'rm -f "$snapshot"' EXIT HUP INT TERM
(cd "$corpus" && find . -type f ! -name .stamp -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) > "$snapshot"
if [ "$command" = produce ]; then cp "$snapshot" "$inventory"; fi
[ -f "$inventory" ] && cmp -s "$snapshot" "$inventory" || { echo 'fixed corpus inventory or bytes differ from its producer' >&2; exit 1; }
printf 'fixed corpus verified for %s: %s files\n' "$(cat "$revision")" "$(wc -l < "$snapshot" | tr -d ' ')"
