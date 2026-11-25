package gotable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// V1L is the C++ suite's LAWFUL older writer for V2 (test/tables/lawful/v1l,
// test/tables/fixedform_main.cpp v_case): V1 with every unlawful edit taken
// back. The Go reference holds the whole unlawful set
// (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH and EMIT, §5.4): a writer field,
// union arm, enum variant or keyed key the reader names nowhere, a kind off
// every ladder, a shrunk array, narrowed text. So V2, handed V1L's layout as a
// lineage entry, must LOAD a V1L record. A V1L that kept a narrowing — the
// Slot key Gamma V2 removed, or a [..6] V2 shrank to [..3] — is refused
// layout_malformed here, and this test is red.
func TestFixedV1LIsALawfulWriterForV2(t *testing.T) {
	t.Parallel()
	v1lPath := filepath.Join("..", "..", "..", "test", "tables", "lawful", "v1l", "V1L.schema")
	data, err := os.ReadFile(v1lPath)
	if err != nil {
		t.Fatal(err)
	}
	older := string(data)
	newer := readSchema(t, "V2")
	file := filepath.Join(t.TempDir(), "v1l.bin")

	write := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestWriteV1L(t *testing.T) {
	v := make([]Cfg, 1)
	CfgReset(&v[0])
	v[0].A = 42
	v[0].Tier = 77
	v[0].TierPresent = true
	buf := make([]byte, CfgFixedMeasure(1))
	if n := CfgFixedSave(v, buf); n != int64(len(buf)) {
		t.Fatalf("V1L save: %%d of %%d", n, len(buf))
	}
	if err := os.WriteFile(%[1]q, buf, 0o600); err != nil {
		t.Fatal(err)
	}
}
`, file)
	if out, err := runVersionProbe(t, older, nil, write); err != nil {
		t.Fatalf("V1L writer: %v\n%s", err, out)
	}

	load := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestV2ReadsV1L(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]Cfg, 1)
	CfgReset(&back[0])
	plan := make([]TableFixedEntry, 8192)
	var r TableReport
	n := CfgFixedLoad(back, data, plan, &r)
	if n != 1 || r.Err() != nil {
		t.Fatalf("V2 refused its LAWFUL older writer V1L: n=%%d err=%%v", n, r.Err())
	}
	if back[0].A != 42 || !back[0].TierPresent || back[0].Tier != 77 || !back[0].C {
		t.Fatalf("V2 read V1L wrong: %%+v", back[0])
	}
}
`, file)
	if out, err := runVersionProbe(t, newer, []string{older}, load); err != nil {
		t.Fatalf("V2 reads V1L: %v\n%s", err, out)
	}
}
