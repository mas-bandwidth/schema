#!/bin/bash
# hunt.sh OUTROOT MAX_ATTEMPTS DEADLINE_EPOCH: wait for a quiet moment (load1 < 1.0 and
# no build/bench process above 5% CPU in a 5 s sample), then take base and confirm
# back to back, and the __restrict experiment after them. EVERY attempt is kept
# under OUTROOT/attempt-NN with its own verdicts; nothing is discarded. It stops at
# the first attempt whose base and confirm windows are both OK.
set -uo pipefail
export PATH=/home/nova/sdk/go1.26.6/bin:/home/nova/sdk/bin:$PATH
cd ~/rowan-jobs/rust-tables-read-w3e21/schema
ROOT=$1; MAX=$2; DEADLINE=$3; T=../tools/sitting.sh; mkdir -p "$ROOT"
echo $$ > ../hunt.pid
BB='^(cc1|cc1plus|as|ld|collect2|link|compile|asm|vet|cgo|go|rustc|cargo|clang|clang\+\+|gcc|g\+\+|c\+\+|cc|make|ninja|cmake|javac|java|gradle|dotnet|node|npm|beam\.smp|erl|elixir|mix|tlc|perf|.*\.test|.*bench.*|table-.*|packet-.*|schema.*)$'
quiet() {
  local l; read -r l _ < /proc/loadavg
  awk -v l=$l 'BEGIN{exit !(l<1.0)}' || return 1
  local c; for c in $(top -b -n 2 -d 5 -w 200 | awk '/^top -/{b++} b==2 && $1~/^[0-9]+$/ && $9+0>5 && $12!="top" {print $12}'); do
    [[ $c =~ $BB ]] && return 1; done
  return 0; }
for ((a=1; a<=MAX; a++)); do
  until quiet; do [ "$(date +%s)" -ge "$DEADLINE" ] && { echo "deadline: no quiet moment"; exit 2; }; sleep 20; done
  O=$(printf "%s/attempt-%02d" "$ROOT" $a); mkdir -p "$O"
  echo "attempt $a on $(hostname) at $(date -u +%H:%M:%S) load $(cut -d' ' -f1-3 /proc/loadavg)"
  bash $T base "$O" 11 7 table-rust table-cpp-clang table-cpp-gcc | grep -E "window:|control_delta|load_max|foreign"
  bash $T confirm "$O" 11 7 table-rust table-cpp-clang table-cpp-gcc | grep -E "window:|control_delta|load_max|foreign"
  if grep -q "window: OK" "$O/base.window.txt" && grep -q "window: OK" "$O/confirm.window.txt"; then
    bash $T exp-restrict "$O" 7 7 table-rust table-cpp-clang exp-cpp-clang-restrict table-cpp-gcc exp-cpp-gcc-restrict | grep -E "window:|control_delta"
    echo "FOUND $O"; exit 0
  fi
  [ "$(date +%s)" -ge "$DEADLINE" ] && { echo "deadline after attempt $a"; exit 2; }
done
echo "no valid pair in $MAX attempts"; exit 3
