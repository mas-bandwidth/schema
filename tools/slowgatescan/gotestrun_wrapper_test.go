package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A recipe that runs go test through tools/gotestrun is still a go test recipe
// line to this scan: read as the wrapper's own spelling, it must come back as
// `go test`, or every wrapped recipe drops out of the scan unseen.
func TestReadLinesReadsTheGotestrunWrapperAsGoTest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.mk")
	text := "a:\n\tSCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -run '^TestA$$' ./p/\n" +
		"b:\n\t$(GOTESTRUN) -each -run X ./p/ ./q/\n" +
		"c:\n\tcd m && $(GOTESTRUN_ELSEWHERE) -run Y .\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readLines(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"a:", "\tSCHEMA_SLOW=1 go test -count=1 -run '^TestA$$' ./p/",
		"b:", "\tgo test -run X ./p/ ./q/",
		"c:", "\tcd m && go test -run Y .", "",
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines %q, want %d", len(lines), lines, len(want))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}
