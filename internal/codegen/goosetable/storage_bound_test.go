package goosetable

// A COUNT OR A LENGTH THAT INDEXES A FIXED ARRAY'S STORAGE IS REFUSED BEFORE
// ANY BYTE, IN EVERY BUILD (docs/SPEC.md §5.1). Goose checks every index and
// aborts on a miss in every build; an abort is not a refusal, so each writer
// bounds the used count and the used length itself and answers its refusal
// (-1, false) first. Each case writes a batch whose first value is lawful and
// whose second carries the overrun into a destination poisoned with 0xA5,
// and requires the refusal with the poison intact: the typed text writer
// (ToJson, ToJsonMeasure), FixedSave, SaveMessages and the cook. The reversed
// witness drops the text writer's count bound and holds that the program
// aborts on the index past the storage.

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

const storageBoundSchema = `package probe
fixed table Root {
    xs   [..3]int32
    note string(4)
    tag  bytes(4)
}
`

// storageBoundCases set one count or length outside 0..its storage.
var storageBoundCases = []struct{ name, set string }{
	{"count_over", "v.XsCount = 4;"},
	{"count_negative", "v.XsCount = -1;"},
	{"text_over", "v.NoteLength = 5;"},
	{"text_negative", "v.NoteLength = -1;"},
	{"bytes_over", "v.TagLength = 5;"},
}

func storageBoundProgram(t *testing.T, edit func(map[string][]byte)) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(storageBoundSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := loadSchemaPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			fmt.Fprintf(&b, "import .%s;\n", before)
		}
	}
	fmt.Fprintf(&b, `
fn lawful() -> %[1]s::Root {
    var v = %[1]s::NewRoot();
    v.Xs[0] = 7; v.XsCount = 1;
    v.Note[0] = 0x61; v.NoteLength = 1;
    v.Tag[0] = 0x01; v.TagLength = 1;
    return v;
}
fn poisoned(n: i64) -> u8[>..] {
    var out: u8[>..] = [];
    for i in n { out.push(0xA5); }
    return out;
}
fn intact(out: const u8[:], n: i64) -> const u8[:] {
    if out.len != n { return "written"; }
    for i in n { if out[i] != 0xA5 { return "written"; } }
    return "intact";
}
fn probe(name: const u8[:], values: const %[1]s::Root[:]) {
    var j = poisoned(4);
    print("json.", name, "=", %[1]s::RootToJson(values[1], j), "|", intact(j, 4));
    print("jsonmeasure.", name, "=", %[1]s::RootToJsonMeasure(values[1]));
    var f = poisoned(4);
    print("fixed.", name, "=", %[1]s::RootFixedSave(values, f), "|", intact(f, 4));
    var m = poisoned(4);
    var r = schema_rt::MsgReport { .. };
    print("message.", name, "=", %[1]s::RootSaveMessages(values, m, r), "|", intact(m, 4));
    let cn = %[1]s::RootCookMeasure();
    var c = poisoned(cn);
    print("cook.", name, "=", %[1]s::RootCook(values[1], c, false), "|", intact(c, cn));
}

fn main() {
    var values: %[1]s::Root[2] = [%[1]s::Root { .. }; 2];
    values[0] = lawful(); values[1] = lawful();
    probe("lawful", values[0..2]);
`, ns)
	for _, c := range storageBoundCases {
		fmt.Fprintf(&b, "    { var v = lawful(); %s values[1] = v; probe(%q, values[0..2]); }\n", c.set, c.name)
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, ns
}

func TestGooseStorageBoundRefusesEveryWriter(t *testing.T) {
	t.Parallel()
	dir, _ := storageBoundProgram(t, nil)
	out := string(gooserun.Run(t, dir, "main.goose", nil))
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value:\n%s", line, out)
		}
		got[k] = v
	}
	for _, w := range []string{"json", "fixed", "message"} {
		if v := got[w+".lawful"]; strings.HasPrefix(v, "-") || !strings.HasSuffix(v, "|written") {
			t.Errorf("%s.lawful reads %q: the lawful batch must write", w, v)
		}
	}
	if v := got["cook.lawful"]; v != "true|written" {
		t.Errorf("cook.lawful reads %q, want true|written", v)
	}
	for _, c := range storageBoundCases {
		for k, want := range map[string]string{
			"json." + c.name:        "-1|intact",
			"jsonmeasure." + c.name: "-1",
			"fixed." + c.name:       "-1|intact",
			"message." + c.name:     "-1|intact",
			"cook." + c.name:        "false|intact",
		} {
			if got[k] != want {
				t.Errorf("%s reads %q, want %q", k, got[k], want)
			}
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	}
}

// THE REVERSED WITNESS: without the text writer's count bound, a count past
// the storage reaches the element read, and Goose aborts on the index.
func TestGooseStorageBoundNegativeControl(t *testing.T) {
	t.Parallel()
	const bound = "        if v.XsCount < 0 || (v.XsCount as i64) > 3 { return false; }\n"
	const anchor = bound + "        return schema_rt::json_write_array(out, v.XsCount,"
	dir, _ := storageBoundProgram(t, func(files map[string][]byte) {
		var hit string
		for name, data := range files {
			if n := strings.Count(string(data), anchor); n > 0 {
				if hit != "" || n != 1 {
					t.Fatalf("the plant's anchor does not stand once: %s and %s", hit, name)
				}
				hit = name
			}
		}
		if hit == "" {
			t.Fatalf("the plant's anchor %q stands in no generated file", anchor)
		}
		files[hit] = []byte(strings.Replace(string(files[hit]), anchor, strings.TrimPrefix(anchor, bound), 1))
	})
	compiler, why := gooserun.Compiler()
	if compiler == "" {
		t.Fatalf("the Goose compiler does not resolve: %s", why)
	}
	cmd := exec.Command(compiler, "run", "main.goose")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("the program without the count bound ran to the end; it owes an abort on the index past the storage:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "json.lawful=") || strings.Contains(stdout.String(), "json.count_over=") {
		t.Fatalf("the abort is not at count_over's text write (%v):\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
}
