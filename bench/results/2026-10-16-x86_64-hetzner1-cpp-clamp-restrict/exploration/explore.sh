#!/bin/sh
# Scratch exploration: the four clamp-counter forms under g++ and clang, pinned.
# Run from the repository root. Output under ../../explore/.
set -e
OUT=../../explore
mkdir -p $OUT
SER=../serialize
FLAGS="-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -O3 -DNDEBUG -DBENCH_MATCHED"
for cxx in g++ clang++; do
  extra=""
  [ $cxx = g++ ] && extra="-Wno-class-memaccess -Wno-type-limits"
  for form in plain restrict local both; do
    case $form in
      plain)    D="-DTABLE_CLAMP_RESTRICT= -DTABLE_CLAMP_LOCAL=0" ;;
      restrict) D="-DTABLE_CLAMP_RESTRICT=__restrict__ -DTABLE_CLAMP_LOCAL=0" ;;
      local)    D="-DTABLE_CLAMP_RESTRICT= -DTABLE_CLAMP_LOCAL=1" ;;
      both)     D="-DTABLE_CLAMP_RESTRICT=__restrict__ -DTABLE_CLAMP_LOCAL=1" ;;
    esac
    bin=$OUT/table-$cxx-$form
    $cxx "-DBENCH_OPT=\"O3\"" $FLAGS $extra $D -Igenerated/bench/paired/cpp -I$SER bench/tables/cpp/table_main.cpp -o $bin
    $bin --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus --read-arm --gate --packet-wire-dir testdata/wire --packet-variant-dir bench/corpus/variants > $OUT/gate-$cxx-$form.txt 2>&1 || { echo "GATE FAIL $cxx $form"; cat $OUT/gate-$cxx-$form.txt; }
  done
done
for r in 0 1 2; do
  for cxx in g++ clang++; do
    for form in plain restrict local both; do
      taskset -c 6 $OUT/table-$cxx-$form --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus --read-arm --csv --round $r --iterations 2000000 --packet-iterations 2000000 --packet-wire-dir testdata/wire --packet-variant-dir bench/corpus/variants | sed "s/^/$cxx,$form,/" >> $OUT/rows.csv
    done
  done
done
cat /proc/loadavg
