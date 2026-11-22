# The Objective-C spec port: clang, Foundation, ARC. macOS only.
#   make            build/driver and build/unittest
#   make test       run the unit tests (no time, sockets, network)

CC      ?= clang
CFLAGS  ?= -O1 -g
FLAGS    = -fobjc-arc -fobjc-arc-exceptions -Wall -Wno-unused-function -Isrc
LIBS     = -framework Foundation
SRC      = src/SPSchema.m src/SPValue.m src/SPWire.m src/SPJson.m

all: build/driver build/unittest

build/driver: $(SRC) src/*.h driver/main.m
	@mkdir -p build
	$(CC) $(CFLAGS) $(FLAGS) $(SRC) driver/main.m $(LIBS) -o $@

build/unittest: $(SRC) src/*.h test/unittest.m
	@mkdir -p build
	$(CC) $(CFLAGS) $(FLAGS) $(SRC) test/unittest.m $(LIBS) -o $@

test: build/unittest
	./build/unittest

clean:
	rm -rf build

.PHONY: all test clean
