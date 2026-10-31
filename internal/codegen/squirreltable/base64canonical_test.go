package squirreltable_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// base64ProbeRow is one row of the probe table: a table surface generated for
// a unit, a root to read at, and either the row's wire (load it, then
// SaveText) or a default WeaponConfig constructed from the schema.
type base64ProbeRow struct {
	key    string
	unit   string
	root   string
	module string
	wire   bool
	reason string // the strict read's name for the decoded wire, "ok" when clean
}

type base64TextCase struct {
	label string
	text  string
}

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// TestSquirrelLoadTextRefusesNonCanonicalBase64 drives LoadText over every
// text the rule of docs/SPEC-TABLES.md §16.8 distinguishes, for every instance
// row the Squirrel table surface generates and for tables/examples/Tables.schema's
// WeaponConfig: the canonical SaveText, one text per pad bit the final quantum
// drops, CR/LF/CRLF inserted mid-text and at the end, a misplaced, missing and
// extra '=', a byte outside the alphabet, a truncated quantum and the empty
// text. The expected name is computed in Go by the oracle's own rule — the
// emitted LoadText at schema-go GO TIP d808c3f54 (internal/codegen/gotable/json.go):
// base64.StdEncoding.Strict() first, the plain decode to tell not-canonical
// from damaged, then EncodedLen — and a decoded wire keeps the strict read's
// row in testdata/conformance/tables/reports.txt where one exists.
func TestSquirrelLoadTextRefusesNonCanonicalBase64(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}
	instances := readInstanceRows(t, manifest)

	dir := t.TempDir()
	inputs := map[string][]byte{}
	mods := map[string]string{}
	refused := map[string]bool{}
	var ordered []string
	seen := map[string]bool{}
	for _, in := range instances {
		if !seen[in.unit] {
			ordered = append(ordered, in.unit)
			seen[in.unit] = true
		}
	}
	for _, key := range ordered {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("instance rows name unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			refused[key] = true
			continue
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mods[key] = squirreltable.ModuleName(u)
	}

	// The strict read's name for a wire, where a pinned report row reads the
	// same unit, root and wire (docs/SPEC-TABLES.md §4, §6.5).
	pinned := readReportCounts(t, filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
	wireReason := map[string]string{}
	for _, r := range readReportRows(t, manifest) {
		p, ok := pinned[r.name]
		if !ok {
			t.Fatalf("report row %s has no pinned report", r.name)
		}
		wireReason[r.unit+"|"+r.root+"|"+r.wire] = strictName(t, r.name, p)
	}

	var rows []base64ProbeRow
	for _, in := range instances {
		if refused[in.unit] {
			continue
		}
		if in.goRefusal != "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[in.name] = data
		reason := "ok"
		if r, ok := wireReason[in.unit+"|"+in.root+"|"+in.wire]; ok {
			reason = r
		}
		rows = append(rows, base64ProbeRow{
			key: in.name, unit: in.unit, root: in.root,
			module: mods[in.unit], wire: true, reason: reason,
		})
	}

	// tables/examples/Tables.schema's fixed table WeaponConfig, at its default.
	{
		const key = "weaponconfig"
		u := loadUnit(t, root, []string{"tables/examples/Tables.schema"})
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("WeaponConfig: the type surface refused: %v", err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatalf("WeaponConfig: the table surface refused: %v", err)
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		rows = append(rows, base64ProbeRow{
			key: key, unit: key, root: "WeaponConfig",
			module: squirreltable.ModuleName(u), wire: false, reason: "ok",
		})
	}

	// PASS ONE: each row's canonical SaveText (docs/SPEC-TABLES.md §16.8).
	var save strings.Builder
	save.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
	save.WriteString("local rows = [\n")
	for _, r := range rows {
		fmt.Fprintf(&save, "  { key=%q, unit=%q, mod=%q, root=%q, wire=%v },\n",
			r.key, r.unit, r.module, r.root, r.wire)
	}
	save.WriteString("];\n")
	save.WriteString(`foreach (r in rows) {
  local schema = dofile(r.unit + "/schema_module.nut", true);
  local T = schema.require(r.mod);
  local TX = schema.require("schema_table_text");
  local v = null;
  if (r.wire) {
    local L = schema.require("schema_table_load");
    local rep = { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false };
    v = L.loadTolerant(T, r.root, inputs[r.key], rep);
    if (v == null || rep.refused) { print("error|" + r.key + "|load\n"); continue; }
  } else {
    local C = T[r.root];
    v = C();
  }
  local text = TX.SaveText(T, r.root, v);
  if (text == null) { print("error|" + r.key + "|save\n"); continue; }
  print("canonical|" + r.key + "|" + text + "\n");
}
`)
	writeFile(t, filepath.Join(dir, "save.nut"), []byte(save.String()))
	canonical := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "save.nut", inputs)), "\n"), "\n") {
		f := strings.SplitN(line, "|", 3)
		if len(f) != 3 {
			t.Fatalf("save probe line %q is not kind|key|text", line)
		}
		if f[0] == "error" {
			t.Fatalf("%s: the save probe reported %s", f[1], f[2])
		}
		if f[0] != "canonical" {
			t.Fatalf("save probe line %q is not a canonical text", line)
		}
		canonical[f[1]] = f[2]
	}

	cases := map[string][]base64TextCase{}
	wires := map[string][]byte{}
	want := map[string]string{}
	for _, r := range rows {
		text, ok := canonical[r.key]
		if !ok {
			t.Fatalf("%s: the save probe printed no canonical text", r.key)
		}
		wire, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			t.Fatalf("%s: SaveText returned text that is not base64: %v", r.key, err)
		}
		wires[r.key] = wire
		cases[r.key] = base64ProbeTexts(text)
		for _, c := range cases[r.key] {
			want[r.key+"|"+c.label] = oracleTextReason(c.text, wire, r.reason)
		}
	}

	// PASS TWO: LoadText's reason for every text.
	var load strings.Builder
	load.WriteString("local cases = [\n")
	for _, r := range rows {
		fmt.Fprintf(&load, "  { key=%q, unit=%q, mod=%q, root=%q, texts={\n",
			r.key, r.unit, r.module, r.root)
		for _, c := range cases[r.key] {
			fmt.Fprintf(&load, "    [%q] = %q,\n", c.label, c.text)
		}
		load.WriteString("  } },\n")
	}
	load.WriteString(`];
foreach (c in cases) {
  local schema = dofile(c.unit + "/schema_module.nut", true);
  local T = schema.require(c.mod);
  local TX = schema.require("schema_table_text");
  foreach (label, text in c.texts) {
    local report = { unknown=0, kindMismatch=0, widened=0, clamped=0, duplicate=0, malformed=false, refused=false };
    local v = TX.LoadText(T, c.root, text, report);
    local reason = ("reason" in report && report.reason != null) ? report.reason : "ok";
    print("reason|" + c.key + "|" + label + "|" + reason + "\n");
  }
}
`)
	writeFile(t, filepath.Join(dir, "load.nut"), []byte(load.String()))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(sqrun.Run(t, dir, "load.nut", inputs)), "\n"), "\n") {
		f := strings.SplitN(line, "|", 4)
		if len(f) != 4 || f[0] != "reason" {
			t.Fatalf("load probe line %q is not reason|key|label|name", line)
		}
		got[f[1]+"|"+f[2]] = f[3]
	}

	count := 0
	for _, r := range rows {
		for _, c := range cases[r.key] {
			k := r.key + "|" + c.label
			g, ok := got[k]
			if !ok {
				t.Errorf("%s %s: the load probe printed no reason", r.key, c.label)
				continue
			}
			count++
			if g != want[k] {
				t.Errorf("%s %s (text %q): LoadText reason %q, the oracle's rule says %q",
					r.key, c.label, previewText(c.text), g, want[k])
			}
		}
	}
	t.Logf("compared %d texts over %d rows", count, len(rows))
}

// oracleTextReason is the oracle's own rule for the text carrier's base64
// (docs/SPEC-TABLES.md §16.8; schema-go GO TIP d808c3f54, the emitted LoadText
// in internal/codegen/gotable/json.go): base64.StdEncoding.Strict() first, the
// plain decode to tell not-canonical from damaged, then EncodedLen. A text that
// decodes canonically keeps the strict read's name for the wire.
func oracleTextReason(text string, wire []byte, wireReason string) string {
	strict, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil {
		if _, plain := base64.StdEncoding.DecodeString(text); plain == nil {
			return "base64_not_canonical"
		}
		return "base64_damaged"
	}
	if base64.StdEncoding.EncodedLen(len(strict)) != len(text) {
		return "base64_not_canonical"
	}
	if len(strict) == 0 && len(wire) > 0 {
		// the empty text decodes to a wire with no form byte; ReadStrict names
		// that wire_damaged (docs/SPEC-TABLES.md §6.5).
		return "wire_damaged"
	}
	return wireReason
}

// base64ProbeTexts is the probe table of §16.8's cases for one canonical text.
func base64ProbeTexts(canonical string) []base64TextCase {
	out := []base64TextCase{{"canonical", canonical}}

	// One flip per pad bit the final quantum drops: four for '==', two for '='.
	var idx, bits int
	switch {
	case strings.HasSuffix(canonical, "=="):
		idx, bits = len(canonical)-3, 4
	case strings.HasSuffix(canonical, "="):
		idx, bits = len(canonical)-2, 2
	}
	if idx >= 0 && idx < len(canonical) {
		if v := strings.IndexByte(base64Alphabet, canonical[idx]); v >= 0 {
			for bit := 0; bit < bits; bit++ {
				b := []byte(canonical)
				b[idx] = base64Alphabet[v^(1<<bit)]
				out = append(out, base64TextCase{fmt.Sprintf("padbit%d", bit), string(b)})
			}
		}
	}

	mid := len(canonical) / 2
	for _, nl := range []struct{ label, text string }{
		{"cr", "\r"}, {"lf", "\n"}, {"crlf", "\r\n"},
	} {
		out = append(out,
			base64TextCase{"mid_" + nl.label, canonical[:mid] + nl.text + canonical[mid:]},
			base64TextCase{"end_" + nl.label, canonical + nl.text})
	}

	out = append(out, base64TextCase{"misplaced_pad", canonical[:mid] + "=" + canonical[mid:]})
	if strings.HasSuffix(canonical, "=") {
		out = append(out, base64TextCase{"missing_pad", canonical[:len(canonical)-1]})
	}
	out = append(out, base64TextCase{"extra_pad", canonical + "="})
	if canonical == "" {
		out = append(out, base64TextCase{"outside_alphabet", "!"})
	} else {
		out = append(out, base64TextCase{"outside_alphabet", "!" + canonical[1:]})
	}
	if canonical != "" {
		out = append(out, base64TextCase{"truncated_quantum", canonical[:len(canonical)-1]})
	}
	out = append(out, base64TextCase{"empty", ""})
	return out
}

// previewText keeps a failure line readable when the canonical text is a large
// blob's base64.
func previewText(s string) string {
	if len(s) <= 48 {
		return s
	}
	return s[:48] + "..."
}
