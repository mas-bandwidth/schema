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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// deep runs the poisoned reader over a file with the TOTAL poison, the one a
// nested value needs (see poisonFinals in the probe).
func t01Deep(t *testing.T, classes, file string) probeRun {
	t.Helper()
	_, javaBin := javaTools(t)
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Probe_reads", file, "deep").CombinedOutput()
	if err != nil {
		t.Fatalf("java Probe_reads %s deep: %v\n%s", filepath.Base(file), err, out)
	}
	return parseProbe(t, string(out))
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
		{"java/R7", t01R7},
		{"java/R9", t01R9},
		{"java/R13", t01R13},
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

// t01R7 is R7 "the identity lane is an index comparison, never a recomputed
// hash": ALGORITHM §5.3, "a runtime NEVER computes a hash from layout bytes it
// holds — not for the IDENTITY LANE". The emitted load selects the lane by
// comparing the file's header hash with the reader's own hash constant, and
// neither it nor the lane selection reaches the runtime's hash function.
func t01R7(t *testing.T, e *t01Env) {
	u := versioningSchemaUnit(t, "VNEW_nested_append.schema")
	files, err := GenerateLineage(u, lineageOf(t, []string{"VOLD_nested_append.schema"}, 0))
	if err != nil {
		t.Fatal(err)
	}
	var load string
	for _, src := range files {
		s := string(src)
		if i := strings.Index(s, "public static int load("); i >= 0 {
			load = s[i:]
			if j := strings.Index(load, "\n    }\n"); j >= 0 {
				load = load[:j]
			}
		}
	}
	if load == "" {
		t.Fatal("no emitted load function found")
	}
	if !strings.Contains(load, "fileHash != hash") {
		t.Error("the identity lane is not selected by comparing the file's header hash with the reader's own hash constant")
	}
	for _, banned := range []string{"TableFixed.hash(", "fnv", ".hash(", "compile(", "parse("} {
		if strings.Contains(load, banned) {
			t.Errorf("load reaches %q: a runtime never derives a hash or walks a layout it holds", banned)
		}
	}
	// The behaviour the lane buys: the reader's own file reads on the identity
	// plan with the compile census untouched.
	intact := runProbe(t, e.classes, "Probe_reads", e.forge(t, "own.bin", func(b []byte) []byte { return b }))
	if intact.unknown != 0 || intact.kindMismatch != 0 || intact.refused || intact.n < 1 {
		t.Errorf("the own-lane read: %+v", intact)
	}
}

// t01R9 is R9 "a known hash with a different layout length or bytes →
// layout_malformed; the seven §1.1 malformations under a known hash all come back
// as this one name": ALGORITHM §5.3 step 7. Each of §1.1's seven rules is
// broken in the layout of a file whose header hash is held.
func t01R9(t *testing.T, e *t01Env) {
	entry := func(i int) int { return t01HeaderBytes + t01LengthBytes + 4 + i*t01EntryBytes }
	// a deep chain for rule 7: 70 single-child tables, the length word honest.
	chain := func(b []byte) []byte {
		const depth = 70
		layout := make([]byte, 4+t01EntryBytes*depth)
		binary.LittleEndian.PutUint32(layout, depth)
		for i := 0; i < depth; i++ {
			at := 4 + i*t01EntryBytes
			binary.LittleEndian.PutUint64(layout[at:], uint64(i+1))
			layout[at+8] = 13
			binary.LittleEndian.PutUint32(layout[at+9:], 8)
			if i+1 < depth {
				binary.LittleEndian.PutUint32(layout[at+13:], 1)
			}
		}
		out := append([]byte(nil), b[:t01HeaderBytes]...)
		out = append(out, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(out[t01HeaderBytes:], uint32(len(layout)))
		out = append(out, layout...)
		return append(out, b[e.recordsAt():]...)
	}
	rules := []struct {
		rule   string
		mutate func(b []byte) []byte
	}{
		{"1 count mismatch", func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[t01HeaderBytes+t01LengthBytes:], uint32((e.layoutLen()-4)/t01EntryBytes+1))
			return b
		}},
		{"1 count zero", func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[t01HeaderBytes+t01LengthBytes:], 0)
			return b
		}},
		{"2 kind unknown", func(b []byte) []byte { b[entry(1)+8] = 0xEE; return b }},
		{"3 size mismatch", func(b []byte) []byte { b[entry(1)+9]++; return b }},
		{"4 kind invalid", func(b []byte) []byte { b[entry(0)+8] = 9; return b }},
		{"5 tree unclosed", func(b []byte) []byte { b[entry(0)+13]++; return b }},
		{"6 record too large", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[entry(0)+9:], 70000); return b }},
		{"7 too deep", chain},
	}
	for _, r := range rules {
		path := e.forge(t, "rule.bin", r.mutate)
		got := runProbe(t, e.classes, "Probe_reads", path)
		requireNamedRefusal(t, "§1.1 rule "+r.rule, got, "layoutMalformed")
		ref := runProbe(t, e.classes, "Probe_refuses", path)
		requireNamedRefusal(t, "§1.1 rule "+r.rule+" (refuses probe)", ref, "layoutMalformed")
	}
}

// t01R13 is R13 "REFUSE is total: refused+reason and malformed are never both
// set, every counter stays zero, and not one destination byte is written":
// ALGORITHM §5.3's answer table. Each refusal by name that a single-record file
// can earn is read into storage poisoned with 0x5A, and every byte of it is
// still 0x5A. A multi-record `no_layout` is the bill's open question
// (FIXED-FORM-VERSIONING-TESTS.md, row 9) and is not asserted here.
func t01R13(t *testing.T, e *t01Env) {
	forged := map[string]struct {
		mutate func(b []byte) []byte
		reason string
	}{
		"layout_newer":     {func(b []byte) []byte { b[t01HeaderBytes-8] ^= 0xFF; return b }, "layoutNewer"},
		"layout_malformed": {func(b []byte) []byte { b[t01HeaderBytes+t01LengthBytes+4] ^= 0xFF; return b }, "layoutMalformed"},
		"layout_truncated": {func(b []byte) []byte { return b[:e.recordsAt()-1] }, "layoutMalformed"},
		"no_layout": {func(b []byte) []byte {
			for i := range 8 {
				b[e.recordsAt()+i] ^= 0xFF
			}
			return b
		}, "noLayout"},
		"newer_form": {func(b []byte) []byte { b[0] = 9; return b }, "newerForm"},
	}
	for name, f := range forged {
		path := e.forge(t, name+".bin", f.mutate)
		r := t01Deep(t, e.classes, path)
		requireNamedRefusal(t, name, r, f.reason)
		for _, field := range []string{"v.x", "v.y", "v.z", "v.w", "seq"} {
			if got, ok := r.value[field]; !ok || got != poison32 {
				t.Errorf("%s wrote the destination: %s=%q, want the 0x5A poison %s", name, field, got, poison32)
			}
		}
	}
	// The floor: a lineage whose oldest entry is retired refuses the file by name.
	_, retired := buildRow(t, filepath.Join(e.dir, "retired"), "nested_append", []sideSpec{
		{key: "reads", schema: "VNEW_nested_append.schema", older: []string{"VOLD_nested_append.schema"}, retire: 1},
	})
	r := t01Deep(t, retired, e.forge(t, "floor.bin", func(b []byte) []byte { return b }))
	requireNamedRefusal(t, "layout_unsupported", r, "layoutUnsupported")
	for _, field := range []string{"v.x", "v.y", "v.z", "v.w", "seq"} {
		if got := r.value[field]; got != poison32 {
			t.Errorf("layout_unsupported wrote the destination: %s=%q, want the 0x5A poison", field, got)
		}
	}
	// The other answer: a malformed read sets malformed and no refusal, and
	// writes nothing either.
	ragged := e.forge(t, "ragged.bin", func(b []byte) []byte { return append(b, 0) })
	m := t01Deep(t, e.classes, ragged)
	if !m.malformed || m.refused || m.n != -1 || m.reason != "none" {
		t.Errorf("a ragged tail: %+v, want malformed alone and -1", m)
	}
	if m.unknown != 0 || m.kindMismatch != 0 || m.widened != 0 || m.clamped != 0 {
		t.Errorf("a ragged tail moved a counter: %+v", m)
	}
	for _, field := range []string{"v.x", "v.y", "v.z", "v.w", "seq"} {
		if got := m.value[field]; got != poison32 {
			t.Errorf("a malformed read wrote the destination: %s=%q, want the 0x5A poison", field, got)
		}
	}
}
