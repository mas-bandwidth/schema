package ctable

// fixedcount_op_reach — the c leg of SER-SCHEMA-05 part 2: a kTableFixedCount
// entry reads four bytes at its source whatever its size, so a count op must be
// bounded at src + 4 (not src + size) and a counted LOCAL against a PEER whose
// array is bare (te.size < 4) must refuse the plan rather than underflow the
// head subtraction and land a count op that over-reads the peer's record.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedCountOpReachCoversItsHeader(t *testing.T) {
	t.Parallel()

	// A kTableFixedCount entry reads four bytes at src whatever its size, so
	// the reach bound must add the header for a count exactly as for a text.
	if !strings.Contains(tableFixedRuntime, "( e.op == kTableFixedText || e.op == kTableFixedCount )") {
		t.Error("the reach check does not bound a kTableFixedCount entry at src + 4")
	}
	// The kind-14 compile step refuses a counted local against a peer array too
	// small to carry the four-byte count, before the head subtraction underflows.
	if !strings.Contains(tableFixedRuntime, "if ( d->counted && te.size < 4u ) { c->hostile = 1; break; }") {
		t.Error("kind-14 does not refuse a counted local field against a peer array smaller than the count")
	}
}

func TestFixedCountOpReachCoversItsHeaderASan(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c compiler (the leg's own toolchain)")

	reader := unitFrom(t, "package probe\n\nfixed table Root\n{\n    arr [..1]uint8\n}\n")
	peer := unitFrom(t, "package probe\n\nfixed table Root\n{\n    arr [1]uint8\n}\n")
	peerEntry, ok := FixedLineageOf(peer, "Root")
	if !ok {
		t.Fatal("no lineage entry for the bare-array peer")
	}
	lineage := map[string][]FixedLineageEntry{
		"Root": {{Wire: peerEntry.Wire, Layout: peerEntry.Layout, Record: peerEntry.Record}},
	}
	files, err := cgen.Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(reader, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("the reader generated no header")
	}

	probe := `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "` + header + `"

int main(void)
{
    static uint8_t file[1 << 16];
    Root back;
    TableReport r;
    static TableFixedEntry plan[4096];
    uint8_t * at;
    int64_t len, n;
    memset(file, 0, sizeof(file));
    file[0] = kTableFixedForm;
    table_fixed_put64(file + kTableFixedHashAt, root_fixed_known[0].hash);
    table_fixed_put32(file + kTableFixedHeaderBytes, (uint32_t) root_fixed_known[0].layout_bytes);
    memcpy(file + kTableFixedHeaderBytes + 4, root_fixed_known[0].layout, (size_t) root_fixed_known[0].layout_bytes);
    at = file + kTableFixedHeaderBytes + 4 + root_fixed_known[0].layout_bytes;
    table_fixed_put64(at, root_fixed_known[0].hash);
    at[8] = 0x7F; /* the bare [1]uint8 array element: the peer's last record */
    len = (int64_t) (at + 9 - file);

    memset(&back, 0, sizeof(back));
    memset(&r, 0, sizeof(r));
    n = root_fixed_load(&back, 1, file, len, plan, 4096, NULL, &r);
    if (n != -1) { fprintf(stderr, "count-op: loaded, want a refusal\n"); return 1; }
    if (!r.refused) { fprintf(stderr, "count-op: the read failed without a named refusal\n"); return 1; }
    if (r.reason != SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE) { fprintf(stderr, "count-op: reason %d, want record-too-large\n", (int) r.reason); return 1; }
    printf("count-op refused by name\n");
    return 0;
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	// The fixed loader is entirely static inline in <Base>Table.h, so the probe
	// compiles main.c alone — never the packet wire's ProbeWire.c, which pulls in
	// the sibling serialize.h checkout this leg only needs for 128-bit storage.
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-fsanitize=address", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-g")
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	output, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("execute: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "count-op refused by name") {
		t.Fatalf("the probe said %q", output)
	}
}
