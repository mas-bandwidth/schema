package darttable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Shared literal endpoint cases include integers beyond floating-point precision.
// The range-free schema supplies hostile wire without pre-clamping its payload.
func TestDartOneSidedRangeNative(t *testing.T) {
	slowtest.Gate(t, "dart")
	fixture := "../../../testdata/conformance/feature-witnesses/one-sided-range"
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := os.ReadFile(filepath.Join(fixture, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(schema)
	for _, bound := range []string{" | max = 18446744073709551610", " | min = 9007199254740993", " | max = -9007199254740993", " | min = -9223372036854775807"} {
		raw = strings.ReplaceAll(raw, bound, "")
	}
	u := unitFrom(t, string(schema))
	rawU := unitFrom(t, raw)
	model, rawModel := tabletext.NewModel(u), tabletext.NewModel(rawU)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	var body strings.Builder
	body.WriteString("import 'dart:io';\nimport 'dart:typed_data';\nimport 'ProbeWire.dart' as w;\nimport 'ProbeCook.dart' as c;\nvoid check(bool b,String s){if(!b)throw StateError(s);}\nbool same(List<int>a,List<int>b)=>a.length==b.length&&List.generate(a.length,(i)=>a[i]==b[i]).every((x)=>x);\nvoid main(){\n")
	n := 0
	for line := range strings.SplitSeq(string(rows), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Fields(line)
		if len(cols) != 4 {
			t.Fatal(line)
		}
		input := fmt.Sprintf(`{"%s":%s}`, cols[0], cols[1])
		expected := fmt.Sprintf(`{"%s":%s}`, cols[0], cols[2])
		v, rv := model.New(u.Tables["Ranges"]), rawModel.New(rawU.Tables["Ranges"])
		var r tabletext.Report
		if !model.Read(v, []byte(expected), &r) || !r.Silent() {
			t.Fatalf("expected: %+v", r)
		}
		r = tabletext.Report{}
		if !rawModel.Read(rv, []byte(input), &r) || !r.Silent() {
			t.Fatalf("input: %+v", r)
		}
		want, err := tablewire.Encode(model, v)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(rawModel, rv)
		if err != nil {
			t.Fatal(err)
		}
		prefix := fmt.Sprint(n)
		for name, data := range map[string][]byte{"wire": wire, "want": want} {
			if err := os.WriteFile(filepath.Join(dir, prefix+"."+name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, big := range []bool{false, true} {
			data, err := tablecook.Cook(model, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			suffix := "le"
			if big {
				suffix = "be"
			}
			if err := os.WriteFile(filepath.Join(dir, prefix+"."+suffix), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Fprintf(&body, `{
 final want=File('%s.want').readAsBytesSync();
 final jr=w.TableTextReport();final j=w.rangesFromJson(%q,jr);
 check(jr.clamped==%s,'JSON %d: ${jr.clamped}');
 check(same(w.rangesWireSave(j),want),'JSON bytes %d');
 final wr=w.TableWireReport();final v=w.rangesWireLoad(File('%s.wire').readAsBytesSync(),wr);
 check(!wr.malformed&&!wr.refused&&wr.clamped==%s,'wire %d: ${wr.clamped}/${wr.malformed}/${wr.refused}');
 check(same(w.rangesWireSave(v),want),'wire bytes %d');
 for(final big in [false,true]){
  final size=c.RangesCookWire.cookMeasure(v);check(size>0,'cook measure');
  final out=Uint8List(size);check(c.RangesCookWire.cook(v,out,big?Endian.big:Endian.little),'cook');
  check(same(out,File('%s.${big?'be':'le'}').readAsBytesSync()),'cook bytes %d');
 }
}
`, prefix, input, cols[3], n, n, prefix, cols[3], n, n, prefix, n)
		n++
	}
	if n != 12 {
		t.Fatalf("expected12 range cases, got%d", n)
	}
	body.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native Dart range: %v\n%s", err, out)
	}
}
