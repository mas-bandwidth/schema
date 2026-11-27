# Fixture root for TestProofListResolvesDerivedNames: proof names computed by
# make functions, as make/go.mk's GO_ROWS_A does.
ROWS := $(sort $(shell sed -n 's/^func \(TestRow[A-Za-z0-9_]*\)(t \*testing\.T).*/\1/p' testpkg/*_test.go))
JUNK := $(shell echo TestRowOne notatest)
