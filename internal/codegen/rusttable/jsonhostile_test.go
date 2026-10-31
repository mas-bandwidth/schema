package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJsonHostileTextRefusalsRust pins the two §16.3 refusals the Rust JSON
// reader carries beside Go and C (testdata/conformance/tables/MANIFEST.txt,
// json-hostile str-illformed-at-bound, str-zero-escape-value,
// str-zero-escape-map-key) at EVERY site they name, not only the three the
// conformance rows reach:
//
//   - text_holds_zero: a U+0000 the read would PLACE in text storage refuses
//     the read — a string(N), wstring(N) or *string value, a string map key
//     (over its bound too) and an integer map key, whose key is a JSON string
//     whatever its kind (text lists are not a Rust construct yet, §2.13). A zero the read only
//     SKIPS (an unknown key's value, an object key) or compares is not the
//     event, and `\u0001` is not affected.
//   - string_not_utf8: bytes that are not well-formed UTF-8 refuse the read at
//     every string site, before any clamp, never replaced by U+FFFD.
//
// make tables-rust-json-hostile-negative-control plants the old reader (the
// zero stored as text, the ill-formed byte decoded lossily) through an overlay
// and requires this test to go red on exactly the refusal rows.
func TestJsonHostileTextRefusalsRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated JSON probe)")

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	modules, err := Generate(unitFrom(t, jsonHostileProbeSrc))
	if err != nil {
		t.Fatalf("the probe unit was refused: %v", err)
	}
	root := t.TempDir()
	var mods []string
	// the text form's modules alone, as TestCardTextFormMatchesGo builds them:
	// the cook and the view are other surfaces with a crate root of their own
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
	main.WriteString(jsonHostileProbeMain)
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
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "MISMATCH ") {
			t.Errorf("%s", line)
		}
	}
	if !strings.Contains(string(out), "PROBE DONE") {
		t.Fatalf("the probe did not finish:\n%s", out)
	}
}

const jsonHostileProbeSrc = `package probe

table Probe
{
    name  string(8)
    wide  wstring(8)
    blob  *string
    names map[string(8)]int32
    by_id map[uint16]int32
}
`

// The cases are byte strings so an ill-formed byte reaches the reader as it
// would from a file; the JSON escapes are spelled `\\u` in the Rust literal.
const jsonHostileProbeMain = `
use probe_table::*;
use variable_runtime::*;

fn verdict(text: &[u8]) -> String {
    let mut r = TableReport::default();
    match probe_from_json_bytes(text, &mut r) {
        Some(_) if !r.malformed => format!("{},{},{},{},{}", r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate),
        _ => "refused".to_string(),
    }
}

fn main() {
    let cases: &[(&str, &[u8], &str)] = &[
        ("zero-string", b"{\"name\": \"a\\u0000b\"}", "refused"),
        ("zero-wstring", b"{\"wide\": \"a\\u0000b\"}", "refused"),
        ("zero-blob", b"{\"blob\": \"a\\u0000b\"}", "refused"),
        ("zero-map-key", b"{\"names\": {\"a\\u0000b\": 1}}", "refused"),
        ("zero-map-key-over-bound", b"{\"names\": {\"aaaaaaaaa\\u0000\": 1}}", "refused"),
        ("zero-integer-map-key", b"{\"by_id\": {\"1\\u0000\": 1}}", "refused"),
        ("zero-after-lone-lead", b"{\"name\": \"\\ud800\\u0000\"}", "refused"),
        ("zero-unknown-value", b"{\"nope\": \"a\\u0000b\"}", "1,0,0,0,0"),
        ("zero-object-key", b"{\"a\\u0000b\": 1}", "1,0,0,0,0"),
        ("one-escape-stored", b"{\"name\": \"a\\u0001b\"}", "0,0,0,0,0"),
        ("illformed-at-bound", b"{\"name\": \"aaaaaaaa\xff\"}", "refused"),
        ("illformed-inside", b"{\"name\": \"a\xffb\"}", "refused"),
        ("illformed-truncated", b"{\"name\": \"a\xe2\x82\"}", "refused"),
        ("illformed-surrogate-bytes", b"{\"name\": \"\xed\xa0\x80\"}", "refused"),
        ("illformed-unknown-value", b"{\"nope\": \"\xff\"}", "refused"),
        ("illformed-object-key", b"{\"\xff\": 1}", "refused"),
        ("illformed-map-key", b"{\"names\": {\"\xff\": 1}}", "refused"),
        ("well-formed-at-bound", b"{\"name\": \"aaaaaaaa\xc3\xa9\"}", "0,0,0,1,0"),
    ];
    for (name, text, want) in cases {
        let got = verdict(text);
        if got != *want {
            println!("MISMATCH {name}: got {got}, want {want}");
        }
    }
    // a lone lead surrogate rides as U+FFFD and the escape after it is read
    // on its own, as Go's scanText does
    let mut r = TableReport::default();
    match probe_from_json_bytes(b"{\"name\": \"\\ud800\\u0041\"}", &mut r) {
        Some(v) if v.name == "\u{fffd}A" => {}
        Some(v) => println!("MISMATCH lone-lead-then-escape: got {:?}, want \"\\u{{fffd}}A\"", v.name),
        None => println!("MISMATCH lone-lead-then-escape: refused"),
    }
    // the &str entry point reads the same text the bytes entry point does
    let mut r = TableReport::default();
    if probe_from_json("{\"name\": \"a\\u0000b\"}", &mut r).is_some() {
        println!("MISMATCH str-entry-zero: the &str read stored a zero");
    }
    println!("PROBE DONE");
}
`
