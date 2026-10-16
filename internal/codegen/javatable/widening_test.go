package javatable

import (
	"strings"
	"testing"
)

// TestWideningReasonsJava pins the variable reader's report contract: a
// compatible older kind widens the value and leaves the named reason beside
// the counter, while an unsupported form is refused by the reference name.
func TestWideningReasonsJava(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, `package widening
table Sample { value int64 }
`))
	if err != nil {
		t.Fatal(err)
	}
	runtime := string(files["TableVariable.java"])
	all := runtime
	for name, body := range files {
		if name != "TableVariable.java" {
			all += string(body)
		}
	}
	if !strings.Contains(runtime, "public String reason;") {
		t.Fatal("TableVariable.Report has no reason field")
	}
	if !strings.Contains(all, `report.reason==null)report.reason="widened"`) {
		t.Fatal("widening does not carry its reason")
	}
	if !strings.Contains(runtime, `report.reason = "unknown_form"`) {
		t.Fatal("unknown form does not carry its refusal reason")
	}
}
