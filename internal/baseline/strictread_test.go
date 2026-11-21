package baseline_test

// Every tightening warning says what the STRICT reader does with a file
// written before the edit, and that is pinned here against a real read: the
// old schema writes a saved file holding a value the new schema no longer
// admits, the new schema reads it, and the warning must name the refusal the
// read gave. A warning that promised a clamp, a truncated value, a kept prefix
// or a skipped entry would fail here, because the reader does none of those:
// it refuses the whole load (docs/SPEC-TABLES.md §4).

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestTighteningWarningsDescribeTheStrictRead(t *testing.T) {
	const tmpl = "package fixture\n\ntable Save\n{\n    %s\n}\n"
	cases := []struct {
		name, before, after, json, token string
	}{
		{"a maximum lowered", "x int32 | min = -100, max = 100", "x int32 | min = -100, max = 50", `{"x": 75}`, "maximum"},
		{"a minimum raised", "x int32 | min = -100, max = 100", "x int32 | min = -50, max = 100", `{"x": -75}`, "minimum"},
		{"an array bound lowered", "xs [..4]int32", "xs [..2]int32", `{"xs": [1, 2, 3, 4]}`, "array bound"},
		{"a string capacity lowered", "name string(8)", "name string(2)", `{"name": "abcdef"}`, "capacity"},
		{"a map key capacity lowered", "by_name map[string(8)]int32", "by_name map[string(2)]int32", `{"by_name": {"abcdef": 1}}`, "map key capacity"},
		{"a text list total lowered", "rules [..4]string(8) | total = 8", "rules [..4]string(8) | total = 3", `{"rules": ["ab", "cd"]}`, "text list total"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := strings.Replace(tmpl, "%s", c.before, 1)
			after := strings.Replace(tmpl, "%s", c.after, 1)

			// the saved file, written by the OLD schema
			old := unit(t, before)
			om := tabletext.NewModel(old)
			oi := om.New(old.Tables["Save"])
			var rr tabletext.Report
			if !om.Read(oi, []byte(c.json), &rr) {
				t.Fatalf("the old schema does not take the fixture %s: %+v", c.json, rr)
			}
			saved, err := tablewire.Encode(om, oi)
			if err != nil {
				t.Fatalf("the old schema does not write the fixture: %v", err)
			}

			// read by the NEW schema
			live := unit(t, after)
			lm := tabletext.NewModel(live)
			li := lm.New(live.Tables["Save"])
			var report tabletext.Report
			ok, err := tablewire.Decode(lm, li, saved, &report)
			if ok || err == nil {
				t.Fatalf("the new schema read the old save: the fixture no longer exercises a tightening")
			}
			reason := tablewire.RefusalReason(err)
			if reason == "" {
				t.Fatalf("the read refused without a reason: %v", err)
			}

			got := baseline.Diff(committed(t, before), baseline.Render(unit(t, after)), baseline.DefaultTokenPolicy)
			warns := 0
			for _, f := range got {
				if f.Verdict != baseline.Warn || !strings.Contains(f.Where, "Save.") {
					continue
				}
				warns++
				if !strings.Contains(f.What, reason) {
					t.Errorf("the warning does not name the refusal the read gave (%s):\n  %s", reason, f.What)
				}
				for _, promise := range []string{"clamp", "truncat", "prefix", "skipped", "drops"} {
					if strings.Contains(f.What, promise) {
						t.Errorf("the warning promises a recovery (%q) the strict reader does not make:\n  %s", promise, f.What)
					}
				}
			}
			if warns == 0 || !find(got, baseline.Warn, "Save.", c.token) {
				t.Fatalf("no %s warning for the edit, got:%s", c.token, summary(got))
			}
		})
	}
}
