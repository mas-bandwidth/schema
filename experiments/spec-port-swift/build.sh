#!/bin/sh
# Build the Swift spec-port driver: one native binary, every surface.
set -e
cd "$(dirname "$0")"
mkdir -p build
swiftc -O -target arm64-apple-macos15 -module-name SpecPortSwift Sources/*.swift -o build/conformance-swift
