package cstable

import (
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCSharpArrayRootClosureCorpus(t *testing.T) {
	slowtest.Gate(t, ".NET")
	base := "../../../testdata/conformance/feature-witnesses/array-root/closure"
	schema, err := os.ReadFile(filepath.Join(base, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(base, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Root, Text, Reason string
		Mismatch, Clamped  int
		Malformed          bool
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 12 {
		t.Fatal(len(cases))
	}
	var body strings.Builder
	for i, c := range cases {
		quoted, _ := json.Marshal(c.Text)
		// The strict read (docs/SPEC-TABLES.md §4, §6.5) returns the whole value or
		// refuses the whole load: a case the corpus records as a changed kind or an
		// out-of-range value is a refusal now, as the Go reference asserts, and
		// broken framing stays damage. The C# text read names a changed kind, an
		// out-of-range value and a count past its bound on the reason path.
		accept := !c.Malformed && c.Mismatch == 0 && c.Clamped == 0
		prefix := ""
		switch {
		case c.Mismatch != 0:
			prefix = "kind_mismatch"
		case c.Clamped != 0 && c.Root == "Bounded":
			prefix = "count_over_bound"
		case c.Clamped != 0:
			prefix = "out_of_range"
		}
		fmt.Fprintf(&body, `{
 var value=new Probe.%s();var report=new Probe.TableReport();bool ok=Probe.Schema.%sFromJson(value,System.Text.Encoding.UTF8.GetBytes(%s),report);
 if(ok!=%t||(%t&&!report.Damaged)||(!ok&&!report.Damaged&&!report.Refused)||(ok&&(report.Damaged||report.Refused))||(report.Reason??"").StartsWith(%q)==false)throw new Exception("closure case%d result ok="+ok+" damaged="+report.Damaged+" refused="+report.Refused+" reason="+report.Reason);
`, c.Root, c.Root, quoted, accept, c.Malformed, prefix, i)
		if c.Reason != "" {
			reason, err := json.Marshal(c.Reason)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, "if(report.Reason!=%s)throw new Exception(\"named root refusal\");\n", reason)
		}
		if accept {
			fmt.Fprintf(&body, `byte[] json=new byte[Probe.Schema.%sToJsonMeasure(value)];Probe.Schema.%sToJson(value,json);
 byte[] wire=new byte[Probe.Schema.%sMeasure(value)];if(Probe.Schema.%sSave(value,wire)!=wire.Length)throw new Exception("save");
 var back=new Probe.%s();var read=new Probe.TableReport();if(!Probe.Schema.%sLoad(back,wire,read)||read.Damaged||read.Refused)throw new Exception("roundtrip");
 byte[] again=new byte[Probe.Schema.%sToJsonMeasure(back)];Probe.Schema.%sToJson(back,again);if(!again.AsSpan().SequenceEqual(json))throw new Exception("lost closure payload");
`, c.Root, c.Root, c.Root, c.Root, c.Root, c.Root, c.Root, c.Root)
			if c.Root == "Nested" {
				body.WriteString("using var doc=System.Text.Json.JsonDocument.Parse(json);var choice=doc.RootElement.GetProperty(\"choice\");\n")
				body.WriteString("if(choice.GetProperty(\"items\")[0].GetInt32()!=5)throw new Exception(\"union array arm lost\");\n")
			}
		}
		body.WriteString("}\n")
	}
	t15RunCS(t, string(schema), body.String())
}
