package darttable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestDartArrayRootNative(t *testing.T) {
	slowtest.Gate(t, "dart")
	fixture := "../../../testdata/conformance/feature-witnesses/array-root"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(fixture, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, Root, Input string }
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 10 {
		t.Fatalf("cases=%d", len(cases))
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var body strings.Builder
	body.WriteString("import 'dart:io';\nimport 'ProbeWire.dart' as w;\nvoid check(bool b,String s){if(!b)throw StateError(s);}\nString hex(List<int> b)=>b.map((x)=>x.toRadixString(16).padLeft(2,'0')).join();\nvoid main(){\n")
	for _, tc := range cases {
		v := m.New(u.Tables[tc.Root])
		var r tabletext.Report
		ok := m.Read(v, []byte(tc.Input), &r)
		fn := dartName(tc.Root)
		if !ok {
			fmt.Fprintf(&body, "{var failed=false;try{w.%sFromJson(%q);}on FormatException{failed=true;}check(failed,%q);}\n", fn, tc.Input, tc.Name+" must refuse")
			continue
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		text, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&body, `{final r=w.TableTextReport();final v=w.%sFromJson(%q,r);
check(r.clamped==%d&&r.kindMismatch==%d,%q);
check(hex(w.%sWireSave(v))==%q,%q);
check(w.%sToJson(v)==%q,%q);
}
`, fn, tc.Input, r.Clamped, r.KindMismatch, tc.Name+" report", fn, hex.EncodeToString(wire), tc.Name+" wire", fn, string(text)+"\n", tc.Name+" JSON")
	}
	body.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("array-root native: %v\n%s", err, out)
	}
}
