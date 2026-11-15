package luautable_test

// A ranged array's announcement carries its min and max, and an array whose
// min exceeds its max is malformed on the announcement (docs/SPEC-TABLES.md
// §3.3). This test holds the Luau announcement read to the generated Go
// leg's verdict on both, and loads a batch naming an entry whose min equals
// its max.

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const minMaxSchema = `package probe

fixed table Box
{
    xs [2]int32
}
`

// The generated Go leg writes its own announcement and a batch of one Box.
// For each case the outer test supplies, it reads the announcement and prints
// the verdict this test compares Luau against.
const minMaxGoOracle = `package probe

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMinMaxOracle(t *testing.T) {
	var v Box
	values := []*Box{&v}
	n := BoxMeasureMessages(values, nil)
	batch := make([]byte, n)
	if got := BoxSaveMessages(values, batch, nil); got != n {
		t.Fatalf("save %d bytes, want %d", got, n)
	}
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	fmt.Printf("ann:%s\n", hex.EncodeToString(ann))
	fmt.Printf("batch:%s\n", hex.EncodeToString(batch))
	cases, err := os.ReadFile("cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(cases)), "\n") {
		name, text, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("case %q", line)
		}
		wire, err := hex.DecodeString(text)
		if err != nil {
			t.Fatal(err)
		}
		vocab := &TableVocabulary{}
		vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
		var report TableReport
		announced := AnnounceRead(vocab, wire, &report)
		fmt.Printf("@%s|announced=%v\n", name, announced)
	}
}
`

// minMaxArrayShape returns the offset of the first kind-14 shape's min in the
// announcement's vocabulary, found by walking the body and the entries.
func minMaxArrayShape(t *testing.T, ann []byte) int {
	t.Helper()
	if len(ann) < 2 || ann[0] != 1 {
		t.Fatalf("not a form-1 announcement: % x", ann)
	}
	at := 1
	start, end := -1, -1
	for {
		ref, next, ok := readCanonicalLeb(ann, at)
		if !ok {
			t.Fatalf("field reference at %d is not canonical", at)
		}
		at = next
		if ref == 0 {
			break
		}
		if at >= len(ann) {
			t.Fatalf("field reference %d runs off the body", ref)
		}
		kind := ann[at]
		at++
		switch kind {
		case 9:
			at += 8
		case 14:
			n, nnext, ok := readCanonicalLeb(ann, at)
			if !ok {
				t.Fatalf("vocabulary length at %d is not canonical", at)
			}
			if nnext >= len(ann) || ann[nnext] != 6 {
				t.Fatalf("vocabulary field at %d does not carry a byte array", at)
			}
			m, wstart, ok := readCanonicalLeb(ann, nnext+1)
			if !ok {
				t.Fatalf("vocabulary byte length at %d is not canonical", nnext+1)
			}
			start = wstart
			end = wstart + int(m)
			at = nnext + int(n)
		default:
			t.Fatalf("field reference %d has kind %d", ref, kind)
		}
	}
	if start < 0 || end > len(ann) {
		t.Fatalf("the announcement carries no vocabulary field: % x", ann)
	}
	for at := start; at < end; {
		if at+9 > end {
			t.Fatalf("entry at %d runs off the vocabulary", at)
		}
		kind := ann[at+8]
		_, n, ok := ir.DecodeShape(ann[at+9:end], kind)
		if !ok {
			t.Fatalf("entry at %d does not decode", at)
		}
		if kind == 14 {
			return at + 9
		}
		at += 9 + n
	}
	t.Fatal("the announcement carries no kind-14 entry")
	return 0
}

// TestLuauMessageAnnounceMinAboveMax holds docs/SPEC-TABLES.md §3.3's rule:
// an announcement whose counted-array entry has min above max is refused,
// while an entry whose min equals max is accepted and a batch naming it loads.
func TestLuauMessageAnnounceMinAboveMax(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the generated Go oracle")
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Box.schema")
	if err := os.WriteFile(schemaPath, []byte(minMaxSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}

	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	luauDir := t.TempDir()
	var mods []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(luauDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}

	ann := tablewire.Announce(u)
	minOff := minMaxArrayShape(t, ann)
	bad := append([]byte(nil), ann...)
	bad[minOff] = bad[minOff+1] + 1
	cases := []struct {
		name  string
		wire  []byte
		valid bool
	}{
		{"min_equals_max", ann, true},
		{"min_above_max", bad, false},
	}

	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	goTables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(goFiles, goTables)
	goRuntime := tfSerializeGoRuntime(t)
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", goRuntime))
	goFiles["oracle_test.go"] = []byte(minMaxGoOracle)
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var casesFile strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&casesFile, "%s:%s\n", c.name, hex.EncodeToString(c.wire))
	}
	if err := os.WriteFile(filepath.Join(goDir, "cases.txt"), []byte(casesFile.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestMinMaxOracle$", "-v", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	var batch []byte
	want := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "batch:"); ok {
			batch, err = hex.DecodeString(after)
			if err != nil {
				t.Fatalf("the oracle's batch: %v", err)
			}
		}
		if after, ok := strings.CutPrefix(line, "@"); ok {
			name, rest, ok := strings.Cut(after, "|")
			if !ok {
				t.Fatalf("unexpected oracle line %q", line)
			}
			want[name] = rest
		}
	}
	if len(batch) == 0 || len(want) != len(cases) {
		t.Fatalf("the oracle printed %d batch bytes and %d cases:\n%s", len(batch), len(want), out)
	}

	if err := os.WriteFile(filepath.Join(luauDir, "main.luau"), []byte(announceProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		announced, _ := luauAnnounce(t, luauDir, c.wire)
		got := fmt.Sprintf("announced=%t", announced)
		if got != want[c.name] {
			t.Errorf("%s: Luau %q, Go %q", c.name, got, want[c.name])
		}
		if c.valid != announced {
			t.Errorf("%s: Luau announced %t, want %t", c.name, announced, c.valid)
		}
	}

	for line := range strings.SplitSeq(strings.TrimSpace(messageMain(t, luauDir, mods, "Box", ann, batch)), "\n") {
		if strings.HasPrefix(line, "count ") {
			if line != "count 1 malformed false" {
				t.Fatalf("a batch naming the min-equals-max entry did not load: %q", line)
			}
			return
		}
	}
	t.Fatal("the load probe printed no count")
}
