package luautable_test

import (
	"encoding/binary"
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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The generated Go oracle saves the unit's own announcement and reads a form-1
// wire whose id-table entry count (docs/SPEC-TABLES.md §3) is each hostile
// value, on a nine-byte wire and on the valid announcement. Its report is the
// expected answer; no verdict is typed into this test.
const negativeAnnounceCountGoOracle = `package probe

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestNegativeAnnounceCountOracle(t *testing.T) {
	ann := make([]byte, AnnounceMeasure())
	if n := Announce(ann); n != int64(len(ann)) {
		t.Fatalf("announce measure %d, wrote %d", len(ann), n)
	}
	fmt.Printf("valid:%s\n", hex.EncodeToString(ann))
	cases := []struct {
		name  string
		count uint64
	}{
		{"minus-one", ^uint64(0)},
		{"high-bit", uint64(1) << 63},
		{"past-end", uint64(len(ann)/8) + 1},
		{"minus-two", ^uint64(1)},
		{"high-bit-continuation", uint64(0x8080808080808080)},
	}
	for _, c := range cases {
		for _, shape := range []string{"nine", "long"} {
			var wire []byte
			if shape == "nine" {
				wire = make([]byte, 9)
				wire[0] = 1
			} else {
				wire = append([]byte(nil), ann...)
			}
			binary.LittleEndian.PutUint64(wire[len(wire)-8:], c.count)
			var storage [TableMessageEntriesHere]TableMessageEntry
			var v TableVocabulary
			v.Init(storage[:])
			var report TableReport
			fmt.Printf("@%s-%s|ok=%v\n", c.name, shape, AnnounceRead(&v, wire, &report))
		}
	}
	var storage [TableMessageEntriesHere]TableMessageEntry
	var v TableVocabulary
	v.Init(storage[:])
	var report TableReport
	fmt.Printf("@valid|ok=%v\n", AnnounceRead(&v, ann, &report))
}
`

const negativeAnnounceCountSchema = `package probe

fixed table Box
{
    a int32
    b string(8)
}
`

// TestLuauMessageAnnounceNegativeCount pins docs/SPEC-TABLES.md §3's trailer
// rule on the message form's announcement: the id table's entry count is an
// unsigned u64, and a count the buffer cannot hold is malformed, never a walk
// whose body reader leaves the buffer. The same unit's generated Go engine
// produces the valid announcement and the expected verdict for a count of -1,
// a count with its high bit set, a count one past what len/8 can hold, and two
// further negative counts; each must agree and raise no interpreter error.
func TestLuauMessageAnnounceNegativeCount(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Box.schema")
	if err := os.WriteFile(schemaPath, []byte(negativeAnnounceCountSchema), 0o644); err != nil {
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
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
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
	runtime := tfSerializeGoRuntime(t)
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	goFiles["oracle_test.go"] = []byte(negativeAnnounceCountGoOracle)
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestNegativeAnnounceCountOracle$", "-v", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	var valid []byte
	want := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "valid:"); ok {
			valid, err = hex.DecodeString(after)
			if err != nil {
				t.Fatalf("the oracle's valid announcement: %v", err)
			}
		}
		if after, ok := strings.CutPrefix(line, "@"); ok {
			name, rest, ok := strings.Cut(after, "|")
			if !ok {
				t.Fatalf("unexpected oracle line %q", line)
			}
			want[name] = name + "|" + rest
		}
	}
	cases := []struct {
		name  string
		count uint64
	}{
		{"minus-one", ^uint64(0)},
		{"high-bit", uint64(1) << 63},
		{"past-end", uint64(len(valid)/8) + 1},
		{"minus-two", ^uint64(1)},
		{"high-bit-continuation", uint64(0x8080808080808080)},
	}
	if len(valid) < 9 || len(want) != 2*len(cases)+1 {
		t.Fatalf("the oracle printed %d announcement bytes and %d cases:\n%s", len(valid), len(want), out)
	}
	if want["valid"] != "valid|ok=true" {
		t.Fatalf("the oracle refused its own announcement: %q", want["valid"])
	}
	inputs := map[string][]byte{"valid": valid}
	var names []string
	for _, c := range cases {
		for _, shape := range []string{"nine", "long"} {
			name := c.name + "-" + shape
			var wire []byte
			if shape == "nine" {
				wire = make([]byte, 9)
				wire[0] = 1
			} else {
				wire = append([]byte(nil), valid...)
			}
			binary.LittleEndian.PutUint64(wire[len(wire)-8:], c.count)
			inputs[name] = wire
			names = append(names, name)
			if want[name] != name+"|ok=false" {
				t.Fatalf("the oracle did not refuse %s: %q", name, want[name])
			}
		}
	}

	var main strings.Builder
	main.WriteString("local M = require(\"./schema_message\")\nlocal inputs = require(\"./inputs\")\nfor _, name in ipairs({")
	for i, name := range append([]string{"valid"}, names...) {
		if i > 0 {
			main.WriteString(", ")
		}
		fmt.Fprintf(&main, "%q", name)
	}
	main.WriteString("}) do\n")
	main.WriteString("\tlocal v = M.NewVocabulary()\n")
	main.WriteString("\tlocal report = { malformed = false, verdict = 0, reason = \"\", unknown = 0 }\n")
	main.WriteString("\tlocal ok = M.AnnounceRead(v, inputs[name], report)\n")
	main.WriteString("\tprint(name .. \"|ok=\" .. tostring(ok))\n")
	main.WriteString("end\n")
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	luauOut := luaurun.Run(t, g.dir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luauOut)), "\n") {
		if line == "" {
			continue
		}
		name, _, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("unexpected Luau line %q", line)
		}
		got[name] = line
	}
	for _, name := range append([]string{"valid"}, names...) {
		if got[name] != want[name] {
			t.Errorf("%s: Luau %q, Go %q", name, got[name], want[name])
		}
	}
}
