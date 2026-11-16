package pythontable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonScalarWidenHistorical(t *testing.T) {
	slowtest.Gate(t, "historical scalar widening on both Python pins")
	cases := []struct{ old, new, value, check string }{
		{"int8", "int16", "-100", "-100"}, {"int16", "int32", "-1234", "-1234"},
		{"int32", "int64", "-123456", "-123456"}, {"int64", "int128", `"-9007199254740993"`, "-9007199254740993"},
		{"uint8", "uint16", "250", "250"}, {"uint8", "bits(9)", "250", "250"}, {"uint16", "uint32", "65000", "65000"},
		{"uint32", "uint64", "4000000000", "4000000000"}, {"uint64", "uint128", `"18446744073709551615"`, "18446744073709551615"},
		{"float32", "float64", "-1.5", "-1.5"},
	}
	for _, generic := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s_to_%s_generic_%t", tc.old, tc.new, generic), func(t *testing.T) {
				extra := ""
				if generic {
					extra = "\n extra []int32"
				}
				writer := loadUnit(t, fmt.Sprintf("package scalarwiden\ntable Root { value %s%s }\n", tc.old, extra))
				readerType := tc.new
				if tc.new == "int128" {
					readerType += " | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727"
				}
				reader := loadUnit(t, fmt.Sprintf("package scalarwiden\ntable Root { value %s%s }\n", readerType, extra))
				input := `{"value":` + tc.value + `}`
				wire := wideEngineWire(t, writer, "Root", input)
				if tc.old == "int16" && !generic {
					cpp := cppWideWire(t, writer, wideVTConstruct{root: "Root", json: input})
					if !bytes.Equal(cpp, wire) {
						t.Fatal("old Go and C++ writers disagree")
					}
				}
				model := tabletext.NewModel(reader)
				value := model.New(model.Lookup("Root"))
				var report tabletext.Report
				ok, err := tablewire.Decode(model, value, wire, &report)
				if err != nil || !ok || report.Widened != 1 {
					t.Fatalf("Go reference: %v %v %+v", ok, err, report)
				}
				expected, err := tablewire.Encode(model, value)
				if err != nil {
					t.Fatal(err)
				}
				files, err := Generate(reader)
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				writeTree(t, dir, files)
				if out, err := pyrun.Analyze(t, dir, "scalarwiden"); err != nil {
					t.Fatalf("mypy: %v %s", err, out)
				}
				probe := pyrun.ImportPrelude + fmt.Sprintf(`from scalarwiden import Root, LoadReport
with open("wire", "rb") as f: data = f.read()
r = LoadReport()
v = Root.load(data, r)
assert v.value == %s and r.widened == 1
print(v.save().hex())
`, tc.check)
				inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
				for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
					stdout, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
					if err != nil || stdout != fmt.Sprintf("%x\n", expected) {
						t.Errorf("%s output %q stderr %s err %v", python, stdout, stderr, err)
					}
				}
			})
		}
	}
}

func TestPythonScalarWidenRefusals(t *testing.T) {
	slowtest.Gate(t, "scalar widening refusals and report atomicity")
	for _, tc := range []struct{ name, old, new, value, reason string }{
		{"narrow", "int32", "int16", "100", "kind_mismatch"},
		{"signedness", "int8", "uint16", "100", "kind_mismatch"},
		{"range", "int16", "int32 | min = -10, max = 10", "11", "out_of_range"},
		{"float_narrow", "float64", "float32", "1.5", "kind_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := loadUnit(t, fmt.Sprintf("package scalarwiden\ntable Root { value %s }\n", tc.old))
			reader := loadUnit(t, fmt.Sprintf("package scalarwiden\ntable Root { value %s }\n", tc.new))
			wire := wideEngineWire(t, writer, "Root", `{"value":`+tc.value+`}`)
			model := tabletext.NewModel(reader)
			value := model.New(model.Lookup("Root"))
			var rep tabletext.Report
			ok, err := tablewire.Decode(model, value, wire, &rep)
			if ok || err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("Go expected %s: %v %v %+v", tc.reason, ok, err, rep)
			}
			files, err := Generate(reader)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := pyrun.ImportPrelude + fmt.Sprintf(`from scalarwiden import Root, LoadReport, Refused
with open("wire", "rb") as f: data = f.read()
r=LoadReport(); r.widened=77; r.unknown=66
try: Root.load(data,r)
except Refused as e:
 assert e.reason == %q, e.reason
 assert r.widened == 77 and r.unknown == 66
 print("refused")
else: raise AssertionError("accepted")
`, tc.reason)
			inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
			for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, python, dir, "main.py", inputs)
				if err != nil || out != "refused\n" {
					t.Errorf("%s: %s %s %v", python, out, stderr, err)
				}
			}
		})
	}
}

func TestPythonScalarWidenDamageAndControl(t *testing.T) {
	slowtest.Gate(t, "scalar widening malformed input and planted control")
	writer := loadUnit(t, "package scalarwiden\ntable Root { value int16 }\n")
	reader := loadUnit(t, "package scalarwiden\ntable Root { value int32 }\n")
	wire := wideEngineWire(t, writer, "Root", `{"value":1234}`)
	// Remove one byte from the scalar payload without altering the ID trailer.
	damaged := append(append([]byte{}, wire[:3]...), wire[4:]...)
	model := tabletext.NewModel(reader)
	v := model.New(model.Lookup("Root"))
	var report tabletext.Report
	ok, err := tablewire.Decode(model, v, damaged, &report)
	if ok || tablewire.RefusalReason(err) != "wire_damaged" {
		t.Fatalf("oracle damage %v %v", ok, err)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from scalarwiden import Root, LoadReport, Refused
with open("wire", "rb") as f: data=f.read()
r=LoadReport(); r.widened=77; r.unknown=66
try: Root.load(data,r)
except Refused as e:
 assert e.reason == "wire_damaged", e.reason
 assert r.widened == 77 and r.unknown == 66
 print("damaged")
else: raise AssertionError("accepted truncated scalar")
`
	for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, py, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": damaged})
		if err != nil || out != "damaged\n" {
			t.Fatalf("%s: %q %s %v", py, out, stderr, err)
		}
	}
	path := "scalarwiden/_schema/load.py"
	src := string(files[path])
	anchor := "if not _widens(kind, field.kind):"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("widen control anchor missing or repeated")
	}
	files[path] = []byte(strings.Replace(src, anchor, "if True:", 1))
	control := t.TempDir()
	writeTree(t, control, files)
	accept := pyrun.ImportPrelude + `from scalarwiden import Root
with open("wire", "rb") as f: Root.load(f.read())
`
	for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, py, control, "main.py", map[string][]byte{"main.py": []byte(accept), "wire": wire})
		if err == nil || !strings.Contains(stderr, "kind_mismatch") {
			t.Fatalf("control did not catch scalar widening: %s %v", stderr, err)
		}
	}
}

// Map keys count once per map; widened value fields count per entry.
func TestPythonScalarWidenMapInteraction(t *testing.T) {
	slowtest.Gate(t, "scalar values and map keys widen independently")
	writer := loadUnit(t, "package probe\ntable Root { values map[int8]int8 }\n")
	reader := loadUnit(t, "package probe\ntable Root { values map[int32]int32 }\n")
	text := `{"values":{"-3":4,"2":5}}`
	wire := pythonVTMapEngineWire(t, writer, "Root", text)
	canonical := pythonVTMapEngineWire(t, reader, "Root", text)
	mapKeyWireProbe(t, reader, "Root", wire, canonical, 3, "")
}

func TestPythonScalarWidenNonFinite(t *testing.T) {
	slowtest.Gate(t, "non-finite widened float and atomic report")
	writer := loadUnit(t, "package scalarwiden\ntable Root { value float32 }\n")
	reader := loadUnit(t, "package scalarwiden\ntable Root { value float64 }\n")
	wire := wideEngineWire(t, writer, "Root", `{"value":1.5}`)
	if len(wire) < 7 || wire[1] != 1 || wire[2] != 10 {
		t.Fatalf("unexpected float wire %x", wire)
	}
	binary.LittleEndian.PutUint32(wire[3:7], 0x7fc00000)
	model := tabletext.NewModel(reader)
	value := model.New(model.Lookup("Root"))
	var report tabletext.Report
	ok, err := tablewire.Decode(model, value, wire, &report)
	if ok || tablewire.RefusalReason(err) != "non_finite_float" {
		t.Fatalf("Go non-finite: %v %v", ok, err)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from scalarwiden import Root, Refused, LoadReport
with open("wire", "rb") as f: data=f.read()
r=LoadReport();r.widened=77;r.unknown=66
try: Root.load(data,r)
except Refused as e:
 assert e.reason=="non_finite_float" and r.widened==77 and r.unknown==66
 print("refused")
else: raise AssertionError("accepted non-finite float")
`
	for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, py, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil || out != "refused\n" {
			t.Errorf("%s: %q %s %v", py, out, stderr, err)
		}
	}
}

func TestPythonOptionalScalarWiden(t *testing.T) {
	slowtest.Gate(t, "historical optional scalar widening")
	writer := loadUnit(t, "package scalarwiden\ntable Root { value ?int8 }\n")
	reader := loadUnit(t, "package scalarwiden\ntable Root { value ?int32 }\n")
	wire := wideEngineWire(t, writer, "Root", `{"value":7}`)
	model := tabletext.NewModel(reader)
	value := model.New(model.Lookup("Root"))
	var report tabletext.Report
	ok, err := tablewire.Decode(model, value, wire, &report)
	if !ok || err != nil || report.Widened != 1 {
		t.Fatalf("Go: %v %v %+v", ok, err, report)
	}
	expected, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from scalarwiden import Root, LoadReport
with open("wire", "rb") as f: data=f.read()
r=LoadReport();v=Root.load(data,r)
assert v.value==7 and r.widened==1
print(v.save().hex())
`
	for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, py, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil || out != fmt.Sprintf("%x\n", expected) {
			t.Errorf("%s: %q %s %v", py, out, stderr, err)
		}
	}
}
