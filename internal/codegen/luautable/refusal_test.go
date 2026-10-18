package luautable_test

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// strictProbe is the Luau main for one strict read: it loads every table
// module (which registers the descriptors), reads inputs.wire under
// refusal.luau's TableReadStrict at rootName and prints `read`, or the
// reason and the field it names (`-` for none).
func strictProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal R = require(\"./refusal\")\n")
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	fmt.Fprintf(&b, `local v, reason, field = R.TableReadStrict(inputs.wire, %q)
if reason == nil then
	if v == nil then print("nil root") else print("read") end
else
	if v ~= nil then print("refused with a root") end
	print(reason .. " " .. (field or "-"))
end
`, rootName)
	return b.String()
}

func runStrict(t *testing.T, g *generatedUnit, rootName string, wire []byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(strictProbe(g, rootName)), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})))
}

// strictReasonOf is what §6.5's strict read must answer for a reports.txt
// line u,k,w,c,d,m,verdict: the refusing counters in refusal.luau's order,
// damage first and evolution last; widened alone reads.
func strictReasonOf(t *testing.T, line string) string {
	t.Helper()
	f := strings.Split(line, ",")
	if len(f) != 7 {
		t.Fatalf("reports line %q is not u,k,w,c,d,m,verdict", line)
	}
	n := func(i int) int {
		v, err := strconv.Atoi(f[i])
		if err != nil {
			t.Fatalf("reports line %q: %v", line, err)
		}
		return v
	}
	switch {
	case f[5] == "true":
		return "malformed"
	case n(1) > 0:
		return "kind_mismatch"
	case n(3) > 0:
		return "clamped"
	case n(4) > 0:
		return "duplicate"
	case n(0) > 0:
		return "unknown"
	}
	return "read"
}

// the manifest's refused report rows, each a forged form byte, and the name
// §6.5 gives each (docs/SPEC-TABLES.md §3, §6.5's TableRefuseReason rows)
var formCases = map[string]string{
	"form_zero":       "unknown_form",
	"form_six":        "newer_form",
	"form_ff":         "newer_form",
	"message_as_file": "message_form_as_file",
}

// TestLuauStrictReadRefusesByName drives refusal.luau's TableReadStrict, the
// strict read (docs/SPEC-TABLES.md §6.5), over the REAL wires the tables
// conformance manifest names, under the landed tolerant load: every report
// row with a counter nonzero refuses naming that counter and, for a field
// event, the root field that raised it; a row whose only counter is widened,
// and every instance row, reads. The form byte's refusals (§3) are the
// manifest's own forged rows plus a forged byte 3 at a variable root and a
// form-1 file at a fixed root (previous_form), and §2.14's
// integrity_mismatch is a CRC-32C trailer flipped, cut and missing.
func TestLuauStrictReadRefusesByName(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := manifestRows(t)
	expect := reportsExpect(t)
	cache := map[string]*generatedUnit{}
	hit := map[string]int{}
	var refusedUnits, reads int
	var variableWire []byte
	var variableUnit *generatedUnit
	var variableRoot string
	for _, row := range rows {
		if _, ok := units[row.unit]; !ok {
			continue
		}
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			cache[row.unit] = g
		}
		if g.err != nil {
			refusedUnits++
			continue
		}
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatalf("case %s: %v", row.caseName, err)
		}
		got := runStrict(t, g, row.root, wire)
		if row.kind == "instance" {
			if got != "read" {
				t.Errorf("instance %s: the strict read refused a wire its own unit wrote: %s", row.caseName, got)
			}
			if variableWire == nil {
				variableWire, variableUnit, variableRoot = wire, g, row.root
			}
			reads++
			continue
		}
		if want, ok := formCases[row.caseName]; ok {
			if got != want+" -" {
				t.Errorf("report %s: got %q, want %q", row.caseName, got, want+" -")
			}
			hit[want]++
			continue
		}
		line, ok := expect[row.caseName]
		if !ok {
			t.Errorf("report %s: reports.txt names no such case", row.caseName)
			continue
		}
		want := strictReasonOf(t, line)
		if want == "read" {
			if got != "read" {
				t.Errorf("report %s (%s): a read with no refusing counter was refused: %s", row.caseName, line, got)
			}
			reads++
			continue
		}
		reason, field, _ := strings.Cut(got, " ")
		if reason != want {
			t.Errorf("report %s (%s): got %q, want the reason %s", row.caseName, line, got, want)
			continue
		}
		if want != "malformed" && !strings.HasPrefix(field, row.root+".") {
			t.Errorf("report %s (%s): the refusal %s names no field of %s: %q", row.caseName, line, want, row.root, field)
			continue
		}
		hit[want]++
		t.Logf("report %s (%s): %s", row.caseName, line, got)
	}
	if variableWire == nil {
		t.Fatal("no instance row read, so no clean wire to forge")
	}

	// DUPLICATE: the manifest carries no duplicate-key case, so the one
	// event the wire counts duplicate (two map entries claiming one key,
	// §2.8, §4) is load_test.go's duplicateMapWire, and the field is Box.m.
	box := generateFiles(t, map[string]string{"Dup.schema": "package dup\ntable Box\n{\n    m map[uint8]uint8\n}\n"})
	if got := runStrict(t, box, "Box", duplicateMapWire(t)); got != "duplicate Box.m" {
		t.Errorf("a duplicate map key: got %q, want %q", got, "duplicate Box.m")
	} else {
		hit["duplicate"]++
	}

	// THE FORM BYTE, forged on a clean wire of Box, a root that does not
	// declare `fixed` (§3, §6.5): the fixed form there is previous_form, and
	// so on down the registry.
	boxWire := boxCleanWire()
	if got := runStrict(t, box, "Box", boxWire); got != "read" {
		t.Errorf("the control, a clean Box: got %q, want read", got)
	}
	forged := map[byte]string{3: "previous_form", 2: "message_form_as_file", 0: "unknown_form", 9: "newer_form"}
	for b, want := range forged {
		w := append([]byte(nil), boxWire...)
		w[0] = b
		if got := runStrict(t, box, "Box", w); got != want+" -" {
			t.Errorf("form byte %d at Box: got %q, want %q", b, got, want+" -")
		} else {
			hit[want]++
		}
	}
	if got := runStrict(t, variableUnit, variableRoot, nil); got != "malformed -" {
		t.Errorf("an empty file: got %q, want %q", got, "malformed -")
	}
	// a `fixed table` root: its form-1 file reads here, and its own form 3
	// is the fixed form's runtime's to read, a form THIS call does not carry
	fixed := generateFiles(t, map[string]string{"Fix.schema": "package fix\nfixed table Pin\n{\n    x uint8\n}\n"})
	pin := []byte{1, 1, 6, 7, 0}
	pin = binary.LittleEndian.AppendUint64(pin, ir.TableWireId("x"))
	pin = binary.LittleEndian.AppendUint64(pin, 1)
	if got := runStrict(t, fixed, "Pin", pin); got != "read" {
		t.Errorf("a form-1 file at a fixed table root: got %q, want read", got)
	}
	pin[0] = 3
	if got := runStrict(t, fixed, "Pin", pin); got != "unknown_form -" {
		t.Errorf("a form-3 byte at a fixed table root: got %q, want %q", got, "unknown_form -")
	} else {
		hit["unknown_form"]++
	}

	// INTEGRITY (§2.14): a root declaring integrity = "crc32c" checks the
	// four-byte CRC-32C trailer before any field.
	led := generateFiles(t, map[string]string{"Ledger.schema": "package led\ntable Ledger | integrity = \"crc32c\"\n{\n    balance int64\n}\n"})
	injectIntegrity(t, led, "Ledger")
	clean := ledgerWire(42)
	sum := binary.LittleEndian.AppendUint32(nil, crc32.Checksum(clean, crc32.MakeTable(crc32.Castagnoli)))
	signed := append(append([]byte(nil), clean...), sum...)
	if got := runStrict(t, led, "Ledger", signed); got != "read" {
		t.Errorf("a file whose trailer matches: got %q, want read", got)
	}
	flipped := append([]byte(nil), signed...)
	flipped[3] ^= 0x01
	cases := map[string][]byte{"a flipped body byte": flipped, "no trailer": clean, "a file of four bytes": signed[:4]}
	for what, w := range cases {
		if got := runStrict(t, led, "Ledger", w); got != "integrity_mismatch -" {
			t.Errorf("integrity, %s: got %q, want %q", what, got, "integrity_mismatch -")
		} else {
			hit["integrity_mismatch"]++
		}
	}

	for _, name := range []string{"malformed", "kind_mismatch", "clamped", "duplicate", "unknown",
		"previous_form", "message_form_as_file", "newer_form", "unknown_form", "integrity_mismatch"} {
		if hit[name] == 0 {
			t.Errorf("no wire refused as %s", name)
		}
	}
	t.Logf("refused by name: %v; %d clean reads; %d rows of units the Luau target refuses", hit, reads, refusedUnits)
}

// injectIntegrity adds the root's `integrity = "crc32c"` to its emitted
// descriptor. The emitter (luautable.go) does not yet carry §2.14's
// declaration in the descriptor, and that file is outside this card's PATHS,
// so the test writes the fact the runtime reads, as injectKeyVs does for a
// keyed array's key variants.
func injectIntegrity(t *testing.T, g *generatedUnit, table string) {
	t.Helper()
	name := ""
	for n, data := range g.files {
		if strings.HasSuffix(n, "Table.luau") && strings.Contains(string(data), fmt.Sprintf("\tname = %q,\n", table)) {
			name = n
		}
	}
	if name == "" {
		t.Fatalf("no table module describes %s", table)
	}
	src := string(g.files[name])
	anchor := fmt.Sprintf("\tname = %q,\n", table)
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the descriptor anchor for %s is not in %s exactly once", table, name)
	}
	src = strings.Replace(src, anchor, anchor+"\tintegrity = \"crc32c\",\n", 1)
	g.files[name] = []byte(src)
	if err := os.WriteFile(filepath.Join(g.dir, name), g.files[name], 0o644); err != nil {
		t.Fatal(err)
	}
}

// ledgerWire is the form-1 file of Ledger{balance} with no trailer: the form
// byte, field reference 1 at kind 5 (int64) and its eight bytes, the
// terminator, and the id table of one id and its count (§3).
func ledgerWire(balance int64) []byte {
	w := []byte{1, 1, 5}
	w = binary.LittleEndian.AppendUint64(w, uint64(balance))
	w = append(w, 0)
	w = binary.LittleEndian.AppendUint64(w, ir.TableWireId("balance"))
	return binary.LittleEndian.AppendUint64(w, 1)
}

// boxCleanWire is the form-1 file of Box{m} with ONE map entry, key 7 and
// value 9: duplicateMapWire's shape with the second entry gone (§3, §2.8).
func boxCleanWire() []byte {
	entry := []byte{2, 6, 7, 3, 6, 9, 0}
	body := append([]byte{13, 1, byte(len(entry))}, entry...)
	w := append([]byte{1, 1, 14, byte(len(body))}, body...)
	w = append(w, 0)
	for _, id := range []uint64{ir.TableWireId("m"), ir.TableWireId("key"), ir.TableWireId("value"), 3} {
		w = binary.LittleEndian.AppendUint64(w, id)
	}
	return w
}
