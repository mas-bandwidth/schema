package rusttable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func rustConstructProbe(t *testing.T, u *ir.Unit, body string) (string, error) {
	t.Helper()
	modules, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	layoutRustUnit(t, dir, "probe", modules)
	main := "#![allow(dead_code, unused_imports)]\nmod probe;\nuse probe::*;\nfn main() {\n" + body + "\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("rustc", "--edition", "2024", "--cfg", `feature="cook"`, "-O", "-A", "warnings", "-o", "run", "main.rs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native Rust compile: %v\n%s", err, out)
	}
	cmd = exec.Command(filepath.Join(dir, "run"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Exact literal endpoints independently name the intended values. The wire
// input is encoded from a range-free schema, so decoder clamping is exercised
// even when the text reader would otherwise clamp before producing the input.
func TestRustOneSidedRangeNative(t *testing.T) {
	slowtest.Gate(t, "rustc (native one-sided integer bounds and cook)")
	keyedRequireRustc(t)
	fixture := filepath.Join("../../..", "testdata/conformance/feature-witnesses/one-sided-range")
	schema, err := os.ReadFile(filepath.Join(fixture, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	cases, err := os.ReadFile(filepath.Join(fixture, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type rangeCase struct {
		field, input, value string
		clamps              int
	}
	var rustRangeCases []rangeCase
	for _, line := range strings.Split(string(cases), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Fields(line)
		if len(cols) != 4 {
			t.Fatalf("invalid boundary row: %q", line)
		}
		clamps, err := strconv.Atoi(cols[3])
		if err != nil || (clamps != 0 && clamps != 1) {
			t.Fatalf("invalid clamp count: %q", line)
		}
		rustRangeCases = append(rustRangeCases, rangeCase{cols[0], cols[1], cols[2], clamps})
	}
	if len(rustRangeCases) != 12 {
		t.Fatalf("boundary corpus has %d cases, want 12", len(rustRangeCases))
	}
	u := unitFrom(t, string(schema))
	rawSchema := string(schema)
	for _, bound := range []string{" | max = 18446744073709551610", " | min = 9007199254740993", " | max = -9007199254740993", " | min = -9223372036854775807"} {
		rawSchema = strings.ReplaceAll(rawSchema, bound, "")
	}

	rawU := unitFrom(t, rawSchema)
	m, rawM := tabletext.NewModel(u), tabletext.NewModel(rawU)
	work := t.TempDir()
	var body strings.Builder
	var goBody strings.Builder
	for i, tc := range rustRangeCases {
		text := fmt.Sprintf(`{"%s":%s}`, tc.field, tc.input)
		expected := fmt.Sprintf(`{"%s":%s}`, tc.field, tc.value)
		value := m.New(u.Tables["Ranges"])
		var report tabletext.Report
		if !m.Read(value, []byte(expected), &report) || report != (tabletext.Report{}) {
			t.Fatalf("literal expected value %s refused: %+v", expected, report)
		}
		want, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatal(err)
		}
		raw := rawM.New(rawU.Tables["Ranges"])
		report = tabletext.Report{}
		if !rawM.Read(raw, []byte(text), &report) || report != (tabletext.Report{}) {
			t.Fatalf("raw input %s refused: %+v", text, report)
		}
		wire, err := tablewire.Encode(rawM, raw)
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Join(work, fmt.Sprint(i))
		for name, data := range map[string][]byte{"input": wire, "want": want} {
			if err := os.WriteFile(base+"."+name, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for _, big := range []bool{false, true} {
			cook, err := tablecook.Cook(m, value, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			name := "le"
			if big {
				name = "be"
			}
			if err := os.WriteFile(base+"."+name, cook, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		values := map[string]string{"upper": "0", "lower": "9007199254740993", "negative": "-9007199254740993", "floor": "0"}
		values[tc.field] = tc.value
		rawExpr := "uint64(" + tc.input + ")"
		if strings.HasPrefix(tc.input, "-") {
			rawExpr = "uint64(int64Value(" + tc.input + "))"
		}
		slot := 0
		for j, name := range []string{"upper", "lower", "negative", "floor"} {
			if name == tc.field {
				slot = j
			}
		}
		fmt.Fprintf(&goBody, `{
v := Ranges{Upper:%s,Lower:%s,Negative:%s,Floor:%s}
buf := make([]byte,RangesFixedMeasure(1))
if RangesFixedSave([]Ranges{v},buf) != int64(len(buf)) {t.Fatal("fixed save")}
if err:=os.WriteFile(%q,buf,0600);err!=nil {t.Fatal(err)}
binary.LittleEndian.PutUint64(buf[len(buf)-RangesFixedRecordBytes+8+%d*8:],%s)
if err:=os.WriteFile(%q,buf,0600);err!=nil {t.Fatal(err)}
}
`, values["upper"], values["lower"], values["negative"], values["floor"], base+".fixed-want", slot, rawExpr, base+".fixed-input")
		fmt.Fprintf(&body, `{
let buf=std::fs::read(%q).unwrap();
let mut out=vec![RangesRow::default();1];
let mut plan=vec![TableFixedEntry::default();64];
let mut remap=vec![0u16;64];
let mut report=TableFixedReport::default();
assert_eq!(ranges_fixed_load(&mut out,&buf,&mut plan,&mut remap,&mut report),Some(1));
assert_eq!(report.clamped,%d,"fixed clamp case %d");
assert_eq!(out[0].%s,%s,"fixed value case %d");
let mut saved=vec![0u8;ranges_fixed_measure(1)];
assert_eq!(ranges_fixed_save(&out,&mut saved),Some(saved.len()));
assert_eq!(saved,std::fs::read(%q).unwrap(),"fixed bytes case %d");
}
`, base+".fixed-input", tc.clamps, i, tc.field, tc.value, i, base+".fixed-want", i)
		fmt.Fprintf(&body, `{
let mut tr = TableReport::default();
let tv = ranges_from_json(%q, &mut tr).expect("JSON value");
let mut wr = TableReport::default();
let wire = std::fs::read(%q).unwrap();
let wv = ranges_load(&wire, &mut wr).expect("wire value");
let want = std::fs::read(%q).unwrap();
assert_eq!(tr.clamped, %d, "JSON clamp case %d");
assert_eq!(wr.clamped, %d, "wire clamp case %d");
assert_eq!(ranges_save(&tv), want, "JSON bytes case %d");
assert_eq!(ranges_save(&wv), want, "wire bytes case %d");
for (order, path) in [(1u64, %q), (2u64, %q)] {
assert_eq!(ranges_cook_write(&tv, order).unwrap(), std::fs::read(path).unwrap(), "cook case %d");
}
println!("case %d ok");
}
`, text, base+".input", base+".want", tc.clamps, i, tc.clamps, i, i, i, base+".le", base+".be", i, i)
	}
	rustRangeFixedOracle(t, u, goBody.String())
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("one-sided native probe: %v\n%s", err, out)
	}
	if bytes.Count([]byte(out), []byte(" ok\n")) != len(rustRangeCases) {
		t.Fatalf("missing native cases: %s", out)
	}
}

// The native Go fixed writer supplies an independent form-3 image; mutating
// only its scalar payload leaves the digest and plan valid while exercising
// read-side bounds. Each field in this witness is exactly eight bytes.
func rustRangeFixedOracle(t *testing.T, u *ir.Unit, body string) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := gotable.GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}
	runtime := slowtest.SerializeGo(t, "../../..")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["oracle_test.go"] = []byte("package probe\nimport (\"encoding/binary\";\"os\";\"testing\")\nfunc int64Value(v int64) int64{return v}\nfunc TestWrite(t *testing.T){\n" + body + "\n}\n")
	dir := t.TempDir()
	for name, data := range files {
		if !strings.Contains(name, "/") {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("go", "test", "-timeout=0", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native Go fixed oracle: %v\n%s", err, out)
	}
}
