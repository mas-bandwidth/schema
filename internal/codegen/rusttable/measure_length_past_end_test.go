package rusttable

// A LENGTH THAT RUNS PAST THE BODY CARRYING IT IS DAMAGE
// (docs/SPEC-TABLES.md §3, §3.1): the node-table field's L and an unbounded
// list's L are read from the wire, and a LEB that runs past the body end into
// the id table is damage, never a length borrowed from the trailer. Both the
// strict read (<root>_load) and the measure (<root>_load_measure) refuse it,
// in release and in a checked debug build, and the Go engine refuses the same
// bytes.
//
// The files are built in Go from the field and reserved ids the wire names,
// with the LEB left unterminated at the body end so its next byte is the id
// table's. A well-formed control loads in both profiles.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// lengthPastBodyEnd builds a form-1 file whose root body holds one field of
// the given kind and id, then a LEB length whose first byte says "more", so
// the next byte read is the id table entry. The body ends before that byte,
// exactly where a reader must stop the LEB.
func lengthPastBodyEnd(kind byte, id uint64) []byte {
	wire := []byte{ir.TableWireForm, 1, kind, 0x80}
	wire = binary.LittleEndian.AppendUint64(wire, id)
	wire = binary.LittleEndian.AppendUint64(wire, 1)
	return wire
}

// measurePastEndControl encodes a well-formed value through the Go model and
// the engine's own writer, so the control's bytes and its verdict are the
// engine's.
func measurePastEndControl(t *testing.T, u *ir.Unit, root, doc string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	v := m.New(u.Tables[root])
	var report tabletext.Report
	if !m.Read(v, []byte(doc), &report) {
		t.Fatalf("the well-formed control %s did not read: %+v", doc, report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatalf("the well-formed control %s did not encode: %v", doc, err)
	}
	bytes, whole, refused := tablewire.FileRegionMeasure(u, u.Tables[root], wire)
	if !whole || refused || bytes <= 0 {
		t.Fatalf("the engine refused its own well-formed control: bytes=%d whole=%t refused=%t", bytes, whole, refused)
	}
	return wire
}

// measurePastEndEngineRefuses is the oracle's verdict on the hostile bytes:
// the engine's strict read refuses the whole file.
func measurePastEndEngineRefuses(t *testing.T, u *ir.Unit, root, name string, wire []byte) {
	t.Helper()
	m := tabletext.NewModel(u)
	var report tabletext.Report
	ok, err := tablewire.DecodeRegion(m, m.New(u.Tables[root]), wire, &report)
	if ok && err == nil {
		t.Fatalf("%s: the engine's strict read accepted the file", name)
	}
}

// runMeasurePastEndProbe generates the unit's Rust modules, then builds and
// runs one probe per profile: optimized (overflow checks off) and a checked
// debug build. Each case prints its two verdicts, with a caught panic named
// rather than ending the probe, so a panic is a failed assertion and not a
// lost case.
func runMeasurePastEndProbe(t *testing.T, u *ir.Unit, prefix string, cases map[string][]byte, control []byte) {
	t.Helper()
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "probe", modules)

	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\nmod probe;\nuse probe::*;\n")
	main.WriteString("fn caught<F: FnOnce() -> bool>(f: F) -> String {\n")
	main.WriteString("    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(f)) {\n")
	main.WriteString("        Ok(v) => v.to_string(),\n")
	main.WriteString("        Err(_) => String::from(\"panic\"),\n")
	main.WriteString("    }\n}\n")
	main.WriteString("fn main() {\n")
	main.WriteString("    std::panic::set_hook(Box::new(|_| {}));\n")
	fmt.Fprintf(&main, "    let control = vec!%s;\n", rustByteVector(control))
	fmt.Fprintf(&main, "    let cl = caught(|| { let mut r = TableReport::default(); %s_load(&control, &mut r).is_some() });\n", prefix)
	fmt.Fprintf(&main, "    let cm = caught(|| %s_load_measure(&control) > 0);\n", prefix)
	main.WriteString("    println!(\"CONTROL load={} measure={}\", cl, cm);\n")
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	main.WriteString("    let cases: [(&str, Vec<u8>); ")
	fmt.Fprintf(&main, "%d] = [\n", len(names))
	for _, name := range names {
		fmt.Fprintf(&main, "        (%q, vec!%s),\n", name, rustByteVector(cases[name]))
	}
	main.WriteString("    ];\n")
	main.WriteString("    for (id, wire) in cases.iter() {\n")
	fmt.Fprintf(&main, "        let l = caught(|| { let mut r = TableReport::default(); %s_load(wire, &mut r).is_none() });\n", prefix)
	fmt.Fprintf(&main, "        let m = caught(|| %s_load_measure(wire) < 0);\n", prefix)
	main.WriteString("        println!(\"CASE {} load={} measure={}\", id, l, m);\n")
	main.WriteString("    }\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, profile := range []struct {
		name string
		args []string
	}{
		{"release", []string{"-O"}},
		{"debug", []string{"-C", "overflow-checks=on"}},
	} {
		bin := filepath.Join(dir, "run-"+profile.name)
		args := append([]string{"--edition", "2024"}, profile.args...)
		args = append(args, "-A", "warnings", "-o", bin, "main.rs")
		build := exec.Command("rustc", args...)
		build.Dir = dir
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("%s: the generated readers did not build: %v\n%s", profile.name, err, out)
		}
		run := exec.Command(bin)
		run.Dir = dir
		out, err := run.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: the probe did not run: %v\n%s", profile.name, err, out)
		}
		seen := map[string]bool{}
		seenControl := false
		for line := range strings.SplitSeq(string(out), "\n") {
			if line == "CONTROL load=true measure=true" {
				seenControl = true
				continue
			}
			rest, ok := strings.CutPrefix(line, "CASE ")
			if !ok {
				continue
			}
			fields := strings.Fields(rest)
			if len(fields) != 3 || fields[1] != "load=true" || fields[2] != "measure=true" {
				t.Errorf("%s: %s", profile.name, line)
				continue
			}
			seen[fields[0]] = true
		}
		if !seenControl {
			t.Errorf("%s: the well-formed control did not load and measure", profile.name)
		}
		for _, name := range names {
			if !seen[name] {
				t.Errorf("%s: %s was not refused by both reads", profile.name, name)
			}
		}
	}
}

func TestRustMeasureLengthPastBodyEnd(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated table readers in release and checked debug)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: %v", err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	lists := generateUnit(t, repo, []string{"tables/lists"})
	stream := generateUnit(t, repo, []string{"tables/stream"})

	saveCases := map[string][]byte{
		"save_node_table": lengthPastBodyEnd(12, ir.TableNodeWireId),
		"save_list":       lengthPastBodyEnd(14, ir.TableWireId("scores")),
	}
	feedCases := map[string][]byte{
		"feed_node_table": lengthPastBodyEnd(12, ir.TableNodeWireId),
	}
	saveControl := measurePastEndControl(t, lists, "Save", `{"scores":[1,2,3]}`)
	feedControl := measurePastEndControl(t, stream, "Feed", `{"id":7}`)

	for name, wire := range saveCases {
		measurePastEndEngineRefuses(t, lists, "Save", name, wire)
	}
	for name, wire := range feedCases {
		measurePastEndEngineRefuses(t, stream, "Feed", name, wire)
	}

	runMeasurePastEndProbe(t, lists, "save", saveCases, saveControl)
	runMeasurePastEndProbe(t, stream, "feed", feedCases, feedControl)
}
