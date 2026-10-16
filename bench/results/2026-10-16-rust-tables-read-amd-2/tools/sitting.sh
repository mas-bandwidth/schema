#!/bin/bash
# sitting.sh NAME OUTDIR ROUNDS CORE BIN... : one paired, interleaved sitting with
# BENCH-STANDARD §2.6 control legs (the C++ family gen runner, round 0, pinned, at
# the start and at the end), §2.5 load recording at every round boundary, and the
# window verdict. Run from the schema checkout; BINs live in build/paired/sit/.
# The order of BINs rotates by one place each round (§2.4).
#
# The window gate follows the C++ read arm's (bench/paired, 2ac615b86): every
# control row (bench_mixed write and round_trip) must agree start-to-end within
# 5%, and a known BUILD or BENCH process seen above 5% CPU at any boundary
# refuses the sitting. Any other process above 5% is recorded with its CPU.
set -euo pipefail
NAME=$1; OUT=$2; ROUNDS=$3; CORE=$4; shift 4; BINS=("$@")
SIB=$(cat /sys/devices/system/cpu/cpu$CORE/topology/thread_siblings_list)
ITERS=4000000
TW="--indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus"
PW="--wire-dir testdata/wire --variant-dir bench/corpus/variants"
OWN=":(table-rust|table-cpp-clang|table-cpp-gcc|exp-cpp-clang-restrict|exp-cpp-gcc-restrict|packet-cpp-control):"
BUILDBENCH='^(cc1|cc1plus|as|ld|ld\.lld|collect2|link|compile|asm|vet|cgo|go|rustc|cargo|clang|clang\+\+|gcc|g\+\+|c\+\+|cc|make|ninja|cmake|javac|java|gradle|dotnet|node|npm|beam\.smp|erl|elixir|mix|tlc|perf|.*\.test|.*bench.*|table-.*|packet-.*)$'
mkdir -p "$OUT"
CSV=$OUT/$NAME.csv; LOG=$OUT/$NAME.load.txt; PRE=$OUT/$NAME.window.txt
statline() { awk -v c="cpu$1" '$1==c{print $2+$3+$4+$7+$8, $2+$3+$4+$5+$6+$7+$8}' /proc/stat; }
allbut() { awk -v c="cpu$1" '$1~/^cpu[0-9]+$/ && $1!=c {b+=$2+$3+$4+$7+$8; t+=$2+$3+$4+$5+$6+$7+$8} END{print b, t}' /proc/stat; }
foreign() {
  top -b -n 2 -d 1 -w 200 | awk '/^top -/{blk++} blk==2 && $1~/^[0-9]+$/ && $9+0>5 && $12!="top" {print $1":"$12":"$9}' \
    | { grep -v -E "$OWN" || true; } | tr "\n" " "; }
buildbench() { local n=0 p c; for p in $1; do c=$(echo "$p" | cut -d: -f2); [[ $c =~ $BUILDBENCH ]] && n=$((n+1)); done; echo $n; }
sample() { # label -> "load1 n_buildbench"
  local l f nb; read -r l _ < /proc/loadavg; f=$(foreign); nb=$(buildbench "$f")
  echo "$(date -u +%H:%M:%S) $1 load1=$l buildbench=$nb above5%: ${f}" >> "$LOG"
  echo "$l $nb"; }
control() { # label
  taskset -c "$CORE" build/paired/sit/packet-cpp-control --csv --round 0 $PW 2>/dev/null \
    | awk -F, -v L="$1" 'NR>1 && $2=="bench_mixed" {print L","$2","$3","$7}' >> "$OUT/$NAME.control.csv"; }
: > "$LOG"; echo "label,bench,path,rate" > "$OUT/$NAME.control.csv"; rm -f "$CSV.tmp"
read -r s0b s0t < <(statline "${SIB#*,}"); read -r a0b a0t < <(allbut "$CORE")
loads=(); nbb=0
r=($(sample start)); loads+=(${r[0]}); nbb=$((nbb+r[1])); L0=$(cat /proc/loadavg)
T0=$(date -u +%Y-%m-%dT%H:%M:%SZ)
control start
first=1
for ((k=1; k<=ROUNDS; k++)); do
  n=${#BINS[@]}
  for ((i=0; i<n; i++)); do
    b=${BINS[$(( (i + k - 1) % n ))]}
    taskset -c "$CORE" build/paired/sit/$b $TW --round $k --csv --iterations $ITERS 2>/dev/null \
      | awk -F, -v B="$b" -v K="$k" -v H="$first" 'NR==1{if(H==1)print "binary,round,"$0; next} {print B","K","$0}' >> "$CSV.tmp"
    first=0
  done
  r=($(sample "round-$k")); loads+=(${r[0]}); nbb=$((nbb+r[1]))
done
control end
r=($(sample end)); loads+=(${r[0]}); nbb=$((nbb+r[1])); L1=$(cat /proc/loadavg)
T1=$(date -u +%Y-%m-%dT%H:%M:%SZ)
read -r s1b s1t < <(statline "${SIB#*,}"); read -r a1b a1t < <(allbut "$CORE")
mv "$CSV.tmp" "$CSV"
lmax=$(printf "%s\n" "${loads[@]}" | sort -g | tail -1)
sib=$(awk -v a=$s0b -v b=$s1b -v c=$s0t -v d=$s1t 'BEGIN{printf "%.1f", 100*(b-a)/(d-c)}')
oth=$(awk -v a=$a0b -v b=$a1b -v c=$a0t -v d=$a1t 'BEGIN{printf "%.1f", 100*(b-a)/(d-c)}')
rowsfile=$(mktemp)
delta=$(awk -F, 'NR>1{r[$1","$3]=$4} END{m=0; for (p in r) {split(p,q,","); if (q[1]=="start") {e=r["end,"q[2]]; d=100*(e/r[p]-1); if (d<0) d=-d; printf "%s %.2f%% ", q[2], d > "/dev/stderr"; if (d>m) m=d}} printf "%.2f", m}' "$OUT/$NAME.control.csv" 2>"$rowsfile")
verdict=OK
awk -v d=$delta 'BEGIN{exit !(d>5)}' && verdict=INVALID
[ "$nbb" -gt 0 ] && verdict=INVALID
{
  echo "# sitting: $NAME   $T0 .. $T1 (UTC)"
  echo "# host: $(hostname) / $(uname -m) / $(uname -sr) / $(lscpu | sed -n 's/^Model name: *//p')"
  echo "# pinning: taskset -c $CORE (SMT siblings $SIB; no isolcpus on this host: the core is declared and its sibling measured, not kernel-isolated)"
  echo "# rounds: $ROUNDS   interleaved: yes (order rotates one place per round)   iterations: $ITERS"
  echo "# legs: ${BINS[*]}"
  echo "# control: build/paired/sit/packet-cpp-control ($(c++ --version | head -1), -O3 -DSERIALIZE_RELEASE; family gen bench_mixed rows), round 0, pinned, start and end"
  echo "# load_start: $L0"
  echo "# load_end:   $L1"
  echo "# load_max:   $lmax   (1-min average at every boundary; this sitting's own pinned leg is ~1.0 of it)"
  echo "# core_busy_pct: sibling cpu ${SIB#*,} $sib   all other cpus $oth"
  echo "# foreign_procs: $nbb   (build/bench processes above 5% CPU, summed over every boundary's 1 s sample; any refuses; other processes above 5% are listed in $NAME.load.txt)"
  echo "# control_delta_pct: $delta   (max over rows: $(cat "$rowsfile"))"
  echo "# window: $verdict"
} > "$PRE"
rm -f "$rowsfile"
cat "$PRE"
