#!/bin/bash
# exp-again.sh: retake the __restrict experiment after an INVALID window, waiting
# for a quiet moment each time, up to 4 more tries; every try is kept.
set -uo pipefail
cd ~/rowan-jobs/rust-tables-read-w3e21/schema; echo $$ > ../exp.pid
BB='^(cc1|cc1plus|as|ld|collect2|link|compile|asm|vet|cgo|go|rustc|cargo|clang|clang\+\+|gcc|g\+\+|c\+\+|cc|make|ninja|cmake|javac|java|gradle|dotnet|node|npm|beam\.smp|erl|elixir|mix|tlc|perf|.*\.test|.*bench.*|table-.*|packet-.*|schema.*)$'
quiet() { local l; read -r l _ < /proc/loadavg; awk -v l=$l 'BEGIN{exit !(l<1.0)}' || return 1
  local c; for c in $(top -b -n 2 -d 5 -w 200 | awk '/^top -/{b++} b==2 && $1~/^[0-9]+$/ && $9+0>5 && $12!="top" {print $12}'); do [[ $c =~ $BB ]] && return 1; done; return 0; }
O=../hunt-b2/attempt-03
for t in 2 3 4 5; do
  until quiet; do sleep 20; done
  bash ../tools/sitting.sh exp-restrict-$t $O 7 7 table-rust table-cpp-clang exp-cpp-clang-restrict table-cpp-gcc exp-cpp-gcc-restrict | grep -E "window:|delta|foreign"
  grep -q "window: OK" $O/exp-restrict-$t.window.txt && { echo "FOUND exp-restrict-$t"; exit 0; }
done
echo "no valid experiment window"
