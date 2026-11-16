package darttable

// THE JSON FORM'S WIDE INTEGERS AND ITS INTEGER TOKEN (docs/SPEC-TABLES.md
// §16.2, §16.14 W10; docs/rulings.json json-wide-integers-are-strings and
// json-integer-token-only). An integer whose storage is wider than 32 bits is
// read and written as a JSON string of decimal digits; every integer field
// takes an integer token only.
//
// The rows are the vectors in testdata/wire/json-integers: every accept row
// reads and writes the committed canonical text byte for byte, and every
// refuse row refuses the whole read. Running the generated program makes this
// a functional test behind slowtest.Gate.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const jsonIntegersSchema = `package probe

table Integers
{
    small   int32
    wide    int64
    uwide   uint64
    wide128 int128 | min = -170141183460469231731687303715884105727, max = 170141183460469231731687303715884105727
    ranged  int64 | min = 0, max = 9007199254740993
    frac    fixed(48, 16) | min = -10, max = 10
    counts  map[uint64]int32
}
`

// TestDartJsonWideIntegersAreStrings holds the json-integers vectors through
// the generated Dart writer and reader: a wide integer is written as a STRING
// of decimal digits, reads only a string, and every integer field
// takes an integer token only — no fraction, no exponent, no negative zero.
func TestDartJsonWideIntegersAreStrings(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dart")
	dart := dartBinary(t)

	u := unitFrom(t, jsonIntegersSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, packetFiles(t, u))
	if _, ok := files["ProbeWire.dart"]; !ok {
		t.Fatal("Generate emitted no ProbeWire.dart for the json-integers unit")
	}

	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	numberTokens, err := os.ReadFile(filepath.Join(dir, "NUMBER-TOKEN.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	numberNames := map[string]bool{}
	for line := range strings.SplitSeq(string(numberTokens), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			fields := strings.Split(line, "\t")
			if len(fields) != 5 {
				t.Fatalf("NUMBER-TOKEN.tsv: malformed row %q", line)
			}
			numberNames[fields[0]] = true
		}
	}
	if len(numberNames) != 13 {
		t.Fatalf("NUMBER-TOKEN.tsv: got %d rows, want 13", len(numberNames))
	}
	data = append(append(data, '\n'), numberTokens...)
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}

	var body strings.Builder
	body.WriteString("import 'dart:convert';\nimport 'dart:io';\nimport 'ProbeWire.dart' as w;\n")
	body.WriteString("void check(bool b,String s){if(!b)throw StateError(s);}\n")
	body.WriteString("bool same(List<int> a,List<int> b){if(a.length!=b.length)return false;for(var i=0;i<a.length;i++){if(a[i]!=b[i])return false;}return true;}\n")
	body.WriteString("void main(){\n")
	body.WriteString(" final canonical = File('canonical.read.json').readAsBytesSync();\n")
	body.WriteString(" final want = List<int>.from(canonical)..add(0x0A);\n")

	rows := 0
	jsonFiles := map[string][]byte{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("%s/MANIFEST.tsv:%d: want name, root, json, verdict, description, got %d columns", dir, i+1, len(fields))
		}
		name, root, jsonFile, verdict := fields[0], fields[1], fields[2], fields[3]
		if root != "Integers" {
			t.Fatalf("%s: root %q, want Integers", name, root)
		}
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", name, verdict)
		}
		text, err := os.ReadFile(filepath.Join(dir, jsonFile))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		jsonFiles[jsonFile] = text
		switch verdict {
		case "read":
			fmt.Fprintf(&body, `{
 final r = w.TableTextReport();
 final v = w.integersFromJson(File(%q).readAsStringSync(), r);
 check(!r.refused, '%s read: ' + r.reason);
 final out = w.integersToJson(v);
 check(same(utf8.encode(out), want), '%s must write the canonical text: ' + out);
}
`, jsonFile, name, name)
		case "refused":
			if numberNames[name] {
				fmt.Fprintf(&body, `{ final r = w.TableTextReport(); w.integersFromJson(File(%q).readAsStringSync(), r); check(r.refused && r.reason.startsWith('kind_mismatch'), '%s must refuse kind_mismatch: ' + r.reason); }
`, jsonFile, name)
				break
			}
			fmt.Fprintf(&body, `{
 final r = w.TableTextReport();
 w.integersFromJson(File(%q).readAsStringSync(), r);
 check(r.refused, '%s must refuse');
}
`, jsonFile, name)
		}
		rows++
	}
	if rows == 0 {
		t.Fatal("no manifest rows read; the test would pass over an empty set")
	}
	body.WriteString("}\n")

	writeDir := t.TempDir()
	writeFiles(t, writeDir, files)
	if err := os.WriteFile(filepath.Join(writeDir, "canonical.read.json"), canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, text := range jsonFiles {
		if err := os.WriteFile(filepath.Join(writeDir, name), text, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(writeDir, "probe.dart"), []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(dart, "--enable-asserts", "probe.dart")
	cmd.Dir = writeDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Dart json-integers probe: %v\n%s", err, out)
	}
}
