package luautable_test

// An announced array whose element is itself an array (docs/SPEC-TABLES.md
// §3.3). tableMessageEntryRead reads the inner kind byte and keeps no inner
// shape. LoadMessages answers a value or a refusal for that announcement, the
// same answer the generated Go reader gives, and the interpreter does not
// raise. The three batches are one announcement with outer counts 0, 1 and 3.
// No verdict is typed here: the Go reader prints its own.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

const nestedArrayGoOracle = `package probe

import (
	"fmt"
	"os"
	"testing"
)

func TestNestedArrayOracle(t *testing.T) {
	t.Parallel()
	ann, err := os.ReadFile("ann")
	if err != nil {
		t.Fatal(err)
	}
	var storage [16]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	var arep TableReport
	if !AnnounceRead(&vocab, ann, &arep) || !vocab.Announced {
		t.Fatalf("AnnounceRead: %+v", arep)
	}
	for _, name := range []string{"count-0", "count-1", "count-3"} {
		batch, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var out [1]Root
		var rep TableReport
		_, ok := RootLoadMessages(out[:], &vocab, batch, &rep)
		reason := ""
		if err := rep.Err(); err != nil {
			reason = err.Error()
		}
		fmt.Printf("@%s|ok=%t|reason=%s\n", name, ok, reason)
	}
}
`

const nestedArraySchema = `package probe

fixed table Root {
    n int32
}
`

// nestedArrayWires is one announcement of an array of arrays, and one batch
// for each outer count. The element's own min and max ride; the inner kind
// is one byte and its shape does not (tableMessageEntryRead).
func nestedArrayWires(t *testing.T) (ann []byte, batches map[string][]byte) {
	t.Helper()
	const (
		outerMin = 0
		outerMax = 3
		innerMin = 0
		innerMax = 0
		fieldID  = 0x2000
	)
	entry := binary.LittleEndian.AppendUint64(nil, fieldID)
	entry = append(entry, 14)
	entry = append(entry, leb128(outerMin)...)
	entry = append(entry, leb128(outerMax)...)
	entry = append(entry, 14) // the element is itself an array
	entry = append(entry, leb128(innerMin)...)
	entry = append(entry, leb128(innerMax)...)
	entry = append(entry, 6) // inner kind byte, no inner shape

	vocabSub := append([]byte{6}, leb128(uint64(len(entry)))...)
	vocabSub = append(vocabSub, entry...)
	vocabField := append([]byte{2, 14}, leb128(uint64(len(vocabSub)))...)
	vocabField = append(vocabField, vocabSub...)
	versionField := binary.LittleEndian.AppendUint64([]byte{1, 9}, 0)

	ann = append([]byte{1}, versionField...)
	ann = append(ann, vocabField...)
	ann = append(ann, 0)
	ann = binary.LittleEndian.AppendUint64(ann, 0xfffffffffffffffe)
	ann = binary.LittleEndian.AppendUint64(ann, 0xfffffffffffffffd)
	ann = binary.LittleEndian.AppendUint64(ann, 2)

	// One vocabulary entry: the reference is one bit. The outer count is
	// bits_required(outerMin, outerMax) bits, then the body terminator.
	refBits := bitsRequired(1)
	countBits := bitsRequired(uint64(outerMax - outerMin))
	batches = map[string][]byte{}
	for _, count := range []struct {
		name  string
		value uint64
	}{
		{"count-0", 0},
		{"count-1", 1},
		{"count-3", 3},
	} {
		var payload []byte
		at := 0
		payload, at = appendBits(payload, at, 1, refBits)
		payload, at = appendBits(payload, at, count.value, countBits)
		payload, _ = appendBits(payload, at, 0, refBits)
		batches[count.name] = append([]byte{2, 0}, payload...)
	}
	return ann, batches
}

func leb128(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func bitsRequired(n uint64) int {
	bits := 0
	for n != 0 {
		bits++
		n >>= 1
	}
	return bits
}

func appendBits(dst []byte, bitLen int, value uint64, width int) ([]byte, int) {
	for (bitLen+width+7)/8 > len(dst) {
		dst = append(dst, 0)
	}
	for i := range width {
		if value&(uint64(1)<<uint(i)) != 0 {
			at := bitLen + i
			dst[at/8] |= 1 << uint(at%8)
		}
	}
	return dst, bitLen + width
}

// TestLuauMessageNestedArrayElement is a functional test: the pinned Luau
// interpreter, behind SCHEMA_SLOW=1. LoadMessages on an array of arrays
// agrees with the generated Go reader for outer counts 0, 1 and 3.
func TestLuauMessageNestedArrayElement(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the schema-go oracle")
	ann, batches := nestedArrayWires(t)

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Root.schema")
	if err := os.WriteFile(schemaPath, []byte(nestedArraySchema), 0o644); err != nil {
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
	sort.Strings(mods)

	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	goTables, err := gotable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(goFiles, goTables)
	goFiles["go.mod"] = []byte("module probe\n\ngo 1.26\n")
	goFiles["oracle_test.go"] = []byte(nestedArrayGoOracle)
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(goDir, "ann"), ann, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, batch := range batches {
		if err := os.WriteFile(filepath.Join(goDir, name), batch, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestNestedArrayOracle$", "-v", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		after, ok := strings.CutPrefix(line, "@")
		if !ok {
			continue
		}
		name, _, ok := strings.Cut(after, "|")
		if !ok {
			t.Fatalf("unexpected oracle line %q", line)
		}
		want[name] = after
	}
	if len(want) != 3 {
		t.Fatalf("the oracle printed %d cases:\n%s", len(want), out)
	}

	var probe strings.Builder
	probe.WriteString("local inputs = require(\"./inputs\")\nlocal M = require(\"./schema_message\")\n")
	for _, m := range mods {
		fmt.Fprintf(&probe, "require(\"./%s\")\n", m)
	}
	probe.WriteString(`local v = M.NewVocabulary()
local arep = { malformed = false, verdict = 0, reason = "", unknown = 0 }
if not M.AnnounceRead(v, inputs.ann, arep) then
	print("announce|ok=false|reason=" .. tostring(arep.reason) .. "|malformed=" .. tostring(arep.malformed))
	return
end
for _, name in ipairs({ "count-0", "count-1", "count-3" }) do
	local report = { malformed = false, verdict = 0, reason = "", unknown = 0, kindMismatch = 0, clamped = 0, widened = 0 }
	local called, count = pcall(function()
		local n = M.LoadMessages(v, "Root", inputs[name], 256, report)
		return n
	end)
	if not called then
		print(name .. "|raised")
	else
		local reason = report.reason or ""
		local accepted = type(count) == "number" and count >= 0 and reason == ""
		print(name .. "|ok=" .. tostring(accepted) .. "|reason=" .. reason)
	end
end
`)
	if err := os.WriteFile(filepath.Join(luauDir, "main.luau"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"ann": ann}
	maps.Copy(inputs, batches)
	luauOut := luaurun.Run(t, luauDir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luauOut)), "\n") {
		if line == "" {
			continue
		}
		name, _, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("unexpected Luau line %q\n%s", line, luauOut)
		}
		got[name] = line
	}
	for _, name := range []string{"count-0", "count-1", "count-3"} {
		if got[name] != want[name] {
			t.Errorf("%s: Luau %q, Go %q", name, got[name], want[name])
		}
	}
}
