package goosetable_test

// THE STRICT-READ RULE's gate (test/conformance/README.md, "Every read has
// one outcome"): a report, framing, json-hostile or retain row reads the WHOLE
// value or refuses it. An accepted report, framing or json-hostile read owes
// the verdict line and the complete value in `<case>` AND the canonical
// write-back wire in `<case>.wire`; an accepted retain read owes the verdict
// line and the complete value (its save is the retain-save surface); a
// refused read owes `refused\n` alone. TestGooseDriverVerdictAndWriteBack runs
// the real driver (test/conformance/goose/main.goose) over the report, framing,
// retain and json-hostile surfaces and holds that rule. The negative control
// plants a fault in the driver artifact itself — it drops the `.wire` write-back
// — and requires the gate to go red exactly where the fault is.

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// verdictUnitFiles generates one unit for Goose and the conformance program
// with working flat-value-table JSON arms, so the report, framing, retain and
// json-hostile surfaces can round-trip the value through json_save_table and
// save_table. `override` replaces any generated file (the negative control
// drops the .wire emission from main.goose through it).
func verdictUnitFiles(t *testing.T, key string, paths []string, override map[string][]byte) map[string][]byte {
	t.Helper()
	gathered, err := compiler.GatherPaths(prefix(paths))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatalf("%s does not load: %v", key, err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("%s: goose: %v", key, err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatalf("%s: goosetable: %v", key, err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		mods = append(mods, strings.TrimSuffix(name, ".goose"))
	}
	sort.Strings(mods)
	files["conformance_unit.goose"] = []byte(verdictGlue(goose.Namespace(u), mods))
	main, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "main.goose"))
	if err != nil {
		t.Fatal(err)
	}
	files["main.goose"] = main
	text, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "units", key+".goose"))
	if errors.Is(err, fs.ErrNotExist) {
		text, err = os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "unit.goose"))
	}
	if err != nil {
		t.Fatal(err)
	}
	files["unit.goose"] = text
	maps.Copy(files, override)
	return files
}

// verdictGlue is the conformance glue module: the imports, the registry and the
// JSON arms through the flat value table (the shell driver's flat arms).
func verdictGlue(ns string, mods []string) string {
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	fmt.Fprintf(&b, "fn conformance_unit() -> schema_rt::LoadUnit { return %s::%s; }\n", ns, goosetable.UnitName)
	b.WriteString("fn conformance_wire(root: const u8[:], data: const u8[:], out: u8[>..<]&) -> i64 { return -1; }\n")
	b.WriteString("fn conformance_message(root: const u8[:], announce: const u8[:], wire: const u8[:], out: u8[>..]&) -> i64 { return -1; }\n")
	b.WriteString("fn conformance_json_read(root: const u8[:], text: const u8[:], data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&) -> i64 {\n")
	b.WriteString("    var report = schema_rt::json_report();\n")
	fmt.Fprintf(&b, "    let at = schema_rt::json_load_table(%s::TableUnit, %s::TextUnit, root, text, data, vals, nodes, report);\n", ns, ns)
	b.WriteString("    return if at < 0 || report.damaged { -2 } else { at };\n}\n")
	b.WriteString("fn conformance_json_write(root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], at: i64, out: u8[>..]&) -> i64 {\n")
	b.WriteString("    if at < 0 { return -1; }\n")
	fmt.Fprintf(&b, "    let n = schema_rt::json_save_table(%s::TableUnit, %s::TextUnit, root, data, vals, nodes, at, out);\n", ns, ns)
	b.WriteString("    return if n < 0 { -2 } else { n };\n}\n")
	b.WriteString("fn conformance_json_measure(root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], nodes: const schema_rt::LoadNode[:], at: i64) -> i64 { return -1; }\n")
	b.WriteString("fn conformance_json_verdict(root: const u8[:], text: const u8[:], out: u8[>..]&) {\n")
	b.WriteString("    var data: u8[>..] = [];\n")
	b.WriteString("    var vals: schema_rt::LoadVal[>..] = [];\n")
	b.WriteString("    var nodes: schema_rt::LoadNode[>..] = [];\n")
	b.WriteString("    var r = schema_rt::json_report();\n")
	fmt.Fprintf(&b, "    let at = schema_rt::json_load_table(%s::TableUnit, %s::TextUnit, root, text, data, vals, nodes, r);\n", ns, ns)
	b.WriteString("    if at >= 0 && !r.damaged {\n")
	b.WriteString("        format(out, r.unknown, \",\", r.mismatch, \",\", r.widened, \",\", r.oor, \",\", r.repeated, \",false,read\");\n")
	b.WriteString("        out.push(10);\n")
	fmt.Fprintf(&b, "        if schema_rt::json_save_table(%s::TableUnit, %s::TextUnit, root, data, vals, nodes, at, out) < 0 { out.append(\"!no value\"); }\n", ns, ns)
	b.WriteString("    } else {\n")
	b.WriteString("        out.append(\"refused\");\n")
	b.WriteString("        out.push(10);\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")
	return b.String()
}

// verdictManifest derives the manifest lines one surface's rows of a unit, with
// paths made absolute. A framing line is framing, name, unit, root, and wire:
// the verdict and the value are withheld, so a refusal comes from the load.
func verdictManifest(t *testing.T, surface, unit string) string {
	t.Helper()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	mf, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()
	var b strings.Builder
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || strings.HasPrefix(f[0], "#") || f[0] != surface {
			continue
		}
		switch {
		case f[0] == "report" && len(f) >= 5 && f[2] == unit:
			fmt.Fprintf(&b, "report %s %s %s %s\n", f[1], f[2], f[3], filepath.Join(abs, f[4]))
		case f[0] == "framing" && len(f) >= 7 && f[2] == unit:
			fmt.Fprintf(&b, "framing %s %s %s %s\n", f[1], f[2], f[3], filepath.Join(abs, f[4]))
		case f[0] == "retain" && len(f) >= 7 && f[2] == unit:
			fmt.Fprintf(&b, "retain %s %s %s %s %s %s\n", f[1], f[2], f[3], filepath.Join(abs, f[4]), f[5], f[6])
		case f[0] == "json-hostile" && len(f) >= 6 && f[2] == unit:
			fmt.Fprintf(&b, "json-hostile %s %s %s %s %s\n", f[1], f[2], f[3], filepath.Join(abs, f[4]), f[5])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// verdictRow is one case and the verdict the manifest pins for it.
type verdictRow struct {
	name, verdict string
}

// verdictRows lists the rows of one surface whose unit is `unit` and the
// verdict each owes: reports from reports.txt, framing and json-hostile from
// the manifest's own verdict column, retain always readable.
func verdictRows(t *testing.T, surface, unit string) []verdictRow {
	t.Helper()
	mf, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()
	reports := map[string]string{}
	if surface == "report" {
		rf, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
		if err != nil {
			t.Fatal(err)
		}
		for sc := bufio.NewScanner(rf); sc.Scan(); {
			f := strings.Fields(sc.Text())
			if len(f) == 2 && (f[1] == "read" || f[1] == "refused") {
				reports[f[0]] = f[1]
			}
		}
		rf.Close()
	}
	var out []verdictRow
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || strings.HasPrefix(f[0], "#") || f[0] != surface {
			continue
		}
		switch {
		case f[0] == "report" && len(f) >= 5 && f[2] == unit:
			if v, ok := reports[f[1]]; ok {
				out = append(out, verdictRow{f[1], v})
			}
		case f[0] == "framing" && len(f) >= 7 && f[2] == unit:
			out = append(out, verdictRow{f[1], f[5]})
		case f[0] == "json-hostile" && len(f) >= 6 && f[2] == unit:
			out = append(out, verdictRow{f[1], f[5]})
		case f[0] == "retain" && len(f) >= 7 && f[2] == unit:
			out = append(out, verdictRow{f[1], "read"})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// runVerdictSurface runs the conformance driver's `surface` over a unit and
// returns the per-case answer bytes and the per-case .wire write-back bytes.
func runVerdictSurface(t *testing.T, key, surface string, files map[string][]byte, manifest string) (map[string][]byte, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	files["manifest.txt"] = []byte(manifest)
	out := gooserun.RunArgs(t, dir, "main.goose", files, surface, "manifest.txt", key)
	content := map[string][]byte{}
	wire := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		name, h, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("%s: bad output line %q", surface, line)
		}
		if _, abs := strings.CutPrefix(h, "absent"); abs {
			continue
		}
		b, err := hex.DecodeString(h)
		if err != nil {
			t.Fatalf("%s: %q is not hex: %v", surface, h, err)
		}
		if base, ok := strings.CutSuffix(name, ".wire"); ok {
			wire[base] = b
		} else {
			content[name] = b
		}
	}
	return content, wire
}

func TestGooseDriverVerdictAndWriteBack(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the conformance driver)")
	c := readWireCorpus(t)
	totalReads, totalRefusals := 0, 0
	verify := func(surface, unit string) {
		files := verdictUnitFiles(t, unit, c.units[unit], nil)
		manifest := verdictManifest(t, surface, unit)
		content, wire := runVerdictSurface(t, unit, surface, files, manifest)
		rows := verdictRows(t, surface, unit)
		if len(rows) == 0 {
			t.Fatalf("%s/%s: no rows to run", surface, unit)
		}
		reads, refusals := 0, 0
		for _, r := range rows {
			got, present := content[r.name]
			if !present {
				t.Errorf("%s/%s: missing output row", surface, r.name)
				continue
			}
			if bytes.Equal(got, []byte("refused\n")) {
				refusals++
				if r.verdict == "read" {
					t.Errorf("%s/%s: the manifest pins a read, the driver refused", surface, r.name)
				}
				if _, has := wire[r.name]; has {
					t.Errorf("%s/%s: a refused row owes no .wire write-back", surface, r.name)
				}
				continue
			}
			reads++
			if r.verdict == "refused" {
				t.Errorf("%s/%s: the manifest pins a refusal, the driver read %q", surface, r.name, got)
			}
			if !bytes.HasPrefix(got, []byte("read\n")) {
				t.Errorf("%s/%s: got %q, neither a read nor a refusal", surface, r.name, got)
				continue
			}
			if len(got) <= len("read\n") {
				t.Errorf("%s/%s: the verdict line owes the complete value", surface, r.name)
			}
			if surface == "retain" {
				continue // retain owes the complete value; its save is the retain-save surface
			}
			w, has := wire[r.name]
			if !has {
				t.Errorf("%s/%s: a readable row owes the .wire write-back", surface, r.name)
			} else if len(w) == 0 {
				t.Errorf("%s/%s: the .wire write-back is empty", surface, r.name)
			}
		}
		t.Logf("%s/%s: %d read, %d refused rows hold the strict-read rule", surface, unit, reads, refusals)
		totalReads += reads
		totalRefusals += refusals
	}
	verify("report", "tblp2")
	verify("framing", "graphdemo")
	verify("retain", "tblrt1")
	verify("json-hostile", "graphdemo")
	if totalReads == 0 || totalRefusals == 0 {
		t.Fatalf("the gate needs both accepted and refused rows (read=%d refused=%d)", totalReads, totalRefusals)
	}
}

func TestGooseDriverVerdictAndWriteBackNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the conformance driver)")
	c := readWireCorpus(t)
	unit := "tblp2"
	readRow := ""
	for _, r := range verdictRows(t, "report", unit) {
		if r.verdict == "read" {
			readRow = r.name
			break
		}
	}
	if readRow == "" {
		t.Fatal("tblp2 has no readable report row")
	}
	_, baseline := runVerdictSurface(t, unit, "report", verdictUnitFiles(t, unit, c.units[unit], nil), verdictManifest(t, "report", unit))
	if _, ok := baseline[readRow]; !ok {
		t.Fatalf("baseline: %s owes a .wire write-back", readRow)
	}
	srcBytes, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "main.goose"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcBytes)
	const anchor = "    answer(str(name, \".wire\"), wire);\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("write-back anchor must occur once: %d", strings.Count(src, anchor))
	}
	mutant := strings.Replace(src, anchor, "", 1)
	files := verdictUnitFiles(t, unit, c.units[unit], map[string][]byte{"main.goose": []byte(mutant)})
	_, wire := runVerdictSurface(t, unit, "report", files, verdictManifest(t, "report", unit))
	if _, has := wire[readRow]; has {
		t.Fatal("the sabotage left the write-back wire in place: the control watches nothing")
	}
}
