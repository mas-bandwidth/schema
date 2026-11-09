package luautable_test

// THE MESSAGE-FORM AND RETAIN-UNKNOWN CELLS (docs/roadmap.sexp rows
// message-form/luau and retain-unknown/luau): LoadMessages and LoadRetain
// are the strict reads, the plain names that hold the whole-value-or-refusal
// rule (docs/SPEC-TABLES.md §6.5). LoadMessages refuses a damaged batch whole
// (count -1, no bodies, malformed); LoadRetain refuses a damaged wire whole
// (nil value, no records). Each is held by a positive read and by a negative
// control that sabotages the emitted runtime so the refusal stops holding.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// moduleList returns the unit's <Base>Table.luau module names, in sorted
// order, the require list the retain probes write.
func moduleList(g *generatedUnit) []string {
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	return mods
}

// retainStrictProbe loads Root's wire with LoadRetain and prints three facts:
// whether the value is nil, whether the report is malformed, and how many
// records were retained. The capacity is the store ceiling the caller owns.
func retainStrictProbe(g *generatedUnit, capacity int) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal retain = require(\"./schema_retain\")\n")
	for _, m := range moduleList(g) {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	fmt.Fprintf(&b, `local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
local v, store = retain.LoadRetain("Root", inputs.wire, report, %d)
print(tostring(v == nil) .. " " .. tostring(report.malformed) .. " " .. tostring(report.retained))
`, capacity)
	return b.String()
}

// retainKnownWire builds a wire of the NEW unit that carries unknowns for the
// OLD reader: child.extra, the note arm, a NewKind pointer target and the
// future root field. The old reader keeps them and loads clean.
func retainKnownWire(t *testing.T) []byte {
	t.Helper()
	newUnit, _ := retainCompile(t, retainNewSchema)
	return retainWire(t, newUnit, `{"child":{"n":7,"extra":"kept"},"choice":{"note":"arm kept"},"ptr":{"m":9},"future":"root kept"}`)
}

// damageIdTableFraming overwrites the file's id-table count (the last eight
// bytes) so the trailer can no longer be framed: the count reads as -1, which
// every open refuses.
func damageIdTableFraming(wire []byte) []byte {
	bad := make([]byte, len(wire))
	copy(bad, wire)
	for i := len(bad) - 8; i < len(bad); i++ {
		bad[i] = 0xFF
	}
	return bad
}

func TestLuauVtMessageRetainCells(t *testing.T) {
	slowtest.Gate(t, "Luau (it runs the emitted readers)")
	t.Parallel()

	t.Run("LoadMessages holds the strict read over the message form", func(t *testing.T) {
		units, conns, rows := messageManifest(t)
		var row messageRow
		for _, r := range rows {
			if r.name == "login_full" {
				row = r
				break
			}
		}
		if row.name == "" {
			t.Fatal("the manifest names no login_full message row")
		}
		conn := conns[row.connection]
		ann, err := os.ReadFile(filepath.Join(root, conn.announcement))
		if err != nil {
			t.Fatal(err)
		}
		good, err := os.ReadFile(filepath.Join(root, row.batch))
		if err != nil {
			t.Fatal(err)
		}
		damaged, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/backend_batch_damaged_second_message.bin"))
		if err != nil {
			t.Fatal(err)
		}
		dir, mods, _ := buildMessageUnit(t, units[conn.unit])

		out := messageMainWith(t, dir, mods, row.root, "LoadMessages", ann, good)
		if !strings.HasPrefix(out, "count 1 malformed false") {
			t.Fatalf("LoadMessages refused a clean batch: %s", out)
		}
		out = messageMainWith(t, dir, mods, row.root, "LoadMessages", ann, damaged)
		if !strings.HasPrefix(out, "count -1 malformed true") {
			t.Fatalf("LoadMessages handed back bodies of a damaged batch: %s", out)
		}
	})

	t.Run("LoadRetain holds the strict read over the variable form", func(t *testing.T) {
		wire := retainKnownWire(t)
		g := generateFiles(t, map[string]string{"Probe.schema": retainOldSchema})
		dir := g.dir
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(retainStrictProbe(g, 1<<20)), 0o644); err != nil {
			t.Fatal(err)
		}
		run := func(w []byte) string {
			return strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": w})))
		}
		fields := strings.Fields(run(wire))
		if len(fields) != 3 || fields[0] != "false" || fields[1] != "false" || fields[2] == "0" {
			t.Fatalf("LoadRetain did not hold a clean wire: %s", strings.Join(fields, " "))
		}
		damaged := run(damageIdTableFraming(wire))
		if damaged != "true true 0" {
			t.Fatalf("LoadRetain held a value of a damaged wire: %s", damaged)
		}
	})
}

func TestLuauVtMessageRetainCellsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "Luau (it runs the emitted readers)")
	t.Parallel()

	t.Run("message form: the sabotage exposes partial bodies", func(t *testing.T) {
		units, conns, rows := messageManifest(t)
		var row messageRow
		for _, r := range rows {
			if r.name == "login_full" {
				row = r
				break
			}
		}
		conn := conns[row.connection]
		ann, err := os.ReadFile(filepath.Join(root, conn.announcement))
		if err != nil {
			t.Fatal(err)
		}
		damaged, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/backend_batch_damaged_second_message.bin"))
		if err != nil {
			t.Fatal(err)
		}
		dir, mods, files := buildMessageUnit(t, units[conn.unit])

		out := messageMainWith(t, dir, mods, row.root, "LoadMessages", ann, damaged)
		if !strings.HasPrefix(out, "count -1 malformed true") {
			t.Fatalf("control needs a clean whole-batch refusal: %s", out)
		}
		src := string(files["schema_message.luau"])
		const anchor = "if rep.malformed then rep.reason = \"wire_damaged\"; return -1, {} end"
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the message strict-refusal anchor appears %d times, want exactly once", strings.Count(src, anchor))
		}
		patched := strings.Replace(src, anchor, "if rep.malformed then rep.reason = \"wire_damaged\"; return count, bodies end", 1)
		if err := os.WriteFile(filepath.Join(dir, "schema_message.luau"), []byte(patched), 0o644); err != nil {
			t.Fatal(err)
		}
		out = messageMainWith(t, dir, mods, row.root, "LoadMessages", ann, damaged)
		if strings.HasPrefix(out, "count -1 malformed true") {
			t.Fatalf("the sabotage did not expose partial bodies: %s", out)
		}
	})

	t.Run("retain-unknown: the sabotage exposes a value on damage", func(t *testing.T) {
		wire := retainKnownWire(t)
		bad := damageIdTableFraming(wire)
		g := generateFiles(t, map[string]string{"Probe.schema": retainOldSchema})
		dir := g.dir
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(retainStrictProbe(g, 1<<20)), 0o644); err != nil {
			t.Fatal(err)
		}
		run := func(w []byte) string {
			return strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": w})))
		}
		if got := run(bad); got != "true true 0" {
			t.Fatalf("control needs a clean refusal: %s", got)
		}
		src := string(g.files["schema_retain.luau"])
		const anchor = "if v == nil then\n\t\treturn v, new_store()\n\tend"
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the retain strict-refusal anchor appears %d times, want exactly once", strings.Count(src, anchor))
		}
		patched := strings.Replace(src, anchor, "if v == nil then\n\t\treturn {}, new_store()\n\tend", 1)
		if err := os.WriteFile(filepath.Join(dir, "schema_retain.luau"), []byte(patched), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := run(bad); got == "true true 0" {
			t.Fatalf("runtime sabotage did not expose a value on damage: %s", got)
		}
	})
}
