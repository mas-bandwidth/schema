#!/bin/sh
# Scratch: quiet wait, then the read-arm sittings, g++ 1, clang 1, g++ 2, clang 2.
cd "$(dirname "$0")/../schema/schema" || exit 1
export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 GOCACHE=$HOME/rowan-working/.cache/go-build
OUTROOT=../../scratch/sittings
mkdir -p $OUTROOT
busy() { # percent busy over 20 s from /proc/stat
  a=$(awk '/^cpu /{print $2+$3+$4+$6+$7+$8, $2+$3+$4+$5+$6+$7+$8}' /proc/stat); sleep 20
  b=$(awk '/^cpu /{print $2+$3+$4+$6+$7+$8, $2+$3+$4+$5+$6+$7+$8}' /proc/stat)
  echo "$a $b" | awk '{printf "%.1f", 100*($3-$1)/($4-$2)}'
}
quiet() {
  n=0
  while [ $n -lt 3 ]; do
    b=$(busy); l=$(cut -d' ' -f1 /proc/loadavg)
    c=$(pgrep -ix 'schema|go|compile|link|asm|make|cmake|ninja|cargo|rustc|dotnet|msbuild|vbcscompiler|csc|gmake|elixirc|swift|swiftc|xcodebuild|ld.lld|lld|cc|c\+\+|gcc|g\+\+|clang|clang\+\+|cc1|cc1plus|ld|javac|dart|mix|paired|conformance-harness|conformance-go' | wc -l)
    if [ "$c" -eq 0 ] && awk -v b=$b -v l=$l 'BEGIN{exit !(b<6 && l<2)}'; then n=$((n+1)); else n=0; fi
    echo "$(date -u +%H:%M:%S) busy=$b load1=$l competing=$c ok=$n"
  done
}
for s in "g++ gcc 1" "clang++ clang 1" "g++ gcc 2" "clang++ clang 2"; do
  set -- $s
  try=1
  while [ $try -le 6 ]; do
    quiet
    echo "START $2 sitting $3 try $try $(date -u +%FT%TZ)"
    rm -rf $OUTROOT/$2-sitting-$3
    CXX=$1 CC=$( [ $2 = gcc ] && echo gcc || echo clang ) taskset -c 6 sh bench/paired/nine.sh --read-arm --out $OUTROOT/$2-sitting-$3 \
      --receipt "READY $2 sitting $3: quiet window (3x20s <6% busy, load1<2, no refused process name), pinned taskset -c 6, schema $(git rev-parse HEAD)" \
      > $OUTROOT/$2-sitting-$3.log 2>&1
    rc=$?
    echo "END $2 sitting $3 try $try rc=$rc $(date -u +%FT%TZ) $(tail -1 $OUTROOT/$2-sitting-$3.log)"
    if [ -f $OUTROOT/$2-sitting-$3/READ.md ] && ! grep -q "window is INVALID" $OUTROOT/$2-sitting-$3/READ.md; then break; fi
    try=$((try+1))
  done
done
