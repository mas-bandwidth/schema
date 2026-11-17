#!/bin/sh
# Split the complete ordered plan into two nonempty GitHub matrix arrays.
# Reading the same plan for both halves preserves each row and all its flags.
set -eu
part=${1:?missing partition}
case "$part" in 0|1) ;; *) echo "invalid Python CI partition" >&2; exit 1 ;; esac
jq -ce --argjson part "$part" '
  if type != "array" or length < 2 or length > 512 then error("Python CI plan must contain 2..512 rows")
  else [to_entries[] | select(.key % 2 == $part) | .value] end
  | if length > 256 then error("Python CI matrix exceeds 256 rows") else . end
'
