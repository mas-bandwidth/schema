#!/bin/bash
# pmu.sh OUT: perf stat counters for each table leg (one --round, pinned), bracketed
# by the same start/end C++ control legs and load samples as sitting.sh. sudo perf
# reads the counters (perf_event_paranoid stays 4; no setting is changed).
set -uo pipefail
cd ~/rowan-jobs/rust-tables-read-w3e21/schema; O=$1; mkdir -p $O
TW="--indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus"
PW="--wire-dir testdata/wire --variant-dir bench/corpus/variants"
ctl() { taskset -c 7 build/paired/sit/packet-cpp-control --csv --round 0 $PW 2>/dev/null | awk -F, -v L=$1 'NR>1 && $2=="bench_mixed"{print L","$3","$7}'; }
{ echo "# pmu: $(hostname) $(date -u +%FT%TZ) load $(cut -d' ' -f1-3 /proc/loadavg)"; ctl start
  for b in table-rust table-cpp-clang table-cpp-gcc; do
    echo "## $b"; sudo -n perf stat -x, -e cycles,instructions,branches,branch-misses,L1-dcache-loads,L1-dcache-load-misses \
      taskset -c 7 build/paired/sit/$b $TW --round 1 --csv --iterations 4000000 2>&1 >/dev/null | grep -v "^$"
  done
  ctl end; echo "# end $(date -u +%FT%TZ) load $(cut -d' ' -f1-3 /proc/loadavg)"
  echo "# top >5% at end:"; top -b -n 2 -d 1 -w 200 | awk '/^top -/{b++} b==2 && $1~/^[0-9]+$/ && $9+0>5 {print "#   "$12" "$9}'
} > $O/pmu.txt
cat $O/pmu.txt
