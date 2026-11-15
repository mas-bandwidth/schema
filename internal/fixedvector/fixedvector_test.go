package fixedvector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	manifest := "file=old_range_widen.bin row=range_widen side=old root=RangeWiden records=1 values=r0.lead=1,r0.v=100,r0.trail=2\n" +
		"file=hostile_range_widen.bin row=range_widen side=hostile root=RangeWiden records=1 forged=r0.v@104=150 values=r0.lead=1,r0.v=100,r0.trail=2\n" +
		"file=landed.bin row=range_widen side=hostile root=RangeWiden records=1 forged=r0.v@104=150 expect=r0.lead=1,r0.v=100,r0.trail=2 values=r0.lead=1,r0.v=100,r0.trail=2\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// the one read: the reference refused the forged bytes, so the line
	// carries no expect= and the answer is the refusal
	e, err := Read(dir, RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Refused || len(e.Paths) != 0 {
		t.Fatalf("a forged line with no expect= answered %+v, want Refused and no landed values", e)
	}
	if e.ForgedPath != "r0.v" || e.ForgedAt != 104 {
		t.Fatalf("forged = %s@%d, want r0.v@104", e.ForgedPath, e.ForgedAt)
	}
	// a line that carries expect= is the values the reference landed
	e, err = Read(dir, "landed.bin")
	if err != nil {
		t.Fatal(err)
	}
	if e.Refused {
		t.Fatal("a line with expect= read as a refusal")
	}
	if v, err := e.Int("r0.v"); err != nil || v != 100 {
		t.Fatalf("r0.v = %d (%v), want 100", v, err)
	}
	leaves, err := e.Leaves()
	if err != nil || len(leaves) != 3 || leaves[1].Field != "v" || leaves[1].Index != -1 || leaves[1].Value != 100 {
		t.Fatalf("leaves = %+v (%v), want lead, v = 100, trail", leaves, err)
	}
	if _, err := Read(dir, "old_range_widen.bin"); err == nil {
		t.Fatal("a line with no forged= read as a vector")
	}
	if _, err := Read(dir, "absent.bin"); err == nil {
		t.Fatal("a file the manifest does not name read as a vector")
	}
}

func TestLeavesArray(t *testing.T) {
	e := Expect{File: "x.bin", Values: map[string]int64{"r0.vals[2]": 100, "r0.lead": 1}, Paths: []string{"r0.lead", "r0.vals[2]"}}
	leaves, err := e.Leaves()
	if err != nil || len(leaves) != 2 || leaves[1].Field != "vals" || leaves[1].Index != 2 || leaves[1].Value != 100 {
		t.Fatalf("leaves = %+v (%v), want lead and vals[2] = 100", leaves, err)
	}
	for _, bad := range []string{"r1.v", "r0.a.b", "r0.vals[x]", "r0.vals[2"} {
		if _, err := (Expect{File: "x.bin", Values: map[string]int64{bad: 1}, Paths: []string{bad}}).Leaves(); err == nil {
			t.Errorf("%s split as a root leaf", bad)
		}
	}
}

func TestWriterRangeNamesEveryVector(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range WriterRange {
		if v.File == "" || v.Name == "" || v.Row == "" || v.Root == "" || seen[v.File] || seen[v.Name] {
			t.Errorf("vector %+v is incomplete or repeated", v)
		}
		seen[v.File] = true
		seen[v.Name] = true
	}
	for _, f := range []string{RangeWidenHostile, RangeWidenArrayHostile, RangeWidenU64Hostile} {
		if !seen[f] {
			t.Errorf("%s is not in WriterRange", f)
		}
	}
}

func TestReadCarriedText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := `file=deprecated_string.bin row=deprecated_string side=none root=FdRoot records=1 expect=r0.lead=11,r0.old_tag="h\,i\"\\\u0001",r0.old_tag_length=5 values=r0.lead=11` + "\n" +
		`file=open.bin row=open side=none root=FdRoot records=1 expect=r0.old_tag="hi values=r0.lead=11` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// a line with expect= and no forged= is a vector read as written
	e, err := Read(dir, DeprecatedString)
	if err != nil {
		t.Fatal(err)
	}
	if e.Refused || e.ForgedPath != "" {
		t.Fatalf("a carried line answered %+v, want landed and unforged", e)
	}
	if got := e.Texts["r0.old_tag"]; got != "h,i\"\\\x01" {
		t.Fatalf("r0.old_tag = %q, want the unescaped text", got)
	}
	if v, err := e.Int("r0.old_tag_length"); err != nil || v != 5 || len(e.Paths) != 2 {
		t.Fatalf("old_tag_length = %d (%v), paths %v: want 5 and two integer paths", v, err, e.Paths)
	}
	if _, err := Read(dir, "open.bin"); err == nil {
		t.Fatal("an unterminated text read as a value")
	}
}

func TestDeprecatedFieldNamesEveryVector(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, v := range DeprecatedField {
		if v.File == "" || v.Name == "" || v.Root == "" || v.Schema == "" || v.Field == "" || seen[v.File] || seen[v.Name] {
			t.Errorf("vector %+v is incomplete or repeated", v)
		}
		seen[v.File] = true
		seen[v.Name] = true
	}
	for _, f := range []string{DeprecatedScalar, DeprecatedString, DeprecatedArray, DeprecatedOptional, DeprecatedNested, DeprecatedUnion} {
		if !seen[f] {
			t.Errorf("%s is not in DeprecatedField", f)
		}
	}
}

// deprecatedStored is what each deprecated-field vector's record holds, stated
// here apart from the dumper: the deprecated field's NON-default value (the
// declared default beside it, from test/tables/FD1.schema) and the ordinary
// fields around it.
var deprecatedStored = map[string]struct {
	ints  map[string]int64
	texts map[string]string
	// leaf is the deprecated field's headline value and dflt what its
	// declared default would land there instead
	leaf string
	dflt string
}{
	DeprecatedScalar: {
		ints: map[string]int64{"r0.lead": 11, "r0.old_count": 99, "r0.trail": 22},
		leaf: "r0.old_count=99", dflt: "r0.old_count=4",
	},
	DeprecatedString: {
		ints:  map[string]int64{"r0.lead": 11, "r0.old_tag_length": 2, "r0.trail": 22},
		texts: map[string]string{"r0.old_tag": "hi"},
		leaf:  `r0.old_tag="hi"`, dflt: `r0.old_tag="abc"`,
	},
	DeprecatedArray: {
		ints: map[string]int64{"r0.lead": 11, "r0.old_marks[0]": 300, "r0.old_marks[1]": 400, "r0.old_marks_count": 2, "r0.trail": 22},
		leaf: "r0.old_marks_count=2", dflt: "r0.old_marks_count=0",
	},
	DeprecatedOptional: {
		ints: map[string]int64{"r0.lead": 11, "r0.old_bonus_present": 1, "r0.old_bonus": 77, "r0.trail": 22},
		leaf: "r0.old_bonus_present=1", dflt: "r0.old_bonus_present=0",
	},
	DeprecatedNested: {
		ints: map[string]int64{"r0.lead": 11, "r0.old_spot.x": 50, "r0.old_spot.y": 60, "r0.trail": 22},
		leaf: "r0.old_spot.x=50", dflt: "r0.old_spot.x=5",
	},
	DeprecatedUnion: {
		ints: map[string]int64{"r0.lead": 11, "r0.old_pick.type": 2, "r0.old_pick.spot.x": 70, "r0.old_pick.spot.y": 80, "r0.trail": 22},
		leaf: "r0.old_pick.type=2", dflt: "r0.old_pick.type=0",
	},
}

// TestDeprecatedFieldVectors holds the deprecated-field vectors to the rule
// they pin (docs/SPEC-TABLES.md §3.4, §21.2, §22.6; docs/rulings.json
// fixed-deprecated-field-is-carried): every file DeprecatedField names is in
// the C++ reference's built corpus, and its manifest line says the reference
// read it and landed the STORED values, the deprecated field's included,
// exactly as the writer stored them (`expect=` is `values=`).
func TestDeprecatedFieldVectors(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ reference's corpus (make tables-fixedform-corpus)")
	corpus := builtCorpus(t)
	lines := manifestLines(t, corpus)
	for _, vec := range DeprecatedField {
		want, ok := deprecatedStored[vec.File]
		if !ok {
			t.Errorf("%s: no stored values stated for it", vec.File)
			continue
		}
		if _, err := os.Stat(filepath.Join(corpus, vec.File)); err != nil {
			t.Errorf("%s is not in the built corpus: %v", vec.File, err)
			continue
		}
		e, err := Read(corpus, vec.File)
		if err != nil {
			t.Errorf("%s: %v", vec.File, err)
			continue
		}
		if e.Refused {
			t.Errorf("%s: the manifest line carries no expect=: the reference did not land the record", vec.File)
			continue
		}
		if len(e.Values) != len(want.ints) || len(e.Texts) != len(want.texts) {
			t.Errorf("%s: expect= lands %v %v, want %v %v", vec.File, e.Values, e.Texts, want.ints, want.texts)
		}
		for p, v := range want.ints {
			if got, err := e.Int(p); err != nil || got != v {
				t.Errorf("%s: expect= %s = %d (%v), want the stored %d", vec.File, p, got, err, v)
			}
		}
		for p, v := range want.texts {
			if got, ok := e.Texts[p]; !ok || got != v {
				t.Errorf("%s: expect= %s = %q (present %v), want the stored %q", vec.File, p, got, ok, v)
			}
		}
		line := lines[vec.File]
		expect, values := field(line, "expect="), field(line, "values=")
		if expect != values {
			t.Errorf("%s: expect= %s is not what the writer stored, values= %s", vec.File, expect, values)
		}
		if !strings.Contains(","+expect+",", ","+want.leaf+",") || strings.Contains(","+expect+",", ","+want.dflt+",") {
			t.Errorf("%s: expect= %s does not carry the stored %s (the declared default is %s)", vec.File, expect, want.leaf, want.dflt)
		}
		if !strings.Contains(line, " root="+vec.Root+" ") || !strings.Contains(line, " records=1 ") {
			t.Errorf("%s: the manifest line is not one %s record: %s", vec.File, vec.Root, line)
		}
	}
}

// builtCorpus is the C++ reference's corpus, built by make
// tables-fixedform-corpus. Missing, it fails by name when SCHEMA_REQUIRE_CORPUS
// is set, so no gate goes green by skipping.
func builtCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "build", "fixedform-corpus"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.txt")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's corpus is not in %s: %v", dir, err)
		}
		t.Skip("the reference's corpus is not built: make tables-fixedform-corpus")
	}
	return dir
}

// manifestLines is the corpus manifest's lines by file name.
func manifestLines(t *testing.T, corpus string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if name, ok := strings.CutPrefix(field(line, "file="), ""); ok && name != "" {
			out[name] = line
		}
	}
	return out
}

// field is a manifest line's `<key>` field: its value up to the next space,
// or the rest of the line for `values=`, the last field.
func field(line, key string) string {
	at := strings.Index(" "+line, " "+key)
	if at < 0 {
		return ""
	}
	rest := line[at+len(key):]
	if key == "values=" {
		return rest
	}
	v, _, _ := strings.Cut(rest, " ")
	return v
}
