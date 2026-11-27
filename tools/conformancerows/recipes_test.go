package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A C row that links only with its unit's compiled source, and then fails
// when it runs, is named by its own failure: the probe link before the unit
// objects (an undefined reference) never reaches the row's log, so it can
// never be the cause a ledger line states. The compiler is a shell script
// standing in for cc (no time, socket or network): it refuses a link without
// a unit object, and the program it links prints the row's failure.
func TestNativeRowCauseIsNotTheProbeLink(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, text string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), mode); err != nil {
			t.Fatal(err)
		}
	}
	mk("test/conformance/c/rows/X.c", "#include \"XTable.h\"\nint main( void ) { return 1; }\n", 0o644)
	mk("gen/x/XTable.h", "int x( void );\n", 0o644)
	mk("gen/x/XTable.c", "int x( void ) { return 0; }\n", 0o644)
	mk("fake-cc", `out=""; compile=0; units=0; prev=""
for a in "$@"; do
  [ "$prev" = "-o" ] && out="$a"
  [ "$a" = "-c" ] && compile=1
  case "$a" in */units/*) units=1;; esac
  prev="$a"
done
if [ $compile = 1 ]; then : > "$out"; exit 0; fi
if [ $units = 0 ]; then echo "X.c:(.text+0x1): undefined reference to x" >&2; exit 1; fi
printf '#!/bin/sh\necho "X: the planted failure" >&2\nexit 1\n' > "$out"; chmod +x "$out"
`, 0o755)
	t.Setenv("CC", "sh "+filepath.Join(root, "fake-cc"))
	t.Setenv("SERIALIZE_C", root)
	measured := filepath.Join(root, "measured.tsv")
	err := run(opts{root: root, leg: "c", shard: "0/1", jobs: 1, rows: "X", gen: "gen", work: filepath.Join(root, "work"), measured: measured})
	if err == nil {
		t.Fatal("a failing row passed")
	}
	got, rerr := os.ReadFile(measured)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(got), "X: the planted failure") || strings.Contains(string(got), "undefined reference") {
		t.Fatalf("the row's cause is not its own failure: %q", got)
	}
}
