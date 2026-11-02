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
		fmt.Fprintf(&body, `{
 var value=new Probe.%s();var report=new Probe.TableReport();bool ok=Probe.Schema.%sFromJson(value,System.Text.Encoding.UTF8.GetBytes(%s),report);
 if(ok==%s||report.Malformed!=%s||report.KindMismatch!=%d||report.Clamped!=%d)throw new Exception("closure case%d result ok="+ok+" malformed="+report.Malformed+" mismatch="+report.KindMismatch+" clamp="+report.Clamped);
`, c.Root, c.Root, quoted, fmt.Sprint(c.Malformed), fmt.Sprint(c.Malformed), c.Mismatch, c.Clamped, i)
		if c.Reason != "" {
			reason, err := json.Marshal(c.Reason)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, "if(report.Reason!=%s)throw new Exception(\"named root refusal\");\n", reason)
		}
		if !c.Malformed {
			fmt.Fprintf(&body, `byte[] json=new byte[Probe.Schema.%sToJsonMeasure(value)];Probe.Schema.%sToJson(value,json);
 byte[] wire=new byte[Probe.Schema.%sMeasure(value)];if(Probe.Schema.%sSave(value,wire)!=wire.Length)throw new Exception("save");
 var back=new Probe.%s();var read=new Probe.TableReport();if(!Probe.Schema.%sLoad(back,wire,read)||read.Malformed||read.Refused||read.KindMismatch!=0||read.Clamped!=0)throw new Exception("roundtrip");
 byte[] again=new byte[Probe.Schema.%sToJsonMeasure(back)];Probe.Schema.%sToJson(back,again);if(!again.AsSpan().SequenceEqual(json))throw new Exception("lost closure payload");
`, c.Root, c.Root, c.Root, c.Root, c.Root, c.Root, c.Root, c.Root)
			if c.Root == "Nested" {
				body.WriteString("using var doc=System.Text.Json.JsonDocument.Parse(json);var choice=doc.RootElement.GetProperty(\"choice\");\n")
				if c.Mismatch == 0 {
					body.WriteString("if(choice.GetProperty(\"items\")[0].GetInt32()!=5)throw new Exception(\"union array arm lost\");\n")
				} else {
					body.WriteString("if(choice.EnumerateObject().MoveNext())throw new Exception(\"wrong shape union selected\");\n")
				}
			}
		}
		body.WriteString("}\n")
	}
	t15RunCS(t, string(schema), body.String())
}
