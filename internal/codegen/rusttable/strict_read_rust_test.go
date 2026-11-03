package rusttable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE STRICT READ'S ONE TYPED REASON (docs/SPEC-TABLES.md §6.5, §11):
// cfg_read_strict answers the value for a clean wire and, for every other
// wire, the one reason TableReport::err decides, in the precedence the
// reference legs use. The wires are the conformance corpus's own two
// generations of Cfg, read by the first: its own wire reads, the second
// generation's clamps and kind-mismatches, a cut id table is damage, and a
// form byte this read does not carry is unknown_form whichever form it is.
// The tolerant cfg_load still reads the clamped and kind-mismatched wires, so
// the probe also holds the strict read apart from it.
func TestRustStrictReadReason(t *testing.T) {
	slowtest.Gate(t, "rustc (strict read reason probe)")
	keyedRequireRustc(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := generateUnit(t, repo, []string{"test/tables/V1.schema"})
	wire := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	own := wire("v1_cfg.bin")
	cut := append([]byte(nil), own[:len(own)-1]...)
	newer := append([]byte{9}, own[1:]...)
	message := append([]byte{2}, own[1:]...)
	cases := []struct {
		name     string
		wire     []byte
		reason   string
		tolerant bool
	}{
		{"own", own, "ok", true},
		{"clamped", wire("v2_cfg.bin"), "clamped", true},
		{"kind_mismatch", wire("v2_seams.bin"), "kind_mismatch", true},
		{"cut_id_table", cut, "wire_damaged", false},
		{"empty", nil, "wire_damaged", false},
		{"newer_form", newer, "unknown_form", false},
		{"message_form", message, "unknown_form", false},
	}
	var probe strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&probe, `{let wire: Vec<u8> = vec!%s;let mut report = TableReport::default();let strict = match cfg_read_strict(&wire, &mut report) { Ok(_) => "ok", Err(why) => why.name() };assert_eq!(strict, %q, "%s strict {:?}", report);assert_eq!(report.err().map_or("ok", |why| why.name()), strict, "%s err {:?}", report);let mut tolerant = TableReport::default();assert_eq!(cfg_load(&wire, &mut tolerant).is_some(), %v, "%s tolerant {:?}", tolerant);}`+"\n",
			rustByteVector(c.wire), c.reason, c.name, c.name, c.tolerant, c.name)
	}
	if out, err := rustConstructProbe(t, u, probe.String()); err != nil {
		t.Fatalf("native: %v\n%s", err, out)
	}
}
