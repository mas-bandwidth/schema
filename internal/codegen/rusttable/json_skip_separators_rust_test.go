package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestRustJsonSkippedValueIsWellFormed holds a value under a key the reader
// cannot name to the same JSON grammar as a value the reader places
// (docs/SPEC-TABLES.md §16.2): a colon after every object key, and after every
// value either a comma or the closer. The dialect adds trailing commas,
// comments and a BOM and nothing else, so `[1 2 3]`, `{"k" 1}`, `{"k": 1 "m":
// 2}` and adjacent literals are malformed. The compiler's own text reader is
// the oracle: every verdict below is compared with internal/tabletext's read
// of the same text in this test.
func TestRustJsonSkippedValueIsWellFormed(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated JSON skipped-value probe)")

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	u := unitFrom(t, `package probe

table Node
{
    v int32
}

table Probe
{
    epoch     int32
    timestamp int32
    peer      *Node
}
`)
	model := tabletext.NewModel(u)
	def := model.Lookup("Probe")
	if def == nil {
		t.Fatal("missing Probe")
	}

	cases := []struct {
		name string
		text string
		ok   bool
	}{
		{"array-missing-comma", `{"zz": [1 2 3]}`, false},
		{"object-key-missing-colon", `{"zz": {"k" 1}}`, false},
		{"object-missing-comma", `{"zz": {"k": 1 "m": 2}}`, false},
		{"array-adjacent-literals", `{"zz": [true false]}`, false},
		{"array-joined-literals", `{"zz": [truefalse]}`, false},
		{"object-label-missing-colon", `{"zz": {"&node" 5}}`, false},
		{"placed-missing-comma", `{"epoch": 1 "timestamp": 2}`, false},
		{"array-trailing-comma", `{"zz": [1, 2, 3,]}`, true},
		{"object-separated", `{"zz": {"k": 1, "m": 2}}`, true},
	}

	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("the probe unit was refused: %v", err)
	}
	root := t.TempDir()
	var mods []string
	for _, m := range []string{"variable_runtime", "probe_table", "probe_variable"} {
		data, ok := modules[m+".rs"]
		if !ok {
			t.Fatalf("missing module %s.rs; got %v", m, keys(modules))
		}
		if err := os.WriteFile(filepath.Join(root, m+".rs"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, m)
	}

	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, m := range mods {
		main.WriteString("mod " + m + ";\n")
	}
	main.WriteString(`
use probe_table::*;
use variable_runtime::*;

fn verdict(text: &[u8]) -> (bool, u64) {
    let mut r = TableReport::default();
    match probe_from_json_bytes(text, &mut r) {
        Some(_) if !r.damaged => (true, r.unknown),
        _ => (false, 0),
    }
}

fn main() {
`)
	for _, c := range cases {
		rep := &tabletext.Report{}
		inst := model.New(def)
		goOK := model.Read(inst, []byte(c.text), rep)
		if goOK != c.ok {
			t.Errorf("%s: internal/tabletext read %q as ok=%v, want ok=%v (%s)", c.name, c.text, goOK, c.ok, rep.Reason)
		}
		if goOK && rep.Unknown != 1 {
			t.Errorf("%s: internal/tabletext counted unknown=%d, want 1", c.name, rep.Unknown)
		}
		if goOK {
			fmt.Fprintf(&main, "    { let (ok, u) = verdict(&%s); if !ok || u != %d { println!(\"MISMATCH %s: ok={} u={}\", ok, u); } }\n",
				rustByteSlice([]byte(c.text)), rep.Unknown, c.name)
		} else {
			fmt.Fprintf(&main, "    { let (ok, _) = verdict(&%s); if ok { println!(\"MISMATCH %s: accepted\"); } }\n",
				rustByteSlice([]byte(c.text)), c.name)
		}
	}
	main.WriteString("    println!(\"SKIP SEPARATORS PROBE DONE\");\n}\n")

	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "probe")
	args := []string{"--edition", "2024"}
	// pass the host's cc by its absolute path: rustc then runs that file
	// directly instead of resolving the linker name itself
	if cc, err := exec.LookPath("cc"); err == nil {
		args = append(args, "-C", "linker="+cc)
	}
	args = append(args, "-o", bin, "main.rs")
	build := exec.Command("rustc", args...)
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build (modules %v): %v\n%s", mods, err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "MISMATCH ") {
			t.Errorf("%s", line)
		}
	}
	if !strings.Contains(string(out), "SKIP SEPARATORS PROBE DONE") {
		t.Fatalf("the probe did not finish:\n%s", out)
	}
}
