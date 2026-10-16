#!/bin/sh
# wait for a quiet host before each form, then build + pinned direct run
cd ~/rowan-jobs/clamp-w1
quiet() {
  while :; do
    l=$(cut -d' ' -f1 /proc/loadavg)
    if pgrep -x "go|cc1|cc1plus|make|clang|clang++|cargo|rustc|dotnet|dartvm|test|opencode.bin" >/dev/null || [ $(echo "$l > 1.5" | bc) = 1 ]; then sleep 20; continue; fi
    return
  done
}
OUT=sittings/$1
for c in g++ clang++; do for f in 0 1 2 3; do
  ./build.sh $HOME/rowan-jobs/clamp-w1/cxx/$c-f$f > sittings/d-build.log 2>&1 || { echo "BUILD FAILED $c f$f" >> $OUT; continue; }
  quiet
  ./direct.sh d-$c-f$f 6 >> $OUT 2>&1
done; done
echo DONE >> $OUT
