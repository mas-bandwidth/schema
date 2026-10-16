#!/bin/sh
# usage: sit.sh <compiler-label> <CXX> <outdir> <core>
set -e
cd ~/rowan-jobs/clamp-w1/schema
export PATH=/home/nova/sdk/bin:$PATH GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 GOCACHE=$HOME/rowan-jobs/clamp-w1/gocache
export SERIALIZE=$HOME/rowan-jobs/clamp-w1/serialize CXX="$2"
LOAD=$(cut -d" " -f1-3 /proc/loadavg)
go build -o bin/schema-paired ./bench/paired
bin/schema-paired -mode build -langs cpp
bin/schema-paired -mode gate -reuse-build -langs cpp
LOAD2=$(cut -d" " -f1-3 /proc/loadavg)
exec taskset -c $4 bin/schema-paired -mode plan-read -langs cpp -quiet-window "rowan clamp-w1 $1 rev $(git rev-parse --short HEAD); pinned taskset -c $4; load before build $LOAD, before measure $LOAD2; $(who | wc -l) sessions" -out "$3"
