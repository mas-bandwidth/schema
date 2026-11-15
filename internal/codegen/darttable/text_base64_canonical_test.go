package darttable

// A JSON bytes value accepts exactly the canonical base64 the writer writes
// (docs/SPEC-TABLES.md §16.2, §16.8; docs/rulings.json
// `json-base64-canonical`): the standard alphabet, padded, pad bits zero. A
// `bytes(N)` field and a `*bytes` blob are the same rule, so the generated
// Dart read and the Go engine (internal/tabletext) must answer the same
// verdict on every body, and the same bytes when they read it. The vectors
// under testdata/wire/json-base64-canonical/ are the landed standard's; the
// escaped spellings prove the JSON layer decides first, and the URL-safe
// bodies are spellings that decode but are not the canonical text.

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const dartJsonBase64Schema = `package probe

table Root {
	data bytes(8)
	blob *bytes
}
`

type dartJsonBase64Case struct {
	name    string
	field   string
	json    string
	verdict string
	wantHex string
}

// dartJsonBase64Vectors reads the standard's vector corpus: one row for the
// `bytes(N)` field and one for the `*bytes` blob, each with the manifest's
// verdict.
func dartJsonBase64Vectors(t *testing.T) []dartJsonBase64Case {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-base64-canonical")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("the vector manifest: %v", err)
	}
	var cases []dartJsonBase64Case
	for line := range strings.SplitSeq(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 5 {
			t.Fatalf("manifest row %q has %d columns", line, len(cols))
		}
		name, file, verdict := cols[0], cols[3], cols[4]
		body, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		text := string(body)
		if !strings.Contains(text, `"data"`) {
			t.Fatalf("%s: the vector does not name the data field: %s", name, text)
		}
		cases = append(cases,
			dartJsonBase64Case{name: name + "/bounded", field: "data", json: text, verdict: verdict},
			dartJsonBase64Case{name: name + "/pointer", field: "blob", json: strings.Replace(text, `"data"`, `"blob"`, 1), verdict: verdict},
		)
	}
	return cases
}

func dartJsonBase64Cases(t *testing.T) []dartJsonBase64Case {
	t.Helper()
	cases := dartJsonBase64Vectors(t)
	// The JSON layer decides first: these spell the same bytes with escapes.
	for _, field := range []string{"data", "blob"} {
		key := fmt.Sprintf(`{"%s":`, field)
		cases = append(cases,
			dartJsonBase64Case{
				name:    "escaped-solidus/" + field,
				field:   field,
				json:    key + `"\/\/\/\/"}`,
				verdict: "read",
				wantHex: "ffffff",
			},
			dartJsonBase64Case{
				name:    "escaped-letter/" + field,
				field:   field,
				json:    key + `"\u0041Q=="}`,
				verdict: "read",
				wantHex: "01",
			},
			dartJsonBase64Case{
				name:    "url-safe-alphabet/" + field,
				field:   field,
				json:    key + `"-_-_"}`,
				verdict: "refused",
			},
			dartJsonBase64Case{
				name:    "url-safe-unpadded/" + field,
				field:   field,
				json:    key + `"AQ-_"}`,
				verdict: "refused",
			},
		)
	}
	return cases
}

// dartJsonBase64Engine answers the Go engine's verdict and decoded bytes for
// one case. It is the reference the generated Dart read is held to.
func dartJsonBase64Engine(t *testing.T, model *tabletext.Model, def *ir.Struct, c dartJsonBase64Case) (string, string) {
	t.Helper()
	inst := model.New(def)
	var rep tabletext.Report
	if !model.Read(inst, []byte(c.json), &rep) {
		return "refused", ""
	}
	fv, found := inst.FieldByKey(c.field)
	if !found {
		t.Fatalf("%s: the table names no %s field", c.name, c.field)
	}
	data := fv.Cell.Str
	if c.field == "blob" {
		if fv.Cell.Blob == nil {
			t.Fatalf("%s: the engine read no blob", c.name)
		}
		data = fv.Cell.Blob.Data
	}
	return "read", hex.EncodeToString(data)
}

const dartJsonBase64Probe = `import 'dart:io';
import 'ProbeWire.dart';

String hex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();

void main(List<String> args) {
  for (var a = 0; a + 1 < args.length; a += 2) {
    final field = args[a];
    final r = TableTextReport();
    var out = 'refused';
    try {
      final v = rootFromJson(File(args[a + 1]).readAsStringSync(), r);
      if (!r.refused) {
        if (field == 'data') {
          out = 'read ${hex(v.data.codeUnits)}';
        } else {
          final b = v.blob;
          out = 'read ${b == null ? 'null' : hex(b)}';
        }
      }
    } on FormatException {
      out = 'refused';
    }
    stdout.writeln(out);
  }
}
`

// dartJsonBase64Run answers each case from the generated Dart read: `refused`,
// or `read <hex of the decoded bytes>`.
func dartJsonBase64Run(t *testing.T, files map[string][]byte, cases []dartJsonBase64Case) []string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var args []string
	for i, c := range cases {
		name := fmt.Sprintf("case-%02d.json", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(c.json), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, c.field, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(dartJsonBase64Probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart text read: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("the probe answered %d lines for %d cases: %q", len(lines), len(cases), out)
	}
	return lines
}

func TestDartJsonBase64Canonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")

	cases := dartJsonBase64Cases(t)
	u := buildUnit(t, dartJsonBase64Schema)
	model := tabletext.NewModel(u)
	def := model.Lookup("Root")
	if def == nil {
		t.Fatal("the probe unit declares no Root")
	}
	want := make([]string, len(cases))
	for i, c := range cases {
		verdict, wantHex := dartJsonBase64Engine(t, model, def, c)
		if c.verdict != "" && verdict != c.verdict {
			t.Fatalf("refuted: the engine answered %q for %s, the standard says %q", verdict, c.name, c.verdict)
		}
		if c.wantHex != "" && wantHex != c.wantHex {
			t.Fatalf("refuted: the engine read %s for %s, want %s", wantHex, c.name, c.wantHex)
		}
		if verdict == "read" {
			want[i] = "read " + wantHex
		} else {
			want[i] = "refused"
		}
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(files["ProbeWire.dart"]) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}
	got := dartJsonBase64Run(t, files, cases)
	for i, c := range cases {
		if got[i] != want[i] {
			t.Errorf("%s: Dart answered %q, the Go engine %q, for %s", c.name, got[i], want[i], c.json)
		}
	}
}
