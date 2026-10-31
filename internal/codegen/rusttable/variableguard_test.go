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
)

// TestGuardCondsComposeTheBranchTreeRust pins how a guarded field's Rust
// condition is spelled (docs/SPEC-TABLES.md §4): the conjunction of every
// enclosing branch, negated on an `else` and on `if !x`, over the body's own
// bool fields — the composition tabletext.Guards reads off the `if` tree,
// which is the oracle's, and this test holds the two equal. A pure test: no
// rustc.
func TestGuardCondsComposeTheBranchTreeRust(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package probe\ntable T\n{\n    a bool\n    b bool\n    if a\n    {\n        if !b\n        {\n            x int32\n        }\n        y int32\n    }\n    else\n    {\n        z int32\n    }\n    w int32\n}\n")
	got := guardConds(u.Tables["T"], "v")
	want := map[string]string{"x": "v.a && !v.b", "y": "v.a", "z": "!v.a"}
	if len(got) != len(want) {
		t.Errorf("guardConds = %v, want %v (w and the guards themselves ride always)", got, want)
	}
	for name, cond := range want {
		if got[name] != cond {
			t.Errorf("%s: condition %q, want %q", name, got[name], cond)
		}
	}
	// and the oracle reads the same tree the same way
	for name, terms := range tabletext.Guards(u.Tables["T"]) {
		parts := make([]string, len(terms))
		for i, term := range terms {
			parts[i] = "v." + term.Field
			if !term.Want {
				parts[i] = "!" + parts[i]
			}
		}
		if oracle := strings.Join(parts, " && "); got[name] != oracle {
			t.Errorf("%s: condition %q, the oracle's %q", name, got[name], oracle)
		}
	}
}

// TestGuardAndOptionalEdgesRust holds test/tables/G1.schema's rule on the
// Rust variable wire against the Go oracle (docs/SPEC-TABLES.md §3.1, §2.3, §4):
// a pointer under a false guard, or inside an absent optional, is not an edge.
// Each case is a JSON text the oracle reads (the reader places every key, a
// guarded one included), and the Rust leg must read it, write the oracle's
// text back (internal/tabletext Write: the false-guarded key is gone) and save
// the oracle's bytes (internal/tablewire Encode: no field, no record).
//
// The four corpus wires seed it, and one more case is the reversed witness:
// guard_true_both_edges read back with `at_rest` false, so `resting` is SET
// under a false guard. With the guard gate taken out of the numbering, the
// save and the text writer (make tables-rust-guard-negative-control) its save
// goes red, because only it holds a pointer the gate has to drop, and every
// false-guard case's text goes red on a `resting` key the oracle never writes;
// guard_true_both_edges, whose guard holds, stays green.
//
// A FUNCTIONAL test: it compiles Rust, skips by name without rustc, and fails
// by name when SCHEMA_REQUIRE_CORPUS is set.
func TestGuardAndOptionalEdgesRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated G1 probe)")
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := generateUnit(t, repo, []string{"test/tables/G1.schema"})
	m := tabletext.NewModel(u)

	type gcase struct{ name, text string }
	var cases []gcase
	for _, name := range []string{"guard_false_no_edge", "guard_true_both_edges", "optional_absent_no_edge", "optional_present_edge"} {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup("Guarded"))
		var rep tabletext.Report
		if ok, err := tablewire.DecodeRegion(m, inst, wire, &rep); !ok || err != nil {
			t.Fatalf("%s: the oracle did not read the corpus wire: %v", name, err)
		}
		text, err := m.Write(inst)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, gcase{name, string(text)})
		if name == "guard_true_both_edges" {
			flipped := strings.Replace(string(text), `"at_rest": true`, `"at_rest": false`, 1)
			if flipped == string(text) || !strings.Contains(flipped, `"resting"`) {
				t.Fatalf("the reversed witness was not planted: %s", text)
			}
			cases = append(cases, gcase{"guard_false_resting_set", flipped})
		}
	}

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the G1 probe cannot be built: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("rust refused G1: %v", err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "tblg1", modules)
	const main = `mod tblg1;
use std::io::Read;
fn main() {
    let mut text = String::new();
    std::io::stdin().read_to_string(&mut text).unwrap();
    let mut report = tblg1::TableReport::default();
    let v = tblg1::guarded_from_json(&text, &mut report).expect("from_json");
    let out = tblg1::guarded_to_json_checked(&v).expect("to_json");
    let wire = tblg1::guarded_save_checked(&v).expect("save");
    let hex: String = wire.iter().map(|b| format!("{b:02x}")).collect();
    print!("{hex}\n{out}");
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := rustcBuild(t, dir)

	for _, c := range cases {
		inst := m.New(m.Lookup("Guarded"))
		var rep tabletext.Report
		if !m.Read(inst, []byte(c.text), &rep) {
			t.Fatalf("case %s: the oracle did not read its own text: %+v", c.name, rep)
		}
		wantText, err := m.Write(inst)
		if err != nil {
			t.Fatal(err)
		}
		wantWire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		if c.name == "guard_false_resting_set" && bytes.Contains(wantText, []byte(`"resting"`)) {
			t.Fatalf("the oracle writes a false-guarded key, so the witness proves nothing: %s", wantText)
		}
		cmd := exec.Command(bin)
		cmd.Stdin = strings.NewReader(c.text)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Errorf("case %s: the Rust probe failed: %v\n%s", c.name, err, stderr.String())
			continue
		}
		line, gotText, _ := bytes.Cut(out, []byte("\n"))
		gotWire, err := hex.DecodeString(string(line))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotWire, wantWire) {
			t.Errorf("case %s: the Rust save is %d bytes, the oracle's %d, first difference at byte %d", c.name, len(gotWire), len(wantWire), firstDiff(gotWire, wantWire))
		}
		if strings.TrimSpace(string(gotText)) != strings.TrimSpace(string(wantText)) {
			t.Errorf("case %s: the Rust text differs from the oracle's:\n%s\nwant:\n%s", c.name, gotText, wantText)
		}
	}
}
