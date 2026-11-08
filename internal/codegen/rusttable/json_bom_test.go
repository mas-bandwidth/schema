package rusttable

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsonLeadingBOMBoundaryRust pins the §16.2 / §16.12 UTF-8 byte-order mark
// rule on the Rust JSON entry points (`<root>_from_json_bytes` and
// `<root>_from_json`): at most ONE leading `[0xEF, 0xBB, 0xBF]` at byte 0 is
// skipped before the JSON value, never inside `Jr::space`, and `U+FEFF` inside
// string values is preserved.
func TestJsonLeadingBOMBoundaryRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated JSON BOM boundary probe)")

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	u := unitFrom(t, `package probe
table Probe {
    value int32 = 7
    tag string(32)
}
`)
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
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

	const doc = "{\"value\":9,\"tag\":\"ok\"}\n"
	cases := []struct {
		name  string
		input string
		reuse bool
	}{
		{"empty-plain", "{}\n", false},
		{"one-leading-bom-empty", "\xef\xbb\xbf{}\n", false},
		{"two-leading-boms-empty", "\xef\xbb\xbf\xef\xbb\xbf{}\n", false},
		{"plain", doc, false},
		{"one-leading-bom", "\xef\xbb\xbf" + doc, false},
		{"two-leading-boms", "\xef\xbb\xbf\xef\xbb\xbf" + doc, false},
		{"space-before-bom", " \xef\xbb\xbf{}\n", false},
		{"newline-before-bom", "\n\xef\xbb\xbf{}\n", false},
		{"comment-before-bom", "/*layout*/\xef\xbb\xbf{}\n", false},
		{"bom-inside-object", "{\xef\xbb\xbf\"value\":9}", false},
		{"bom-before-value", "{\"value\":\xef\xbb\xbf9}", false},
		{"bom-after-document", "{}\n\xef\xbb\xbf", false},
		{"truncated-bom", "\xef\xbb{}\n", false},
		{"field-string-raw-feff", "{\"tag\":\"a\xef\xbb\xbfb\"}", false},
		{"field-string-escaped-feff", "{\"tag\":\"a\\uFEFFb\"}", false},
		{"reuse-events", "{\"unknown\":1,\"value\":\"bad\",\"value\":9}", true},
		{"reuse-damage", "\xef\xbb\xbf\xef\xbb\xbf" + doc, true},
		{"reuse-valid-leading-bom", "\xef\xbb\xbf" + doc, true},
		{"reuse-field-string", "{\"tag\":\"a\xef\xbb\xbfb\"}", true},
	}

	model := tabletext.NewModel(u)
	def := model.Lookup("Probe")
	if def == nil {
		t.Fatal("missing Probe")
	}

	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, m := range mods {
		main.WriteString("mod " + m + ";\n")
	}
	main.WriteString(`
use probe_table::*;
use probe_variable::*;
use variable_runtime::*;

fn hex_encode(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

fn check_case(
    name: &str,
    input: &[u8],
    check_str: bool,
    rep: &mut TableReport,
    rep_str: &mut TableReport,
    want_ok: bool,
    want_u: u64,
    want_k: u64,
    want_w: u64,
    want_c: u64,
    want_d: u64,
    want_m: bool,
    want_canon: &[u8],
    want_wire_hex: &str,
) {
    let got = probe_from_json_bytes(input, rep);
    let got_ok = got.is_some();
    let mut got_canon = Vec::new();
    let mut got_wire_hex = String::new();
    if let Some(ref v) = got {
        got_canon = probe_to_json(v).into_bytes();
        let saved = probe_save(v);
        got_wire_hex = hex_encode(&saved);
    }
    println!(
        "CASE {name}: ok={got_ok} u={} w={} malformed={} canon_len={} wire={got_wire_hex}",
        rep.unknown, rep.widened, rep.damaged, got_canon.len()
    );
    if got_ok != want_ok
        || (want_ok && (got_canon.as_slice() != want_canon || got_wire_hex != want_wire_hex))
    {
        println!(
            "MISMATCH {name} (bytes): got ok={got_ok} ({},{},{},{},{},{}) canon={:?} wire={got_wire_hex}, want ok={want_ok} ({want_u},{want_k},{want_w},{want_c},{want_d},{want_m}) canon={:?} wire={want_wire_hex}",
            rep.unknown,
            0,
            rep.widened,
            0,
            0,
            rep.damaged,
            String::from_utf8_lossy(&got_canon),
            String::from_utf8_lossy(want_canon),
        );
    }
    if check_str {
        let s = std::str::from_utf8(input).expect("valid utf8 for str check");
        let got_s = probe_from_json(s, rep_str);
        let got_s_ok = got_s.is_some();
        let mut got_s_canon = Vec::new();
        let mut got_s_wire_hex = String::new();
        if let Some(ref v) = got_s {
            got_s_canon = probe_to_json(v).into_bytes();
            let saved = probe_save(v);
            got_s_wire_hex = hex_encode(&saved);
        }
        if got_s_ok != want_ok
            || (want_ok && (got_s_canon.as_slice() != want_canon || got_s_wire_hex != want_wire_hex))
        {
            println!(
                "MISMATCH {name} (str): got ok={got_s_ok} ({},{},{},{},{},{}) canon={:?} wire={got_s_wire_hex}, want ok={want_ok} ({want_u},{want_k},{want_w},{want_c},{want_d},{want_m}) canon={:?} wire={want_wire_hex}",
                rep_str.unknown,
                0,
                rep_str.widened,
                0,
                0,
                rep_str.damaged,
                String::from_utf8_lossy(&got_s_canon),
                String::from_utf8_lossy(want_canon),
            );
        }
    }
}

fn main() {
    let mut reused = TableReport::default();
    let mut reused_str = TableReport::default();
`)

	var reusedGo tabletext.Report
	for _, c := range cases {
		rep := &tabletext.Report{}
		if c.reuse {
			rep = &reusedGo
		}
		inst := model.New(def)
		ok := model.Read(inst, []byte(c.input), rep)
		var canon []byte
		var wireHex string
		if ok {
			var err error
			canon, err = model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			canon = append(canon, '\n')
			wire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatal(err)
			}
			wireHex = hex.EncodeToString(wire)
		}
		checkStr := utf8.ValidString(c.input)
		if c.reuse {
			fmt.Fprintf(&main, "    check_case(%q, &%s, %v, &mut reused, &mut reused_str, %v, %d, %d, %d, %d, %d, %v, &%s, %q);\n",
				c.name, rustByteSlice([]byte(c.input)), checkStr,
				ok, rep.Unknown, 0, rep.Widened, 0, 0, rep.Refused,
				rustByteSlice(canon), wireHex)
		} else {
			fmt.Fprintf(&main, "    { let mut r = TableReport::default(); let mut rs = TableReport::default(); check_case(%q, &%s, %v, &mut r, &mut rs, %v, %d, %d, %d, %d, %d, %v, &%s, %q); }\n",
				c.name, rustByteSlice([]byte(c.input)), checkStr,
				ok, rep.Unknown, 0, rep.Widened, 0, 0, rep.Refused,
				rustByteSlice(canon), wireHex)
		}
	}
	main.WriteString("    println!(\"BOM PROBE DONE\");\n}\n")

	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "probe")
	build := exec.Command("rustc", "--edition", "2024", "-o", bin, "main.rs")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build (modules %v): %v\n%s", mods, err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	t.Logf("Rust BOM boundary probe output:\n%s", string(out))
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "MISMATCH ") {
			t.Errorf("%s", line)
		}
	}
	if !strings.Contains(string(out), "BOM PROBE DONE") {
		t.Fatalf("the probe did not finish:\n%s", out)
	}
}

func rustByteSlice(b []byte) string {
	var parts []string
	for _, x := range b {
		parts = append(parts, fmt.Sprintf("0x%02xu8", x))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
