package javatable

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
// CfgVariable.readStrict answers the value for a clean wire and, for every
// other wire, null with the one reason Report.err decides, in the precedence
// the reference legs use. The wires are the conformance corpus's own two
// generations of Cfg, read by the first: its own wire reads, the second
// generation's clamps and kind mismatches, a cut id table and an empty wire
// are damage, and a form byte this read does not carry is unknown_form
// whichever form it is. The tolerant load still reads the clamped and
// kind-mismatched wires, so the probe also holds the strict read apart from it.
func TestJavaStrictReadReason(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	schema, err := os.ReadFile(filepath.Join(repo, "test", "tables", "V1.schema"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name))
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
	for _, c := range cases {
		fmt.Fprintf(&body, "        check(%q, HexFormat.of().parseHex(%q), %q, %v);\n", c.name, hex.EncodeToString(c.wire), c.reason, c.tolerant)
	}
	driver := `import tblv1.*;
import java.util.HexFormat;

public final class Main {
    static void check(String name, byte[] wire, String reason, boolean tolerant) {
        // a report that held another read's verdict: readStrict resets it
        TableVariable.Report report = new TableVariable.Report();
        report.clamped = 3;
        report.malformed = true;
        CfgVariable.Value v = CfgVariable.readStrict(wire, report);
        TableVariable.TableRefuseReason why = report.err();
        String strict = why == null ? "ok" : why.spelling;
        if (!strict.equals(reason)) {
            throw new AssertionError(name + ": strict read named " + strict + ", want " + reason);
        }
        if ((v != null) != (why == null)) {
            throw new AssertionError(name + ": value and reason disagree");
        }
        TableVariable.Report tolerantReport = new TableVariable.Report();
        if ((CfgVariable.load(wire, tolerantReport) != null) != tolerant) {
            throw new AssertionError(name + ": tolerant load");
        }
        System.out.println(name + " " + strict);
    }

    public static void main(String[] args) {
` + body.String() + `    }
}
`
	_, out := compileAndRunJavaProbe(t, string(schema), "tblv1", driver)
	var want strings.Builder
	for i, c := range cases {
		if i > 0 {
			want.WriteString("\n")
		}
		fmt.Fprintf(&want, "%s %s", c.name, c.reason)
	}
	if out != want.String() {
		t.Fatalf("strict read reasons:\n%s\nwant:\n%s", out, want.String())
	}
}
