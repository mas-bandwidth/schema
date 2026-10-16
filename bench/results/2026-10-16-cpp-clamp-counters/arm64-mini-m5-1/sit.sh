#!/bin/sh
# usage: sit.sh <label> <CXX> <outdir>
set -e
cd ~/rowan-jobs/clamp-w1/schema
export PATH=$HOME/sdk/bin:$PATH GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 GOCACHE=$HOME/rowan-jobs/clamp-w1/gocache
export SERIALIZE=$HOME/rowan-jobs/clamp-w1/serialize CXX="$2"
LOAD=$(sysctl -n vm.loadavg)
go build -o bin/schema-paired ./bench/paired
bin/schema-paired -mode build -langs cpp
bin/schema-paired -mode gate -reuse-build -langs cpp
LOAD2=$(sysctl -n vm.loadavg)
exec bin/schema-paired -mode plan-read -langs cpp -quiet-window "rowan clamp-w1 $1 rev $(git rev-parse --short HEAD); UNPINNED (Darwin has no core affinity); load before build $LOAD, before measure $LOAD2" -out "$3"
