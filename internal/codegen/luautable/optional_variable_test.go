package luautable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// optionalVariableClosureExists holds docs/SPEC-TABLES.md §2.3's rule for
// `?T` over a variable closure in the Luau backend, under the pinned
// interpreter in a t.TempDir(): a walk that follows pointer edges visits T's
// edges only when the presence companion is set, and an absent value owns no
// node.
//
// The saved bytes are computed by the emitted save.luau walk itself: an
// absent `inner` saves the absent wire (no node rides), loading it back and
// re-saving reproduces the same bytes (a wire round-trip), and an absent
// value that still holds a pointer edge saves the same absent wire — a
// present value then cleared writes no stale node.
func TestLuauOptionalVariableClosureTable(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{
		"Probe.schema": "package probe\n\ntable Inner\n{\n    text *string\n    n int32\n}\n\ntable Outer\n{\n    inner ?Inner\n    tail int32\n}\n",
	})
	main := "local save = require(\"./save\")\nlocal load = require(\"./load\")\nlocal St = require(\"./schema_tables\")\n"
	main += jsonModules(g)
	main += "local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }\n"
	main += "local d = St.lookup(\"Outer\")\n"
	main += "-- an absent inner: no node rides (§2.3)\n"
	main += "local absent = d.new()\n"
	main += "absent.tail = 5\n"
	main += "local w1 = save.save(d, absent)\n"
	main += "local m1 = save.measure(d, absent)\n"
	main += "if w1 == nil or m1 == nil then print(\"SAVENIL\"); return end\n"
	main += "-- an absent value that still holds a pointer edge saves the same absent wire\n"
	main += "local stale = d.new()\n"
	main += "stale.tail = 5\n"
	main += "stale.inner = { text = buffer.fromstring(\"x\") }\n"
	main += "local w2 = save.save(d, stale)\n"
	main += "local m2 = save.measure(d, stale)\n"
	main += "if w2 == nil or m2 == nil then print(\"SAVENIL\"); return end\n"
	main += "-- load the absent wire back and re-save: same bytes\n"
	main += "local r = load.load(\"Outer\", w1, report)\n"
	main += "if r == nil or report.refused > 0 then print(\"REFUSED\"); return end\n"
	main += "local w3 = save.save(d, r)\n"
	main += "if w3 == nil then print(\"SAVENIL\"); return end\n"
	main += "local hex = function(w)\n"
	main += "\tlocal p = table.create(buffer.len(w))\n"
	main += "\tfor i = 0, buffer.len(w) - 1 do\n"
	main += "\t\tp[i + 1] = string.format(\"%02x\", buffer.readu8(w, i))\n"
	main += "\tend\n"
	main += "\treturn table.concat(p)\n"
	main += "end\n"
	main += "print(\"x\" .. hex(w1))\n"
	main += "print(\"x\" .. hex(w2))\n"
	main += "print(\"x\" .. hex(w3))\n"
	main += "print(m1)\n"
	main += "print(m2)\n"
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", nil)))
	if out == "SAVENIL" || out == "REFUSED" {
		t.Fatalf("the saved ran refused: %s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("unexpected output %q", out)
	}
	absentH, staleH, rtH := lines[0], lines[1], lines[2]
	m1, err := strconv.Atoi(lines[3])
	if err != nil {
		t.Fatalf("bad measure line %q", lines[3])
	}
	m2, err := strconv.Atoi(lines[4])
	if err != nil {
		t.Fatalf("bad measure line %q", lines[4])
	}
	absent, err := hex.DecodeString(strings.TrimPrefix(absentH, "x"))
	if err != nil {
		t.Fatalf("bad hex %q", absentH)
	}
	if strings.TrimPrefix(staleH, "x") != strings.TrimPrefix(absentH, "x") {
		t.Errorf("an absent value that still holds a pointer saved a different wire: %s vs %s", staleH, absentH)
	}
	if m2 != m1 {
		t.Errorf("an absent value that still holds a pointer measured differently: %d vs %d", m2, m1)
	}
	if strings.TrimPrefix(rtH, "x") != strings.TrimPrefix(absentH, "x") {
		t.Errorf("absent wire did not round-trip: %s vs %s", rtH, absentH)
	}
	if m1 != len(absent) {
		t.Errorf("measure %d != saved bytes %d", m1, len(absent))
	}
}
