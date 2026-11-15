package luautable_test

// THE ANNOUNCEMENT'S SHAPES ARE BOUNDED AS THE WIRE HOLDS THEM (docs/SPEC-
// TABLES.md §3.3, SECURITY): a packed width above what its kind can carry, a
// max above what its kind can carry, a reserved id as an entry's id, an id
// table carrying one id twice, and bytes left between the body's terminator
// and the id table are each malformed, and AnnounceRead refuses them whole.
// The probes below derive each hostile announcement from a lawful one and
// compare the Luau announcement read with the generated Go oracle on the same
// bytes. Verdicts only, never reason names (docs/rulings.json,
// refusal-verdict-only).

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math/big"
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
	"github.com/mas-bandwidth/schema/v2/ir"
)

const announceChecksSchema = `package probe

fixed table Root
{
    x uint32
}
`

const announceChecksOracle = `package probe

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestAnnounceChecksOracle(t *testing.T) {
	cases := [][2]string{
%s
	}
	for _, c := range cases {
		data, err := hex.DecodeString(c[1])
		if err != nil {
			t.Fatal(err)
		}
		var v TableVocabulary
		v.Init(make([]TableMessageEntry, 64))
		var report TableReport
		ok := AnnounceRead(&v, data, &report)
		fmt.Printf("%%s|accepted=%%t\n", c[0], ok && v.Announced)
	}
}
`

func announceChecksLeb(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

// announceChecksEncode is a lawful form-1 announcement: the build version
// under its reserved id, the vocabulary under its reserved id, the body's
// terminator, and the id table. strays rides between the terminator and the
// first id, where §3 allows nothing.
func announceChecksEncode(vocab []byte, ids []uint64, strays []byte) []byte {
	out := []byte{ir.TableWireForm}
	out = append(out, 1, uint8(ir.TableKindU64))
	out = binary.LittleEndian.AppendUint64(out, 7)
	out = append(out, 2, uint8(ir.TableKindArray))
	body := []byte{ir.TableKindU8}
	body = append(body, announceChecksLeb(uint64(len(vocab)))...)
	body = append(body, vocab...)
	out = append(out, announceChecksLeb(uint64(len(body)))...)
	out = append(out, body...)
	out = append(out, 0)
	out = append(out, strays...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

func announceChecksEntry(id uint64, kind uint8, shape ir.TableMessageShape) []byte {
	return ir.TableVocabularyEntry{Id: id, Kind: kind, Shape: shape}.Encode(nil)
}

func announceChecksCases() []struct {
	name              string
	lawful, malformed []byte
} {
	buildID := ir.TableBuildVersionWireId
	vocabID := ir.TableMessageVocabularyWireId
	ids := []uint64{buildID, vocabID}

	rangedU32 := func(bits int64) []byte {
		return announceChecksEntry(0x1111, ir.TableKindU32, ir.TableMessageShape{
			Packing: ir.TableMessageRanged,
			Bits:    bits,
			Base:    big.NewInt(0),
		})
	}
	stringMax := func(max int64) []byte {
		return announceChecksEntry(0x2222, ir.TableKindString, ir.TableMessageShape{Max: max})
	}
	arrayMax := func(max int64) []byte {
		return announceChecksEntry(0x3333, ir.TableKindArray, ir.TableMessageShape{
			Min:  0,
			Max:  max,
			Elem: ir.TableKindU8,
		})
	}
	reserved := func(id uint64) []byte {
		return announceChecksEntry(id, ir.TableKindU32, ir.TableMessageShape{})
	}

	return []struct {
		name              string
		lawful, malformed []byte
	}{
		{
			name:      "packed_bits_above_kind",
			lawful:    announceChecksEncode(rangedU32(32), ids, nil),
			malformed: announceChecksEncode(rangedU32(200), ids, nil),
		},
		{
			name:      "string_max_above_kind",
			lawful:    announceChecksEncode(stringMax(64), ids, nil),
			malformed: announceChecksEncode(stringMax(1<<31), ids, nil),
		},
		{
			name:      "array_max_above_kind",
			lawful:    announceChecksEncode(arrayMax(ir.TableMessageListMax), ids, nil),
			malformed: announceChecksEncode(arrayMax(1<<32), ids, nil),
		},
		{
			name:      "reserved_build_version_id",
			lawful:    announceChecksEncode(reserved(0x4444), ids, nil),
			malformed: announceChecksEncode(reserved(buildID), ids, nil),
		},
		{
			name:      "reserved_vocabulary_id",
			lawful:    announceChecksEncode(reserved(0x4444), ids, nil),
			malformed: announceChecksEncode(reserved(vocabID), ids, nil),
		},
		{
			name:      "id_table_repeats_an_id",
			lawful:    announceChecksEncode(reserved(0x4444), ids, nil),
			malformed: announceChecksEncode(reserved(0x4444), []uint64{buildID, vocabID, vocabID}, nil),
		},
		{
			name:      "bytes_after_the_terminator",
			lawful:    announceChecksEncode(reserved(0x4444), ids, nil),
			malformed: announceChecksEncode(reserved(0x4444), ids, []byte{0, 0, 0}),
		},
	}
}

// TestLuauMessageAnnounceMalformedShapes holds §3.3's announcement bounds and
// identity rules on the Luau leg: each malformed shape is refused exactly as
// the generated Go oracle refuses the same bytes, and the lawful announcement
// it was built from is accepted by both.
func TestLuauMessageAnnounceMalformedShapes(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter and the generated Go oracle")
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Root.schema")
	if err := os.WriteFile(schemaPath, []byte(announceChecksSchema), 0o644); err != nil {
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
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(luauDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cases := announceChecksCases()
	inputs := map[string][]byte{}
	var oracleCases strings.Builder
	names := make([]string, 0, 2*len(cases))
	for _, c := range cases {
		for _, kind := range []struct {
			suffix string
			data   []byte
		}{
			{"lawful", c.lawful},
			{"malformed", c.malformed},
		} {
			name := c.name + "_" + kind.suffix
			inputs[name] = kind.data
			names = append(names, name)
			fmt.Fprintf(&oracleCases, "\t\t{%q, %q},\n", name, hex.EncodeToString(kind.data))
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
	goFiles["oracle_test.go"] = []byte(fmt.Sprintf(announceChecksOracle, oracleCases.String()))
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestAnnounceChecksOracle$", "-v", ".")
	cmd.Dir = goDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Go oracle failed: %v\n%s", err, out)
	}
	want := map[string]string{}
	for line := range strings.SplitSeq(string(out), "\n") {
		if name, rest, ok := strings.Cut(line, "|accepted="); ok && (rest == "true" || rest == "false") {
			want[name] = rest
		}
	}
	if len(want) != len(inputs) {
		t.Fatalf("the oracle printed %d verdicts, want %d:\n%s", len(want), len(inputs), out)
	}
	for _, c := range cases {
		if want[c.name+"_malformed"] != "false" {
			t.Fatalf("%s: the oracle accepted the malformed announcement", c.name)
		}
		if want[c.name+"_lawful"] != "true" {
			t.Fatalf("%s: the oracle refused the lawful announcement", c.name)
		}
	}

	var probe strings.Builder
	probe.WriteString(`local M = require("./schema_message")
local inputs = require("./inputs")
local names = {
`)
	for _, name := range names {
		fmt.Fprintf(&probe, "\t%q,\n", name)
	}
	probe.WriteString(`}
for _, name in ipairs(names) do
	local v = M.NewVocabulary()
	local report = { malformed = false, verdict = 0, reason = "", unknown = 0 }
	local ok = M.AnnounceRead(v, inputs[name], report)
	print(name .. "|accepted=" .. tostring(ok and v.announced))
end
`)
	if err := os.WriteFile(filepath.Join(luauDir, "main.luau"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	luauOut := luaurun.Run(t, luauDir, "main.luau", inputs)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(luauOut)), "\n") {
		if line == "" {
			continue
		}
		name, rest, ok := strings.Cut(line, "|accepted=")
		if !ok {
			t.Fatalf("unexpected Luau line %q", line)
		}
		got[name] = rest
	}
	for _, name := range names {
		if got[name] != want[name] {
			t.Errorf("%s: Luau accepted=%s, Go accepted=%s", name, got[name], want[name])
		}
	}
}
