package tabletext_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// textZeroSchema holds every text kind the pack text reader places: string(N),
// *string, wstring(N), a bounded and an unbounded text-list element,
// a map key, and a string(N) one table down.
const textZeroSchema = `package probe

table Inner
{
    s string(8)
}

table Root
{
    t string(8)
    s *string
    w wstring(8)
    list [..4]string(8)
    open []string(8)
    m map[string(8)]int32
    inner Inner
    n int32
}
`

func textZeroModel(t *testing.T) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(textZeroSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// roundTrip encodes what the read left behind, decodes it, and hands back the
// decoded instance's text: a value any read produced is one the wire carries
// and reads back undamaged (docs/SPEC-TABLES.md §16.3).
func roundTrip(t *testing.T, m *tabletext.Model, inst *tabletext.Instance, label string) string {
	t.Helper()
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("%s: Encode refused the value the read left: %v", label, err)
	}
	back := m.New(inst.Def)
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, wire, &r); !ok || err != nil || r.Malformed {
		t.Fatalf("%s: the wire Encode wrote decodes damaged: ok=%v err=%v report=%+v wire=%x", label, ok, err, r, wire)
	}
	text, err := m.Write(back)
	if err != nil {
		t.Fatalf("%s: Write: %v", label, err)
	}
	return string(text)
}

// A ZERO IN TEXT TAKES ONE RULE IN EVERY FORM (docs/SPEC-TABLES.md §16.3): the
// pack text reader refuses a \u0000 bound for any text storage, names the
// innermost field, and never lands a value its own wire calls malformed. A
// \u0001 control reads clean; a zero only skipped or in a key is not the event.
func TestPackTextReaderRefusesZeroInText(t *testing.T) {
	t.Parallel()
	m := textZeroModel(t)
	root := m.Lookup("Root")
	forms := []struct {
		key, value, reason string
	}{
		{"t", `"%s"`, "Root.t"},
		{"s", `"%s"`, "Root.s"},
		{"w", `"%s"`, "Root.w"},
		{"list", `["%s"]`, "Root.list"},
		{"open", `["ok","%s"]`, "Root.open"},
		{"m", `{"%s":1}`, "Root.m"},
		{"inner", `{"s":"%s"}`, "Inner.s"},
	}
	for _, form := range forms {
		for _, at := range []string{`\u0000ab`, `a\u0000b`, `ab\u0000`} {
			label := form.key + " " + at
			text := fmt.Sprintf(`{"n":7,"%s":%s}`, form.key, fmt.Sprintf(form.value, at))
			inst := m.New(root)
			var r tabletext.Report
			if m.Read(inst, []byte(text), &r) {
				t.Errorf("%s: Read accepted a zero in text: %s", label, text)
				continue
			}
			if !r.Malformed || r.Reason != "text_holds_zero: "+form.reason {
				t.Errorf("%s: report %+v, want Malformed with Reason text_holds_zero: %s", label, r, form.reason)
			}
			roundTrip(t, m, inst, label)

			// the control: the same text with U+0001 reads clean and the
			// value round-trips the wire to the same text
			control := strings.ReplaceAll(text, `\u0000`, `\u0001`)
			inst = m.New(root)
			r = tabletext.Report{}
			if !m.Read(inst, []byte(control), &r) || !r.Silent() {
				t.Errorf("%s: the \\u0001 control did not read clean: %+v", label, r)
				continue
			}
			want, err := m.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			if got := roundTrip(t, m, inst, label+" control"); got != string(want) {
				t.Errorf("%s: control round trip\n got %s\nwant %s", label, got, want)
			}
		}
	}

	// a zero the read only SKIPS or COMPARES stores nothing and is not the event
	for _, text := range []string{
		`{"gone":"a\u0000b","n":1}`,
		`{"gone":["a\u0000b"],"n":1}`,
		`{"a\u0000b":"x","n":1}`,
	} {
		inst := m.New(root)
		var r tabletext.Report
		if !m.Read(inst, []byte(text), &r) || r.Malformed || r.Unknown != 1 {
			t.Errorf("%s: a skipped zero is not the event: %+v", text, r)
		}
	}
}

// The probe the cold read named, through the examples' GlobalSettings: the
// build_note string(48) read from text never reaches a wire Decode refuses.
func TestPackTextReaderRefusesZeroInBuildNote(t *testing.T) {
	t.Parallel()
	m, inst, r := read(t, "GlobalSettings", `{"build_note":"a\u0000b"}`)
	if !r.Malformed || r.Reason != "text_holds_zero: GlobalSettings.build_note" {
		t.Fatalf("report %+v, want Malformed with Reason text_holds_zero: GlobalSettings.build_note", r)
	}
	roundTrip(t, m, inst, "build_note")
}
