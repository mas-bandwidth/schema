package rusttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// writeRefusalReachSchema puts a value out of the write contract on every
// carrier the variable-form save writes besides the plain field: a bytes(N)
// field, an optional scalar, an array element, a counted array's count and a
// bytes(N) length inside an array body, a union arm's payload and a keyed
// array's slot.
const writeRefusalReachSchema = `package probe

enum Grade { Bronze, Silver, Gold }

union Pick
{
    number int32 | min = 0, max = 10
    label  string(8)
    raw    bytes(4)
    list   [..3]int32
}

table Config
{
    blob     bytes(4)
    o_amount ?int32 | min = 0, max = 10
    o_chan   ?bits(6)
    fixed_a  [3]int32 | min = 0, max = 10
    o_list   ?[..4]int32 | min = 0, max = 10
    pick     Pick
    scores   [Grade]int32 | min = 0, max = 10
}
`

// TestRustWriteRefusalReachesEveryCarrier pins the variable-form save's write
// contract (docs/SPEC.md §5.1: Rust variable Save, rows 1–2 and 4 debug) on
// every carrier of writeRefusalReachSchema. Each was written with no check
// before, so an out-of-contract value was cut or written silently
// (docs/silent-failure-audit-1122.md SF-09, SF-10, SF-12..SF-15; SF-11, an
// optional string(N) or bytes(N), and the string half of SF-15, a
// [E]string(M), are refused by the checker, docs/SPEC-TABLES.md §15, §2.13).
// One subtest per finding.
func TestRustWriteRefusalReachesEveryCarrier(t *testing.T) {
	t.Parallel()
	out := generate(t, writeRefusalReachSchema)
	var src strings.Builder
	for _, data := range out {
		src.Write(data)
	}
	whole := src.String()
	for _, c := range []struct {
		id   string
		want []string
	}{
		{"SF-09", []string{
			`debug_assert!(v.blob.len() <= 4, "blob length past 4");`,
		}},
		{"SF-10", []string{
			`debug_assert!((*opt) >= 0 && (*opt) <= 10, "o_amount out of range [0, 10]");`,
			`debug_assert!((*opt) <= 63, "o_chan exceeds bits(6) width");`,
		}},
		{"SF-12", []string{
			`debug_assert!((*e) >= 0 && (*e) <= 10, "fixed_a out of range [0, 10]");`,
			`debug_assert!((*e) >= 0 && (*e) <= 10, "o_list out of range [0, 10]");`,
		}},
		{"SF-13", []string{
			`debug_assert!(opt.len() <= 4, "o_list count past 4");`,
			`debug_assert!(arm.len() <= 3, "list count past 3");`,
			`debug_assert!(arm.len() <= 4, "raw length past 4");`,
		}},
		{"SF-14", []string{
			`debug_assert!((*arm) >= 0 && (*arm) <= 10, "number out of range [0, 10]");`,
			`debug_assert!(arm.len() <= 8, "label length past 8");`,
			`debug_assert!(!arm.as_bytes().contains(&0), "label holds a zero byte");`,
		}},
		{"SF-15", []string{
			`debug_assert!((*e) >= 0 && (*e) <= 10, "scores out of range [0, 10]");`,
		}},
	} {
		t.Run(c.id, func(t *testing.T) {
			for _, want := range c.want {
				if !strings.Contains(whole, want) {
					t.Errorf("%s: the variable-form save does not emit %q", c.id, want)
				}
			}
		})
	}
}

// TestRustWriteRefusalReachesEveryCarrierNative compiles the generated save in
// a DEBUG build and proves each assert fires: an in-contract value saves, and
// each out-of-contract value panics with the message naming its field.
func TestRustWriteRefusalReachesEveryCarrierNative(t *testing.T) {
	keyedRequireRustc(t)
	dir := t.TempDir()
	layoutRustUnit(t, dir, "probe", generate(t, writeRefusalReachSchema))
	cases := []struct{ id, set, panic string }{
		{"in-contract", "c.blob = vec![1; 4]; c.o_amount = Some(10); c.pick = Pick::Label(String::from(\"12345678\"));", ""},
		{"SF-09", "c.blob = vec![1; 5];", "blob length past 4"},
		{"SF-10-range", "c.o_amount = Some(11);", "o_amount out of range [0, 10]"},
		{"SF-10-bits", "c.o_chan = Some(64);", "o_chan exceeds bits(6) width"},
		{"SF-12-fixed", "c.fixed_a = vec![1, 11, 1];", "fixed_a out of range [0, 10]"},
		{"SF-12-optional", "c.o_list = Some(vec![11]);", "o_list out of range [0, 10]"},
		{"SF-13-count", "c.o_list = Some(vec![1; 5]);", "o_list count past 4"},
		{"SF-13-arm-count", "c.pick = Pick::List(vec![1; 4]);", "list count past 3"},
		{"SF-13-arm-bytes", "c.pick = Pick::Raw(vec![1; 5]);", "raw length past 4"},
		{"SF-14-range", "c.pick = Pick::Number(11);", "number out of range [0, 10]"},
		{"SF-14-length", "c.pick = Pick::Label(String::from(\"123456789\"));", "label length past 8"},
		{"SF-14-zero", "c.pick = Pick::Label(String::from(\"a\\0b\"));", "label holds a zero byte"},
		{"SF-15", "c.scores = vec![1, 11, 1];", "scores out of range [0, 10]"},
	}
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\nmod probe;\nuse probe::*;\nfn main() {\n    let which = std::env::args().nth(1).expect(\"a case\");\n    let mut c = Config::default();\n    match which.as_str() {\n")
	for _, c := range cases {
		main.WriteString("        \"" + c.id + "\" => { " + c.set + " }\n")
	}
	main.WriteString("        other => panic!(\"unknown case {other}\"),\n    }\n    let _ = config_save(&c);\n    println!(\"saved\");\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	// no -O: debug assertions are on, which is the build the contract names
	cmd := testproc.Command(t, "rustc", "--edition", "2024", "-A", "warnings", "-o", "run", "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native Rust compile: %v\n%s", err, out)
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			cmd := testproc.Command(t, filepath.Join(dir, "run"), c.id)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			switch {
			case c.panic == "" && (err != nil || !strings.Contains(string(out), "saved")):
				t.Fatalf("%s: an in-contract value did not save: %v\n%s", c.id, err, out)
			case c.panic != "" && (err == nil || !strings.Contains(string(out), c.panic)):
				t.Fatalf("%s: the debug save did not refuse with %q: %v\n%s", c.id, c.panic, err, out)
			}
		})
	}
}
