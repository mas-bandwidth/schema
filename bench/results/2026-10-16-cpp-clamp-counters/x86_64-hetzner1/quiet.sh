#!/bin/sh
# usage: quiet.sh <label> <CXX> <outdir> <core> : wait for a quiet host, sit, retry an INVALID window (max 40 tries)
cd ~/rowan-jobs/clamp-w1
busy() { pgrep -x "go|cc1|cc1plus|make|clang|clang++|cargo|rustc|dotnet|dartvm|test|opencode.bin" >/dev/null || [ $(echo "$(cut -d' ' -f1 /proc/loadavg) > 1.5" | bc) = 1 ]; }
for try in $(seq 1 40); do
  while busy; do sleep 20; done
  sleep 15; busy && continue
  rm -rf "$3"
  ./sit.sh "$1 try$try" "$2" "$3" "$4" > "$3.log" 2>&1
  grep -q "^# window: OK" "$3/READ.md" 2>/dev/null && { echo "$1 OK try $try: $(grep '^cpp ' $3/READ.md)"; exit 0; }
  echo "$1 INVALID try $try: $(grep '^# window' $3/READ.md 2>/dev/null | cut -c1-140) $(tail -1 $3.log | cut -c1-140)"
done
exit 1
