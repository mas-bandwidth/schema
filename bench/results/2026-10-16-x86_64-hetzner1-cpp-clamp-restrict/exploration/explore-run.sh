#!/bin/sh
# Scratch: run the built form binaries, 5 rounds, pinned to core 6. Run from repo root.
OUT=../../explore
f=$OUT/rows-quiet.csv
: > $f
for r in 0 1 2 3 4; do
  for cxx in g++ clang++; do
    for form in plain restrict local both; do
      taskset -c 6 $OUT/table-$cxx-$form --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus --read-arm --csv --round $r --iterations 2000000 --packet-iterations 2000000 --packet-wire-dir testdata/wire --packet-variant-dir bench/corpus/variants | grep -v ',lang,' | sed "s/^/$r,$cxx,$form,/" >> $f
    done
  done
done
cat /proc/loadavg
# best rate per (cxx,form,bench) -> ns/record
awk -F, '{k=$2","$3","$5; if($12>b[k]) b[k]=$12} END{for(k in b) printf "%s,%.1f\n", k, 1e9/b[k]}' $f | sort
