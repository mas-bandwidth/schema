package javatable

// fixedroadmap_t01 — the java leg's file-envelope and plan-selection rows of
// docs/roadmap.sexp's node `fixed-tables`, the assertions no other test of this
// leg makes. Law: docs/FIXED-FORM-ALGORITHM.md §5.3 (the load steps and the two
// answer tables) and docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the
// hash"). One subtest per task id.
//
// Every forgery starts from the C++ reference's `old_nested_append.bin`, read by
// a build whose lineage holds that file's layout as a KNOWN entry, so the
// header's hash selects a lineage entry and a mutation of the layout bytes is a
// lie about a known version. Each subtest first reads the unmutated file, so a
// broken fixture cannot pass a refusal row by accident.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const (
	t01HeaderBytes = 16 // form byte, seven reserved, the hash
	t01LengthBytes = 4  // the layout's own u32 length
	t01EntryBytes  = 17 // id u64, kind u8, size u32, children u32
)

// t01Env is the one build and the one real file every subtest forges from.
type t01Env struct {
	classes string
	raw     []byte
	dir     string
}

// layoutLen is the layout length word of a well-formed file.
func (e *t01Env) layoutLen() int {
	return int(binary.LittleEndian.Uint32(e.raw[t01HeaderBytes:]))
}

// recordsAt is where the records begin in the unmutated file.
func (e *t01Env) recordsAt() int { return t01HeaderBytes + t01LengthBytes + e.layoutLen() }

// forge writes a mutated copy of the real file and answers its path.
func (e *t01Env) forge(t *testing.T, name string, mutate func(b []byte) []byte) string {
	t.Helper()
	path := filepath.Join(e.dir, name)
	if err := os.WriteFile(path, mutate(append([]byte(nil), e.raw...)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// requireNamedRefusal is §5.3's first row of the answer table: a refusal BY NAME
// sets refused and the name, never malformed, returns -1 and moves no counter.
func requireNamedRefusal(t *testing.T, what string, r probeRun, reason string) {
	t.Helper()
	if !r.refused || r.reason != reason {
		t.Errorf("%s: refused=%v reason=%s, want a refusal named %s", what, r.refused, r.reason, reason)
	}
	if r.malformed {
		t.Errorf("%s: malformed is set beside a reason; the report is two answers and never both", what)
	}
	if r.n != -1 {
		t.Errorf("%s: n=%d, want -1", what, r.n)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
		t.Errorf("%s: a counter moved: %+v; REFUSE is total", what, r)
	}
}

func TestFixedRoadmapT01Framing(t *testing.T) {
	t.Parallel()
	corpus := corpusDir(t)
	raw, err := os.ReadFile(filepath.Join(corpus, "old_nested_append.bin"))
	if err != nil {
		t.Fatalf("the corpus has no old_nested_append.bin: run `make tables-fixedform-corpus`: %v", err)
	}
	dir := t.TempDir()
	_, classes := buildRow(t, filepath.Join(dir, "build"), "nested_append", []sideSpec{
		{key: "reads", schema: "VNEW_nested_append.schema", older: []string{"VOLD_nested_append.schema"}},
		{key: "refuses", schema: "VOLD_nested_append.schema"},
	})
	if err := os.MkdirAll(filepath.Join(dir, "forged"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &t01Env{classes: classes, raw: raw, dir: filepath.Join(dir, "forged")}

	// The unmutated file must read: every refusal below is a refusal OF A MUTATION.
	if ok := runProbe(t, classes, "Probe_reads", env.forge(t, "intact.bin", func(b []byte) []byte { return b })); ok.n < 1 || ok.refused || ok.malformed {
		t.Fatalf("the unmutated file does not read: %+v", ok)
	}
	if env.recordsAt()+8 > len(raw) || env.layoutLen() < 4+t01EntryBytes {
		t.Fatalf("old_nested_append.bin is not shaped as the forgeries assume: layout %d, file %d", env.layoutLen(), len(raw))
	}

	// each subtest: the task id, and what it asserts.
	tests := []struct {
		id  string
		run func(t *testing.T, e *t01Env)
	}{
		{"java/F4", t01F4},
		{"java/F12", t01F12},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t, env)
		})
	}
}

// t01F4 is F4 "layout_malformed, truncated": ALGORITHM §5.3 step 3, "`20 + L`
// past the file | `layout_malformed` | the name". A file cut inside the layout,
// and a length word that claims more than the file holds, each refuse by name
// with nothing decoded; a file cut exactly after the layout fits and is not that
// refusal.
func t01F4(t *testing.T, e *t01Env) {
	at := e.recordsAt()
	for _, cut := range []int{t01HeaderBytes + t01LengthBytes, t01HeaderBytes + t01LengthBytes + 1, (t01HeaderBytes + t01LengthBytes + at) / 2, at - 1} {
		path := e.forge(t, "trunc.bin", func(b []byte) []byte { return b[:cut] })
		r := runProbe(t, e.classes, "Probe_reads", path)
		requireNamedRefusal(t, "truncated at "+strconv.Itoa(cut), r, "layoutMalformed")
		if r.layoutHash != "0x0" {
			t.Errorf("truncated at %d: layoutHash=%s, want 0x0: only the two layout refusals that name a hash carry it", cut, r.layoutHash)
		}
		ref := runProbe(t, e.classes, "Probe_refuses", path)
		requireNamedRefusal(t, "truncated at "+strconv.Itoa(cut)+" (refuses probe)", ref, "layoutMalformed")
		assertFresh(t, ref)
	}
	// A length word that runs past the file, the file itself whole, and the
	// largest u32 (read unsigned, so it cannot wrap to a small number).
	for _, claim := range []uint32{uint32(e.layoutLen()) + 1, 0xFFFFFFFF} {
		path := e.forge(t, "claim.bin", func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[t01HeaderBytes:], claim)
			return b
		})
		requireNamedRefusal(t, "length word "+strconv.Itoa(int(claim)), runProbe(t, e.classes, "Probe_reads", path), "layoutMalformed")
	}
	// The boundary: cut exactly after the layout, `20 + L` is the file's end.
	path := e.forge(t, "exact.bin", func(b []byte) []byte { return b[:at] })
	if r := runProbe(t, e.classes, "Probe_reads", path); r.reason == "layoutMalformed" || r.malformed {
		t.Errorf("a file ending exactly after its layout is `20 + L` inside the file, and must not answer layout_malformed or malformed: %+v", r)
	}
}

// t01F12 is F12 "second layout for a held hash": SPEC-TABLES §3.4, "A second
// layout for a hash already held is refused by name and changes nothing". The
// header's hash selects a held lineage entry; layout bytes that differ from the
// lock's (one byte of each field, and a different length) refuse
// `layout_malformed`, NOT `layout_newer`, with nothing decoded.
func t01F12(t *testing.T, e *t01Env) {
	count := (e.layoutLen() - 4) / t01EntryBytes
	offsets := []int{0} // the entry count word
	for i := 0; i < count; i++ {
		base := 4 + i*t01EntryBytes
		offsets = append(offsets, base, base+8, base+9, base+13) // id, kind, size, children
	}
	for _, off := range offsets {
		path := e.forge(t, "second.bin", func(b []byte) []byte {
			b[t01HeaderBytes+t01LengthBytes+off] ^= 0xFF
			return b
		})
		r := runProbe(t, e.classes, "Probe_reads", path)
		requireNamedRefusal(t, "layout byte "+strconv.Itoa(off)+" changed", r, "layoutMalformed")
		if r.layoutHash != "0x0" {
			t.Errorf("layout byte %d changed: layoutHash=%s, want 0x0", off, r.layoutHash)
		}
	}
	// A different LENGTH under the same held hash: one entry more, the length
	// word honest about it, so the framing check passes and step 7 is what refuses.
	longer := e.forge(t, "longer.bin", func(b []byte) []byte {
		at := e.recordsAt()
		out := append([]byte(nil), b[:at]...)
		out = append(out, make([]byte, t01EntryBytes)...)
		out = append(out, b[at:]...)
		binary.LittleEndian.PutUint32(out[t01HeaderBytes:], uint32(e.layoutLen()+t01EntryBytes))
		return out
	})
	requireNamedRefusal(t, "a longer layout under the held hash", runProbe(t, e.classes, "Probe_reads", longer), "layoutMalformed")
	// The contrast that makes the row discriminating: a hash held by NO entry is
	// the other name.
	stranger := e.forge(t, "stranger.bin", func(b []byte) []byte { b[t01HeaderBytes-8] ^= 0xFF; return b })
	if r := runProbe(t, e.classes, "Probe_reads", stranger); r.reason != "layoutNewer" {
		t.Errorf("an unheld hash: reason=%s, want layoutNewer, which is not the second-layout name", r.reason)
	}
}
