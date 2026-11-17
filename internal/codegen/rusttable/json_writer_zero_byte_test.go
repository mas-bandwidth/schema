package rusttable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestRustJsonWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (JSON zero-byte writer probe)")
	keyedRequireRustc(t)

	u := unitFrom(t, `package probe
table Root {
    name string(16)
    names [..2]string(8)
    by_name map[string(8)]int32
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	layoutRustUnit(t, dir, "probe", files)
	main := `#![allow(dead_code, unused_imports, unused_variables, unused_mut)]
mod probe;
use probe::*;

fn must_refuse<F: FnOnce()>(f: F) {
    assert!(std::panic::catch_unwind(std::panic::AssertUnwindSafe(f)).is_err(), "JSON writer accepted a zero byte");
}

fn main() {
    let expected = r#"{
  "name": "ok",
  "names": [
    "item"
  ],
  "by_name": {
    "key": 1
  }
}"#;
    let mut good = Root::default();
    good.name = "ok".into();
    good.names.push(RootNamesText { value: "item".into() });
    good.by_name.push(RootByNameEntry { key: "key".into(), value: 1 });
    assert_eq!(root_to_json(&good).trim_end(), expected);
    assert_eq!(root_to_json_checked(&good).unwrap().trim_end(), expected);

    let mut string_value = Root::default();
    string_value.name = "a\0b".into();
    must_refuse(|| { let _ = root_to_json(&string_value); });
    must_refuse(|| { let _ = root_to_json_checked(&string_value); });

    let mut text_element = Root::default();
    text_element.names.push(RootNamesText { value: "a\0b".into() });
    must_refuse(|| { let _ = root_to_json(&text_element); });
    must_refuse(|| { let _ = root_to_json_checked(&text_element); });

    let mut map_key = Root::default();
    map_key.by_name.push(RootByNameEntry { key: "a\0b".into(), value: 1 });
    must_refuse(|| { let _ = root_to_json(&map_key); });
    must_refuse(|| { let _ = root_to_json_checked(&map_key); });
    println!("JSON_ZERO_BYTE_OK");
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe-bin")
	cmd := exec.Command("rustc", "--edition=2024", "--cfg", `feature="cook"`, "-A", "warnings", "-o", bin, "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("debug Rust compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "JSON_ZERO_BYTE_OK") {
		t.Fatalf("debug Rust probe: %v\n%s", err, out)
	}
}
