package cstable

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestOneSidedRangeCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	schema := `package probe
table Root {
 a int64 | min = 0
 b uint16 | max = 9
 c int8 | min = -3
 nums [..4]int32 | min = -10
}
`
	inputs := []string{
		`{}`, `{"a":17,"b":8,"c":-2,"nums":[-9,20]}`,
		`{"a":-5,"b":20,"c":-100,"nums":[-20,2147483647]}`,
		`{"a":9223372036854775807,"b":-1,"c":200}`,
	}
	t.Run("variable", func(t *testing.T) { runCarryCS(t, schema, inputs) })
	t.Run("fixed", func(t *testing.T) {
		runCarryCS(t, strings.Replace(schema, "table Root", "fixed table Root", 1), inputs, `var fixedValue = new Root { A = -12, B = 100, C = -100 };
 var clamp = new TableReport(); Schema.RootFixedClamp(fixedValue, clamp);
 if (fixedValue.A != 0 || fixedValue.B != 9 || fixedValue.C != -3 || clamp.Clamped != 3) throw new Exception("one-sided fixed clamp");
 byte[] fixedWire = new byte[Schema.RootFixedMeasure(1)];
 if (Schema.RootFixedSave(fixedValue, fixedWire) != fixedWire.Length) throw new Exception("one-sided fixed save");
 var fixedBack = new Root(); var fixedReport = new TableReport();
 if (Schema.RootFixedLoad(fixedBack, fixedWire, new TableFixedEntry[32], fixedReport) != 1 || fixedBack.A != 0 || fixedBack.B != 9 || fixedBack.C != -3 || fixedReport.Clamped != 0) throw new Exception("one-sided fixed load");`)
	})

}

func TestOptionalMapCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	runCarryCS(t, `package probe
table Inner {
 by_id ?map[uint32]int32
}
table Root {
 tags ?map[string(8)]int32
 inner Inner
 nest ?map[uint8]map[uint8]int32
}
`, []string{
		`{}`, `{"tags":null}`, `{"tags":{}}`,
		`{"tags":{"b":2,"a":1},"inner":{"by_id":{"7":4}},"nest":{"3":{"2":9}}}`,
		`{"tags":{"x":1,"x":2},"inner":{"by_id":{}},"nest":{}}`,
		`{"tags":{"x":1},"tags":null}`, `{"tags":[]}`,
	})
}

// Every case executes generated C# JSON read/write and wire save/load/save.
// The neutral oracle independently supplies the value, counters and exact bytes;
// absent and present-empty maps consequently cannot collapse into one state.
func runCarryCS(t *testing.T, schema string, inputs []string, extra ...string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	type result struct {
		OK                                                 bool
		Unknown, KindMismatch, Widened, Clamped, Duplicate int
		Malformed, Refused                                 bool
		Canon, Wire, Reason                                string
	}
	want := make([]result, len(inputs))
	dir := t.TempDir()
	var sources, cases []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	for i, input := range inputs {
		value := model.New(model.Lookup("Root"))
		report := tabletext.Report{}
		ok := model.Read(value, []byte(input), &report)
		want[i] = result{OK: ok, Unknown: report.Unknown, KindMismatch: report.KindMismatch, Widened: report.Widened, Clamped: report.Clamped, Duplicate: report.Duplicate, Malformed: report.Malformed, Refused: report.Refused, Reason: report.Reason}
		if ok {
			text, err := model.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatal(err)
			}
			want[i].Canon = strings.TrimRight(string(text), "\n")
			want[i].Wire = hex.EncodeToString(wire)
		}
		path := filepath.Join(dir, fmt.Sprintf("case-%d.json", i))
		if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, path)
	}
	const driver = `using System;
using System.IO;
using System.Text;
using System.Text.Json;
using Probe;
class CarryProbe {
 static void Main(string[] args) {
  // EXTRA
  foreach (string path in args) {
   var value = new Root(); var report = new TableReport();
   bool ok = Schema.RootFromJson(value, File.ReadAllBytes(path), report);
   string canon = "", wire = "";
   if (ok) {
    byte[] json = new byte[Schema.RootToJsonMeasure(value)];
    long textN = Schema.RootToJson(value, json);
    if (textN < 0) throw new Exception("JSON write refused");
    canon = Encoding.UTF8.GetString(json, 0, (int)textN).TrimEnd('\n');
    byte[] bytes = new byte[Schema.RootMeasure(value)];
    if (Schema.RootSave(value, bytes) != bytes.Length) throw new Exception("wire write size");
    wire = Convert.ToHexString(bytes).ToLowerInvariant();
    var back = new Root(); var read = new TableReport();
    if (!Schema.RootLoad(back, bytes, read) || read.Malformed || read.Refused || read.Clamped != 0 || read.Duplicate != 0 || read.Unknown != 0 || read.KindMismatch != 0 || read.Widened != 0) throw new Exception("canonical wire load");
    byte[] again = new byte[Schema.RootMeasure(back)];
    if (Schema.RootSave(back, again) != again.Length || !bytes.AsSpan().SequenceEqual(again)) throw new Exception("wire changed on load/save");
    byte[] backText = new byte[Schema.RootToJsonMeasure(back)];
    long backN = Schema.RootToJson(back, backText);
    if (backN < 0 || Encoding.UTF8.GetString(backText, 0, (int)backN).TrimEnd('\n') != canon) throw new Exception("wire lost value or presence");
   }
   Console.WriteLine(JsonSerializer.Serialize(new { OK=ok, report.Unknown, report.KindMismatch, report.Widened, report.Clamped, report.Duplicate, report.Malformed, report.Refused, report.Reason, Canon=canon, Wire=wire }));
  }
 }
}`
	main := filepath.Join(dir, "CarryProbe.cs")
	if err := os.WriteFile(main, []byte(strings.Replace(driver, "// EXTRA", strings.Join(extra, "\n"), 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	csc, refs, runtime := csDotnetToolchain(t)
	args := append([]string{"exec", csc, "-nologo", "-unsafe", "-nowarn:CS8500", "-out:" + filepath.Join(dir, "probe.dll")}, sources...)
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	cmd := exec.Command(csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	config := fmt.Sprintf(`{"runtimeOptions":{"tfm":"net10.0","framework":{"name":"Microsoft.NETCore.App","version":%q}}}`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(csDotnet(), append([]string{"exec", filepath.Join(dir, "probe.dll")}, cases...)...)
	cmd.Env = csDotnetEnv(dir, dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("got %d results, want %d: %s", len(lines), len(inputs), output)
	}
	for i, input := range inputs {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			var got result
			if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
				t.Fatal(err)
			}
			if got != want[i] {
				t.Errorf("input %s\ngot %+v\nwant %+v", input, got, want[i])
			}
		})
	}
}

func TestArrayRootCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t.Run("root", func(t *testing.T) {
		runCarryCS(t, `package probe
table Row { x int32 }
table Root | json = "array" {
 rows [..3]Row
}
`, []string{`[]`, `[{"x":4},{"x":9}]`, `[{"x":1},{"x":2},{"x":3},{"x":4}]`, `{"rows":[{"x":4}]}`, `[4]`, "\xef\xbb\xbf[{\"x\":3}]"})
	})
	t.Run("nested", func(t *testing.T) {
		runCarryCS(t, `package probe
table Row { x int32 }
table RowsView | json = "array" { rows []Row }
table Root { list RowsView }
`, []string{`{}`, `{"list":[]}`, `{"list":[{"x":4},{"x":9}]}`, `{"list":{"rows":[]}}`})
	})
}
