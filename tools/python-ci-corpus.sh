#!/bin/sh
# A run-scoped reference corpus, built once at this source and runtime pin.
set -eu
archive=${2:-build/python-ci-fixed-corpus.tar}
source_head=$(git rev-parse HEAD)
test -z "$(git status --porcelain)" || { echo 'corpus: source checkout is dirty' >&2; exit 1; }
runtime_pin=$(awk '$1 == "../serialize" { print $4 }' SIBLINGS)
test -n "$runtime_pin"
mkdir -p build
stage=$(mktemp -d build/python-ci-corpus.XXXXXX)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
check_runtime() {
  test "$(git -C ../serialize rev-parse HEAD)" = "$runtime_pin" || { echo 'corpus: C++ runtime does not match SIBLINGS' >&2; exit 1; }
  test -z "$(git -C ../serialize status --porcelain)" || { echo 'corpus: C++ runtime checkout is dirty' >&2; exit 1; }
}
case ${1:-} in
  build)
    check_runtime
    # A fresh writer prevents a previous checkout's build products from
    # being relabelled with this source SHA. No interpreter is needed here.
    make -j2 -B tables-fixedform-corpus build/conformance-python SERIALIZE=../serialize
    test -s build/fixedform-corpus/manifest.txt
    test -d build/conformance-python
    cp -R build/fixedform-corpus "$stage/fixedform-corpus"
    cp -R build/conformance-python "$stage/conformance-python"
    printf '%s\n' "$source_head" > "$stage/source"
    printf '%s\n' "$runtime_pin" > "$stage/runtime"
    (cd "$stage" && find fixedform-corpus conformance-python -type f -print | LC_ALL=C sort | xargs sha256sum > contents.sha256)
    tar -cf "$archive" -C "$stage" .
    ;;
  restore)
    tar -xf "$archive" -C "$stage"
    test "$(cat "$stage/source")" = "$source_head" || { echo 'corpus: source SHA mismatch' >&2; exit 1; }
    test "$(cat "$stage/runtime")" = "$runtime_pin" || { echo 'corpus: C++ runtime pin mismatch' >&2; exit 1; }
    check_runtime
    test -s "$stage/contents.sha256"
    (cd "$stage" && sha256sum -c contents.sha256)
    test -s "$stage/fixedform-corpus/manifest.txt"
    test -f "$stage/fixedform-corpus/.stamp"
    test -d "$stage/conformance-python"
    rm -rf build/fixedform-corpus
    rm -rf build/conformance-python
    mv "$stage/fixedform-corpus" build/fixedform-corpus
    mv "$stage/conformance-python" build/conformance-python
    ;;
  *) echo 'usage: python-ci-corpus.sh build|restore [archive]' >&2; exit 2 ;;
esac
