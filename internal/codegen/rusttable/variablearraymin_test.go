package rusttable

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// variableMinSrc carries a declared minimum on every element class the
// variable wire rides a counted array of: a scalar, a table, an enum, and an
// optional ?[Min..N]T (docs/SPEC-TABLES.md §2.3, §2.9, §3; a text list takes
// no minimum, §2.13).
const variableMinSrc = `package vmin

enum Mood { Calm, Loud }

table Child
{
    n int32 = 55
}

table Root
{
    items [2..3]int32
    rows  [1..2]Child
    moods [1..3]Mood
    maybe ?[2..3]int32
}
`

// TestRustVariableCountedArrayMinimum holds the Rust variable class to the Go
// oracle on a counted array with a declared minimum, [Min..N]T
// (docs/SPEC-TABLES.md §3, §4, §16.2): a count under Min is refused on the
// wire and in JSON, a count at Min, between and at N is read and saved to the
// oracle's bytes and text, a field absent on the wire or in JSON is born at Min
// with every element at its default, and a value at its birth elides on save
// exactly as internal/tablewire's encodeArray elides it. Every expected answer
// is the oracle's (internal/tablewire Decode/Encode, internal/tabletext
// Read/Write) at run time; the under-Min wires are written by the oracle
// through the same unit spelled [..N], which is the same bytes (§2.9).
//
// The DEBUG build is held to the write side (Rust write checks are debug
// asserts only): saving a value under Min panics there by name.
//
// A FUNCTIONAL test: it compiles Rust, skips by name without rustc, and fails
// by name when SCHEMA_REQUIRE_CORPUS is set.
func TestRustVariableCountedArrayMinimum(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated [Min..N]T probe)")
	u := unitFrom(t, variableMinSrc)
	m := tabletext.NewModel(u)
	loose := strings.NewReplacer("[2..3]", "[..3]", "[1..2]", "[..2]", "[1..3]", "[..3]").Replace(variableMinSrc)
	lm := tabletext.NewModel(unitFrom(t, loose))

	type mcase struct {
		name, mode, input string // mode: wire (hex) or json
		accept            bool
		wantWire          []byte
		wantText          string
	}
	var cases []mcase

	// the oracle's answer for a wire: refuse, or the bytes and text it saves
	wireCase := func(name string, wire []byte) {
		inst := m.New(m.Lookup("Root"))
		var rep tabletext.Report
		ok, _ := tablewire.Decode(m, inst, wire, &rep)
		c := mcase{name: name, mode: "wire", input: hex.EncodeToString(wire), accept: ok}
		if ok {
			var err error
			if c.wantWire, err = tablewire.Encode(m, inst); err != nil {
				t.Fatal(err)
			}
			text, err := m.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			c.wantText = string(text)
		}
		cases = append(cases, c)
	}
	jsonCase := func(name, text string) {
		inst := m.New(m.Lookup("Root"))
		var rep tabletext.Report
		ok := m.Read(inst, []byte(text), &rep)
		c := mcase{name: name, mode: "json", input: text, accept: ok}
		if ok {
			var err error
			if c.wantWire, err = tablewire.Encode(m, inst); err != nil {
				t.Fatal(err)
			}
			out, err := m.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			c.wantText = string(out)
		}
		cases = append(cases, c)
	}
	encodeLoose := func(text string) []byte {
		t.Helper()
		inst := lm.New(lm.Lookup("Root"))
		var rep tabletext.Report
		if !lm.Read(inst, []byte(text), &rep) {
			t.Fatalf("the [..N] control did not read %s: %+v", text, rep)
		}
		wire, err := tablewire.Encode(lm, inst)
		if err != nil {
			t.Fatal(err)
		}
		return wire
	}

	accepted := map[string]string{
		"at_min":       `{"items":[1,2],"rows":[{"n":1}],"moods":["Loud"],"maybe":[3,4]}`,
		"at_max":       `{"items":[1,2,3],"rows":[{"n":1},{"n":2}],"moods":["Loud","Calm","Loud"],"maybe":[4,5,6]}`,
		"empty":        `{}`,
		"birth_values": `{"items":[0,0],"rows":[{"n":55}],"moods":["Calm"]}`,
		"birth_live":   `{"items":[0,7],"rows":[{"n":56}],"moods":["Loud"]}`,
		"maybe_birth":  `{"maybe":[0,0]}`,
	}
	under := map[string]string{
		"items_under": `{"items":[5]}`,
		"maybe_under": `{"maybe":[5]}`,
		"rows_zero":   `{"rows":[]}`,
		"moods_zero":  `{"moods":[]}`,
	}
	for _, name := range []string{"at_min", "at_max", "empty", "birth_values", "birth_live", "maybe_birth"} {
		text := accepted[name]
		jsonCase("json_"+name, text)
		inst := m.New(m.Lookup("Root"))
		var rep tabletext.Report
		if !m.Read(inst, []byte(text), &rep) {
			t.Fatalf("%s: the oracle did not read its own text: %+v", name, rep)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		wireCase("wire_"+name, wire)
	}
	for _, name := range []string{"items_under", "maybe_under", "rows_zero", "moods_zero"} {
		jsonCase("json_"+name, under[name])
	}
	// a count under Min on the wire: items 1 < 2, maybe 1 < 2 (a zero count
	// elides in the [..N] spelling, so it is not a wire under Min)
	for _, name := range []string{"items_under", "maybe_under"} {
		wireCase("wire_"+name, encodeLoose(under[name]))
	}
	// the oracle's answers are what the rule says, or the test proves nothing
	for _, c := range cases {
		want := !strings.Contains(c.name, "under") && !strings.Contains(c.name, "_zero")
		if c.accept != want {
			t.Fatalf("%s: the oracle accept=%v, the rule says %v", c.name, c.accept, want)
		}
	}
	if len(cases) != 18 {
		t.Fatalf("%d cases, want 18", len(cases))
	}

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the [Min..N]T probe cannot be built: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("rust refused the [Min..N]T unit: %v", err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "tblvmin", modules)
	const main = `mod tblvmin;
use std::io::BufRead;
fn hex(b: &[u8]) -> String { b.iter().map(|x| format!("{x:02x}")).collect() }
fn unhex(s: &str) -> Vec<u8> { (0..s.len()).step_by(2).map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap()).collect() }
fn main() {
    if std::env::args().nth(1).as_deref() == Some("under") {
        // the write side: a value under Min (debug build: the assert fires)
        let mut v = tblvmin::Root::default();
        v.items = vec![9];
        let wire = tblvmin::root_save(&v);
        println!("saved {}", hex(&wire));
        return;
    }
    let fresh = tblvmin::Root::default();
    println!("fresh {} {}", hex(&tblvmin::root_save(&fresh)), tblvmin::root_to_json(&fresh).replace('\n', " "));
    for line in std::io::stdin().lock().lines() {
        let line = line.unwrap();
        let (mode, input) = line.split_once(' ').unwrap();
        let mut report = tblvmin::TableReport::default();
        let v = if mode == "wire" { tblvmin::root_load(&unhex(input), &mut report) } else { tblvmin::root_from_json(input, &mut report) };
        match v {
            Some(v) if !report.damaged && !report.refused => {
                let wire = tblvmin::root_save_checked(&v).expect("save");
                let text = tblvmin::root_to_json_checked(&v).expect("to_json");
                println!("accept {} {}", hex(&wire), text.replace('\n', " "));
            }
            _ => println!("refuse"),
        }
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := rustcBuild(t, dir)

	var stdin strings.Builder
	for _, c := range cases {
		stdin.WriteString(c.mode + " " + c.input + "\n")
	}
	cmd := testproc.Command(t, bin)
	cmd.Stdin = strings.NewReader(stdin.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the Rust probe failed: %v\n%s", err, stderr.String())
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(cases)+1 {
		t.Fatalf("the Rust probe answered %d lines, want %d:\n%s", len(lines), len(cases)+1, out)
	}
	oneLine := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	// a fresh Root is born at every minimum and saves the oracle's fresh bytes
	freshInst := m.New(m.Lookup("Root"))
	freshWire, err := tablewire.Encode(m, freshInst)
	if err != nil {
		t.Fatal(err)
	}
	freshText, err := m.Write(freshInst)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.SplitN(lines[0], " ", 3)
	if len(fields) != 3 || fields[0] != "fresh" || fields[1] != hex.EncodeToString(freshWire) || oneLine(fields[2]) != oneLine(string(freshText)) {
		t.Errorf("fresh: the Rust probe says %q, the oracle saves %x and writes %s", lines[0], freshWire, oneLine(string(freshText)))
	}
	for i, c := range cases {
		got := lines[i+1]
		if !c.accept {
			if got != "refuse" {
				t.Errorf("%s: the oracle refuses, the Rust probe says %q", c.name, got)
			}
			continue
		}
		parts := strings.SplitN(got, " ", 3)
		if len(parts) != 3 || parts[0] != "accept" {
			t.Errorf("%s: the oracle accepts, the Rust probe says %q", c.name, got)
			continue
		}
		gotWire, err := hex.DecodeString(parts[1])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotWire, c.wantWire) {
			t.Errorf("%s: the Rust save is %x, the oracle's %x (first difference at byte %d)", c.name, gotWire, c.wantWire, firstDiff(gotWire, c.wantWire))
		}
		if oneLine(parts[2]) != oneLine(c.wantText) {
			t.Errorf("%s: the Rust text differs from the oracle's:\n%s\nwant:\n%s", c.name, parts[2], c.wantText)
		}
	}

	// the write side in a DEBUG build: under Min is a debug assert
	debugBin := filepath.Join(dir, "driver-debug")
	build := testproc.Command(t, "rustc", "--edition", "2024", "-C", "debug-assertions=on", "-A", "warnings", "-o", debugBin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the generated Rust did not build in debug: %v\n%s", err, out)
	}
	run := testproc.Command(t, debugBin, "under")
	var debugErr bytes.Buffer
	run.Stderr = &debugErr
	if out, err := run.Output(); err == nil || !strings.Contains(debugErr.String(), "items count under 2") {
		t.Errorf("a debug save under Min did not assert by name: err=%v stdout=%s stderr=%s", err, out, debugErr.String())
	}
}
