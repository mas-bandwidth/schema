package cstable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE STRICT READ'S ONE TYPED REASON (docs/SPEC-TABLES.md §6.5, §11):
// CfgReadStrict answers true for a clean wire and, for every other wire, false
// and the one reason TableReport.Err decides, in the precedence the reference
// legs use. The wires are the conformance corpus's own two generations of Cfg,
// read by the first: its own wire reads, the second generation's clamps and
// kind-mismatches, a cut id table and an empty wire are damage, and a form byte
// this read does not carry is unknown_form whichever form it is. The tolerant
// CfgLoad still reads the clamped and kind-mismatched wires, so the probe also
// holds the strict read apart from it, and a strict refusal lands nothing.
func TestCSharpStrictReadReason(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK (strict read reason probe)")
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(repo, "test/tables/V1.schema"))
	if err != nil {
		t.Fatal(err)
	}
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
	var body strings.Builder
	body.WriteString(`void Check(string name, string hex, Tblv1.TableRefuseReason want, bool tolerant) {
 byte[] bytes = Convert.FromHexString(hex);
 var value = new Tblv1.Cfg(); var report = new Tblv1.TableReport();
 bool ok = Tblv1.Schema.CfgReadStrict(value, bytes, report, out Tblv1.TableRefuseReason why);
 if (ok != (want == Tblv1.TableRefuseReason.ok) || why != want) throw new Exception(name + " strict " + why);
 if (report.Err() != why && !(why == Tblv1.TableRefuseReason.wire_damaged && report.Err() == Tblv1.TableRefuseReason.ok)) throw new Exception(name + " err " + report.Err());
 if (!ok) { var fresh = new Tblv1.Cfg(); byte[] a = new byte[Tblv1.Schema.CfgMeasure(value)]; byte[] b = new byte[Tblv1.Schema.CfgMeasure(fresh)]; Tblv1.Schema.CfgSave(value, a); Tblv1.Schema.CfgSave(fresh, b); if (!a.AsSpan().SequenceEqual(b)) throw new Exception(name + " strict refusal landed a value"); }
 if (Tblv1.Schema.CfgLoad(new Tblv1.Cfg(), bytes, new Tblv1.TableReport()) != tolerant) throw new Exception(name + " tolerant");
}
`)
	for _, c := range cases {
		fmt.Fprintf(&body, "Check(%q, %q, Tblv1.TableRefuseReason.%s, %v);\n", c.name, hex.EncodeToString(c.wire), c.reason, c.tolerant)
	}
	t15RunCS(t, string(schema), body.String())
}
