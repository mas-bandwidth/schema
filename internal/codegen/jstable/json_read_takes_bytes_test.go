package jstable

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Every JSON read is handed BYTES and validates UTF-8 itself
// (docs/SPEC-TABLES.md §16.2). The JavaScript module gains a bytes entry
// beside its string entry: the bytes entry refuses an overlong form, an
// encoded surrogate, a truncated sequence and a code point above U+10FFFF,
// and reads clean UTF-8 to the same value the string entry reads. The string
// entry keeps its shape and refuses a lone surrogate.
func TestJsJsonReadTakesBytes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	node := jsNode(t)
	u := unitFrom(t, `package probe
table Root {
 name string(32)
 count int32
}
`)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	clean := `{"name":"héllo 😀","count":7}`
	cases := []struct {
		Name        string
		Text        string
		Bytes       []byte
		Lone        bool
		CheckString bool
		WantString  bool
		WantBytes   bool
		Same        bool
	}{
		{
			Name:        "clean",
			Text:        clean,
			Bytes:       []byte(clean),
			CheckString: true,
			WantString:  true,
			WantBytes:   true,
			Same:        true,
		},
		{
			Name:      "overlong",
			Bytes:     append(append([]byte(`{"name":"`), 0xc0, 0xaf), []byte(`","count":1}`)...),
			WantBytes: false,
		},
		{
			Name:      "encoded-surrogate",
			Bytes:     append(append([]byte(`{"name":"`), 0xed, 0xa0, 0x80), []byte(`","count":1}`)...),
			WantBytes: false,
		},
		{
			Name:      "truncated",
			Bytes:     append(append([]byte(`{"name":"`), 0xe2, 0x82), []byte(`","count":1}`)...),
			WantBytes: false,
		},
		{
			Name:      "above-max",
			Bytes:     append(append([]byte(`{"name":"`), 0xf4, 0x90, 0x80, 0x80), []byte(`","count":1}`)...),
			WantBytes: false,
		},
		{
			Name:        "lone-surrogate-string",
			Bytes:       append(append([]byte(`{"name":"`), 0xed, 0xa0, 0x80), []byte(`","count":1}`)...),
			Lone:        true,
			CheckString: true,
			WantString:  false,
			WantBytes:   false,
		},
	}
	spec, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.json"), spec, 0o600); err != nil {
		t.Fatal(err)
	}
	probe := `import { readFileSync } from "node:fs";
import * as m from "./` + filepath.Base(runtimeHome(u)) + `Variable.js";
const rows = JSON.parse(readFileSync("cases.json", "utf8"));
const hasBytes = typeof m.RootFromJsonBytes === "function";
for (const row of rows) {
  const text = row.Lone ? ('{"name":"' + String.fromCharCode(0xd800) + '","count":1}') : row.Text;
  const value = m.RootFromJson(text, new m.TableVariableReport());
  const stringOk = value !== null;
  let bytesOk = null, same = null;
  if (hasBytes) {
    const bytes = new Uint8Array(Buffer.from(row.Bytes, "base64"));
    const bvalue = m.RootFromJsonBytes(bytes, new m.TableVariableReport());
    bytesOk = bvalue !== null;
    same = bytesOk && stringOk ? (m.RootToJson(bvalue) === m.RootToJson(value)) : null;
  }
  console.log(JSON.stringify({ Name: row.Name, HasBytes: hasBytes, StringOK: stringOk, BytesOK: bytesOk, Same: same }));
}
`
	if err := os.WriteFile(filepath.Join(dir, "readbytes.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "readbytes.mjs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native bytes probe: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("got %d native rows, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var got struct {
				Name     string
				HasBytes bool
				StringOK bool
				BytesOK  *bool
				Same     *bool
			}
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatal(err)
			}
			if !got.HasBytes {
				t.Fatalf("%s: the module has no bytes entry", c.Name)
			}
			if got.BytesOK == nil {
				t.Fatalf("%s: the bytes entry answered nothing", c.Name)
			}
			if *got.BytesOK != c.WantBytes {
				t.Errorf("%s: bytes entry verdict %t, want %t (bytes %x)", c.Name, *got.BytesOK, c.WantBytes, c.Bytes)
			}
			if c.CheckString && got.StringOK != c.WantString {
				t.Errorf("%s: string entry verdict %t, want %t", c.Name, got.StringOK, c.WantString)
			}
			if c.Same {
				if got.Same == nil || !*got.Same {
					t.Errorf("%s: the bytes entry and the string entry read different values", c.Name)
				}
			}
		})
	}
}
