package darttable

// A COMMA IS TRAILING ONLY AFTER A VALUE (docs/SPEC-TABLES.md §16.2): the
// generated Dart text read drops a comma that follows a member or element and
// precedes the closing `}` or `]`, and refuses one that follows an opening
// bracket, a colon or another comma. The Go engine (internal/tabletext) is the
// oracle: each text below is read by both and must earn the same verdict, and
// the same value where accepted. The negative control plants the old
// unconditional comma rule into the emitted ProbeWire.dart and requires the
// leading-comma rows to turn into reads.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const dartTextCommaSchema = `package probe

table Root {
	val int32
	items [..8]int32
}

table ArrayRoot | json = "array" {
	items [..8]int32
}
`

// the emitted guard the fix added, and the old rule the control plants.
const dartTextCommaValueEndCheck = "        if (k >= 0 && stripped[k] != 0x7b && stripped[k] != 0x5b && stripped[k] != 0x2c && stripped[k] != 0x3a) continue;"
const dartTextCommaOldRule = "        continue;"

var dartTextCommaCases = []struct {
	name string
	root string
	json string
}{
	{"object-leading-comma", "Root", `{,}`},
	{"list-leading-comma", "Root", `{"items":[,]}`},
	{"object-trailing-comma", "Root", `{"a":1,}`},
	{"array-trailing-comma", "ArrayRoot", `[1,]`},
	{"object-comma-before-key", "Root", `{,"a":1}`},
	{"object-double-trailing-comma", "Root", `{"a":1,,}`},
}

const dartTextCommaProbe = `import 'dart:convert';
import 'dart:io';
import 'ProbeWire.dart';

void main(List<String> args) {
  for (var a = 0; a + 1 < args.length; a += 2) {
    final root = args[a];
    final text = File(args[a + 1]).readAsStringSync();
    final r = TableTextReport();
    try {
      final String out;
      if (root == 'Root') {
        out = rootToJson(rootFromJson(text, r));
      } else {
        out = arrayRootToJson(arrayRootFromJson(text, r));
      }
      if (r.refused) {
        stdout.writeln('refused');
      } else {
        stdout.writeln('read ${base64Encode(utf8.encode(out.trimRight()))}');
      }
    } on FormatException {
      stdout.writeln('refused');
    }
  }
}
`

// dartTextCommaRun answers each case's verdict from the generated Dart text
// read: `refused`, or `read <base64 of the canonical text>`.
func dartTextCommaRun(t *testing.T, edit func(map[string][]byte)) []string {
	t.Helper()
	slowtest.Gate(t, "dart")
	u := buildUnit(t, dartTextCommaSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(files["ProbeWire.dart"]) == 0 {
		t.Fatal("Generate emitted no ProbeWire.dart")
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var args []string
	for i, tc := range dartTextCommaCases {
		name := fmt.Sprintf("case-%d.json", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(tc.json), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, tc.root, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(dartTextCommaProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), append([]string{"--enable-asserts", "probe.dart"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated Dart text read: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(dartTextCommaCases) {
		t.Fatalf("the probe answered %d lines for %d cases: %q", len(lines), len(dartTextCommaCases), out)
	}
	return lines
}

// dartTextCommaMismatches compares the Dart verdicts with the Go engine's.
func dartTextCommaMismatches(t *testing.T, got []string) []string {
	t.Helper()
	model := tabletext.NewModel(buildUnit(t, dartTextCommaSchema))
	var msgs []string
	for i, tc := range dartTextCommaCases {
		def := model.Lookup(tc.root)
		if def == nil {
			t.Fatalf("the probe unit declares no %s", tc.root)
		}
		inst := model.New(def)
		var rep tabletext.Report
		want := "refused"
		if model.Read(inst, []byte(tc.json), &rep) {
			text, err := model.Write(inst)
			if err != nil {
				t.Fatalf("%s: model.Write: %v", tc.name, err)
			}
			want = "read " + base64.StdEncoding.EncodeToString(text)
		}
		if got[i] != want {
			msgs = append(msgs, fmt.Sprintf("%s: Dart answered %q, the Go engine %q, for %s", tc.name, got[i], want, tc.json))
		}
	}
	return msgs
}

func TestDartTextCommaFollowsAValue(t *testing.T) {
	t.Parallel()

	for _, msg := range dartTextCommaMismatches(t, dartTextCommaRun(t, nil)) {
		t.Error(msg)
	}
}

func TestDartTextCommaFollowsAValueNegativeControl(t *testing.T) {
	t.Parallel()

	got := dartTextCommaRun(t, func(files map[string][]byte) {
		total := 0
		for _, data := range files {
			total += bytes.Count(data, []byte(dartTextCommaValueEndCheck))
		}
		if total != 1 {
			t.Fatalf("the emitted pre-pass does not carry the value-end guard exactly once (%d)", total)
		}
		for name, data := range files {
			files[name] = bytes.Replace(data, []byte(dartTextCommaValueEndCheck), []byte(dartTextCommaOldRule), 1)
		}
	})
	for i := range 2 {
		if got[i] == "refused" {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s is still refused with the value-end guard removed; the row does not reach the rule", dartTextCommaCases[i].name)
		}
	}
	if len(dartTextCommaMismatches(t, got)) == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: the old unconditional comma rule left the row green")
	}
}
