# Build with GHC alone: no package index, no network (boot packages only).
GHC ?= ghc
OUT := build
FLAGS := -O1 -isrc -outputdir $(OUT) -Wall -Wno-name-shadowing -Wno-unused-matches -Wno-incomplete-uni-patterns -Wno-x-partial -Wno-unused-imports

all: $(OUT)/driver $(OUT)/unit

$(OUT)/driver: src/*.hs app/Driver.hs
	mkdir -p $(OUT)
	$(GHC) --make $(FLAGS) -iapp -o $@ app/Driver.hs

$(OUT)/unit: src/*.hs test/Unit.hs
	mkdir -p $(OUT)
	$(GHC) --make $(FLAGS) -itest -o $@ test/Unit.hs

test: $(OUT)/unit
	$(OUT)/unit

clean:
	rm -rf $(OUT)

.PHONY: all test clean
