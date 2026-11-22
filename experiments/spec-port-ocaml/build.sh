#!/bin/sh
# Build the OCaml port with ocamlopt and the standard library alone.
set -e
here=$(cd "$(dirname "$0")" && pwd)
out="$here/_build"
mkdir -p "$out"
mods="big ast parse model num timefmt wirecommon jsonw wire1 fixed jsonr"
for m in $mods driver unittest; do cp "$here/src/$m.ml" "$out/"; done
cd "$out"
ocamlopt -w +a-4-9-40-41-42-44-45-70-32-27-26-37-69-35 -I . -o driver.exe $(for m in $mods; do printf '%s.ml ' $m; done) driver.ml
ocamlopt -w +a-4-9-40-41-42-44-45-70-32-27-26-37-69-35 -I . -o unittest.exe $(for m in $mods; do printf '%s.ml ' $m; done) unittest.ml
