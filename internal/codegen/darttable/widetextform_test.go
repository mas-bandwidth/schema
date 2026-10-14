package darttable

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestDartWideTextThroughGeneratedWire(t *testing.T) {
	src := `package probe
 table Root { value wstring(3)
 entries map[uint16]wstring(3)
 after int32
 body *string }
 `
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files["ProbeWire.dart"]) == 0 {
		t.Fatal("wstring root must carry form1")
	}
	m := tabletext.NewModel(u)
	var checks strings.Builder
	for _, text := range []string{"", "A", "Ω漢", "💡", "A💡", "💡Z"} {
		root := m.New(u.Tables["Root"])
		root.Fields[0].Cell.Units = utf16.Encode([]rune(text))
		root.Fields[0].Count = len(root.Fields[0].Cell.Units)
		root.Fields[2].Cell.U = 77
		root.Fields[2].Cell.I = 77
		entry := m.New(u.Tables["Root"].Fields[1].MapEntry)
		entry.Fields[0].Cell.U = 1
		entry.Fields[0].Cell.I = 1
		entry.Fields[1].Cell.Units = utf16.Encode([]rune(text))
		entry.Fields[1].Count = len(entry.Fields[1].Cell.Units)
		root.Fields[1].Entries = []tabletext.Cell{{Tab: entry}}
		wire, err := tablewire.Encode(m, root)
		if err != nil {
			t.Fatal(err)
		}
		json, err := m.Write(root)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&checks, "{final v=Root()..value=%q..after=77;v.entries.add(%s()..key=1..value=%q);final wire=rootWireSave(v);check(hex(wire)==%q,'Go wide wire');check(rootToJson(v)==%q,'Go wide JSON');final r=TableWireReport();final back=rootWireLoad(wire,r);check(!r.malformed&&r.clamped==0&&back.after==77,'clean wide read');check(hex(rootWireSave(back))==hex(wire),'wide resave');check(hex(rootWireSave(rootFromJson(rootToJson(v))))==hex(wire),'JSON wire identity');}\n", text, u.Tables["Root"].Fields[1].MapEntry.Name, text, hex.EncodeToString(wire), string(json)+"\n")
	}
	producer := unitFrom(t, strings.ReplaceAll(src, "wstring(3)", "wstring(16)"))
	pm := tabletext.NewModel(producer)
	p := pm.New(producer.Tables["Root"])
	if !pm.Read(p, []byte(`{"value":"A💡Z","entries":{"1":"A💡Z"},"after":77}`), &tabletext.Report{}) {
		t.Fatal("producer JSON")
	}
	input, err := tablewire.Encode(pm, p)
	if err != nil {
		t.Fatal(err)
	}
	back := m.New(u.Tables["Root"])
	r := tabletext.Report{}
	if _, err := tablewire.Decode(m, back, input, &r); err != nil {
		t.Fatal(err)
	}
	normalized, err := tablewire.Encode(m, back)
	if err != nil {
		t.Fatal(err)
	}
	if r.Clamped != 2 || r.Malformed {
		t.Fatal("Go wide clamp oracle", r)
	}
	fmt.Fprintf(&checks, "{final r=TableWireReport();final v=rootWireLoad(base64Decode(%q),r);check(!r.malformed&&r.clamped==2&&v.after==77,'wide boundary clamp');check(hex(rootWireSave(v))==%q,'Go clamped wire');}\n", base64.StdEncoding.EncodeToString(input), hex.EncodeToString(normalized))
	for _, payload := range [][]byte{{0, 0xd8}, {0, 0xdc}, {65}, {0, 0}, {65, 0, 66, 0, 0, 0xdc}} {
		for _, repeat := range []bool{false, true} {
			input := wideTextForgedWire(payload, fieldId(u.Tables["Root"].Fields[0]), fieldId(u.Tables["Root"].Fields[2]))
			if repeat {
				input = append([]byte{1, 1, 33, 2, 65, 0}, input[1:]...)
			}
			v := m.New(u.Tables["Root"])
			r := tabletext.Report{}
			if _, err := tablewire.Decode(m, v, input, &r); err != nil {
				t.Fatal(err)
			}
			if !r.Malformed || v.Fields[2].Cell.U != 77 {
				t.Fatal("Go bad wide oracle", r)
			}
			normalized, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&checks, "{final r=TableWireReport();final v=rootWireLoad(base64Decode(%q),r);check(r.malformed&&r.clamped==0&&v.after==77,'bad wide parent continuation');check(hex(rootWireSave(v))==%q,'Go bad wide recovery');}\n", base64.StdEncoding.EncodeToString(input), hex.EncodeToString(normalized))
		}

	}
	for _, input := range []string{`{"value":"A💡Z","entries":{"1":"A💡Z"},"after":77}`, `{"value":"A","value":"\ud800","after":77}`, `{"value":"A","value":"\u0000","after":77}`, `{"value":"A💡\ud800","after":77}`} {
		v := m.New(u.Tables["Root"])
		r := tabletext.Report{}
		m.Read(v, []byte(input), &r)
		text, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&checks, "{final r=TableTextReport();final v=rootFromJson(%q,r);check(r.malformed==%v&&r.clamped==%d&&r.duplicate==%d&&r.kindMismatch==%d,'wide JSON recovery events');check(rootToJson(v)==%q,'Go wide JSON recovery');}\n", input, r.Malformed, r.Clamped, r.Duplicate, r.KindMismatch, string(text)+"\n")
	}
	built := m.New(u.Tables["Root"])
	built.Fields[0].Cell.Units = []uint16{0xd800, 0}
	built.Fields[0].Count = 2
	builtText, err := m.Write(built)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&checks, "{final v=Root()..value=String.fromCharCodes([0xd800,0]);check(rootToJson(v)==%q,'Go built wide JSON replacement and NUL escape');}\n", string(builtText)+"\n")

	// C++ JSON places/resets a valid-key entry before rejecting its value shape
	// (cpptable/json.go). Go's tool currently lacks that shape gate in readMap;
	// use its encoder on independently constructed canonical storage here.
	mapInput := `{"entries":{"1":"A","1":3},"after":77}`
	observed := m.New(u.Tables["Root"])
	observedReport := tabletext.Report{}
	m.Read(observed, []byte(mapInput), &observedReport)
	t.Logf("Go raw map wrongshape reference gap: %+v", observedReport)
	canonical := m.New(u.Tables["Root"])
	canonical.Fields[2].Cell.U = 77
	canonical.Fields[2].Cell.I = 77
	entry := m.New(u.Tables["Root"].Fields[1].MapEntry)
	entry.Fields[0].Cell.U = 1
	entry.Fields[0].Cell.I = 1
	canonical.Fields[1].Entries = []tabletext.Cell{{Tab: entry}}
	mapWire, err := tablewire.Encode(m, canonical)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&checks, "{final r=TableTextReport();final v=rootFromJson(%q,r);check(!r.malformed&&r.kindMismatch==1&&r.duplicate==1&&v.after==77,'C++ valid-key wrongshape resets entry default');check(hex(rootWireSave(v))==%q,'Go canonical map storage wire');}\n", mapInput, hex.EncodeToString(mapWire))

	t.Run("runtime-production-oracle", func(t *testing.T) {
		slowtest.Gate(t, "dart")
		dir := t.TempDir()
		for name, data := range files {
			if !strings.Contains(name, "/") {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		probe := "import 'dart:convert';\nimport 'ProbeWire.dart';\nString hex(List<int> b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nvoid check(bool b,String why){if(!b)throw StateError(why);}\nvoid main(){\n" + checks.String() + "}\n"
		if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(probe), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"analyze", "--fatal-infos", "probe.dart"}, {"--enable-asserts", "probe.dart"}} {
			cmd := exec.Command(dartBinary(t), args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
		}
	})
}
