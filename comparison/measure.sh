#!/usr/bin/env bash
# Encode the same ShipCreate (see VALUES.md) in every format and print the
# sizes. Requires protoc, capnp and flatc on PATH, plus a C++ compiler and the
# serialize runtime checked out as a sibling of this repository.
set -euo pipefail
cd "$( dirname "$0" )"

SERIALIZE=${SERIALIZE:-../../serialize}
GEN=${GEN:-../generated/cpp}

if [ ! -d "$GEN" ]; then
    echo "generated C++ not found at $GEN - run 'make' in the repository root first" >&2
    exit 1
fi

echo "== schema"
c++ -std=c++17 -Wall -Wextra -O2 -I"$GEN" -I"$SERIALIZE" measure_schema.cpp -o /tmp/measure_schema
/tmp/measure_schema

echo "== protobuf"
protoc --encode=comparison.ShipCreate ship.proto < ship.pbtxt > ship.pb
echo "PROTOBUF: $( wc -c < ship.pb | tr -d ' ' ) bytes"

echo "== cap'n proto"
capnp encode ship.capnp ShipCreate < ship.capnp.txt > ship.capnp.bin
capnp encode --packed ship.capnp ShipCreate < ship.capnp.txt > ship.capnp.packed.bin
echo "CAPNP unpacked: $( wc -c < ship.capnp.bin | tr -d ' ' ) bytes"
echo "CAPNP packed:   $( wc -c < ship.capnp.packed.bin | tr -d ' ' ) bytes"

echo "== flatbuffers"
flatc --binary ship.fbs ship.json
echo "FLATBUFFERS: $( wc -c < ship.bin | tr -d ' ' ) bytes"
