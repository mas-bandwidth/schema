package rusttable

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func TestRustMessageNestedFixedDefault(t *testing.T) {
	t.Parallel()
	messageBatchRustc(t)

	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile("../../../" + path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	u := unitFrom(t, string(read("tables/scalars/Scalars.schema")))
	model := tabletext.NewModel(u)
	cases := []struct {
		name, text, checks string
	}{
		{
			"explicit_zero",
			string(read("testdata/conformance/tables/json/scalars_uint128_max.json")),
			`v.spawn_present && v.spawn.x == 0 && v.entity_id.0 == u128::MAX
                && v.seeds_count == 2 && v.seeds[0].0 == 0 && v.seeds[1].0 == u128::MAX`,
		},
		{"omitted_x", `{"spawn":{}}`, `v.spawn_present && v.spawn.x == 32768`},
	}
	var body strings.Builder
	for _, c := range cases {
		inst := model.New(model.Lookup("SimState"))
		var rep tabletext.Report
		if !model.Read(inst, []byte(c.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: read: %+v", c.name, rep)
		}
		wire, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		fmt.Fprintf(&body, `
    {
        let want = [%s];
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        assert!(announce_read(&mut vocab, &TABLE_ANNOUNCEMENT, &mut TableReport::default()));
        let mut values = [sim_state_message_default()];
        let mut report = TableReport::default();
        let (count, ok) = sim_state_load_messages(&mut values, &vocab, &want, &mut report);
        let v = &values[0];
        let mut bytes = vec![0; want.len()];
        let mut saved = TableReport::default();
        let n = sim_state_save_messages(&values, &mut bytes, &mut saved);
        say(%q, ok && count == 1 && !report.damaged && !report.refused && report.err().is_none()
            && (%s) && n == want.len() as i64 && bytes == want,
            format!("count={count}, ok={ok}, report={report:?}, spawn.x={}, encoded={n}, saved={saved:?}", v.spawn.x));
    }
`, rustBytes(wire), c.name, c.checks)
	}
	out := runMessageBatchProbe(t, u, body.String())
	for _, c := range cases {
		if line := messageBatchLine(out, c.name); line != "BATCH "+c.name+" ok" {
			t.Errorf("%s: Rust message round trip: %s", c.name, line)
		}
	}
}
