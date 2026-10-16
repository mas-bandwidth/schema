#!/bin/sh
cd ~/rowan-jobs/clamp-w1
for s in 1 2; do for c in g++ clang++; do
  ./quiet.sh final-$c-s$s $c $HOME/rowan-jobs/clamp-w1/sittings/final-$c-s$s 6
done; done
echo DONE
