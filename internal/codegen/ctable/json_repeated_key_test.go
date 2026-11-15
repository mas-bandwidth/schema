// A repeated key refuses the JSON read in every object of the text
// (docs/SPEC-TABLES.md §16.2, §16.12): a known field's key in a table of any
// width, an unknown key, and a key inside a value the reader only skips, at
// any depth.
package ctable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestCJsonRepeatedKeyEveryObject compares the C reader's verdict (read or
// refused) on each text with the compiler's own engine's verdict on the same
// text, and with the verdict the rule names. Only the verdict is compared;
// which reason a refusal names is per-language detail.
func TestCJsonRepeatedKeyEveryObject(t *testing.T) {
	t.Parallel()
	const wideFields = 600
	const repeatedField = 549 // the 550th field
	var schema strings.Builder
	schema.WriteString("package probe\n\nfixed table Small\n{\n    name string(8)\n    count int32\n}\n\nfixed table Wide\n{\n")
	for i := 0; i < wideFields; i++ {
		fmt.Fprintf(&schema, "    f%d uint8\n", i)
	}
	schema.WriteString("}\n")

	wideText := func(repeat int) string {
		var b strings.Builder
		b.WriteString("{")
		for i := 0; i < wideFields; i++ {
			fmt.Fprintf(&b, "\"f%d\":%d,", i, i%200)
		}
		if repeat >= 0 {
			fmt.Fprintf(&b, "\"f%d\":%d,", repeat, 7)
		}
		return strings.TrimSuffix(b.String(), ",") + "}"
	}

	type row struct {
		name    string
		table   string
		text    string
		refused bool
	}
	rows := []row{
		{"unknown-key-repeated", "Small", `{"zz":1,"zz":2}`, true},
		{"empty-key-repeated", "Small", `{"":1,"":2}`, true},
		{"proto-key-repeated", "Small", `{"__proto__":1,"__proto__":2}`, true},
		{"skipped-object-key-repeated", "Small", `{"zz":{"a":1,"a":2}}`, true},
		{"skipped-array-object-key-repeated", "Small", `{"zz":[1,{"b":[{"a":1,"a":2}]}]}`, true},
		{"escaped-spelling-repeated", "Small", `{"zz":1,"\u007a\u007a":2}`, true},
		{"wide-field-repeated", "Wide", wideText(repeatedField), true},
		{"control-distinct-unknown-keys", "Small", `{"zz":1,"yy":2,"":3,"__proto__":4}`, false},
		{"control-same-key-two-objects", "Small", `{"zz":{"a":1},"yy":{"a":2},"xx":[{"a":1},{"a":2}]}`, false},
		{"control-known-keys-once", "Small", `{"name":"a","count":3,"zz":{"name":1,"count":2}}`, false},
		{"control-wide-every-field-once", "Wide", wideText(-1), false},
	}

	u := unitFrom(t, schema.String())
	m := tabletext.NewModel(u)
	for _, r := range rows {
		inst := m.New(m.Lookup(r.table))
		var report tabletext.Report
		engineRefused := !m.Read(inst, []byte(r.text), &report)
		if engineRefused != r.refused {
			t.Fatalf("%s: the engine's verdict is refused=%v, the rule's is refused=%v", r.name, engineRefused, r.refused)
		}
	}

	var probe strings.Builder
	probe.WriteString(`#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
static Wide wide_value;
static int small_refused(const char * text)
{
    Small value;
    TableReport report;
    int ok;
    memset(&report, 0, sizeof(report));
    small_reset(&value);
    ok = small_from_json(&value, text, (int64_t) strlen(text), &report);
    return !ok || report.refused;
}
static int wide_refused(const char * text)
{
    TableReport report;
    int ok;
    memset(&report, 0, sizeof(report));
    wide_reset(&wide_value);
    ok = wide_from_json(&wide_value, text, (int64_t) strlen(text), &report);
    return !ok || report.refused;
}
int main(void)
{
    int failed = 0;
`)
	for _, r := range rows {
		call := "small_refused"
		if r.table == "Wide" {
			call = "wide_refused"
		}
		want := 0
		if r.refused {
			want = 1
		}
		fmt.Fprintf(&probe, "    if (%s(%s) != %d) { fprintf(stderr, \"%s: C verdict refused=%%d, engine refused=%d\\n\", %d ^ 1); failed = 1; }\n",
			call, cStringLiteral(r.text), want, r.name, want, want)
	}
	probe.WriteString("    return failed;\n}\n")
	runCHostileResidual(t, schema.String(), probe.String())
}

// cStringLiteral spells s as a C string literal, byte for byte.
func cStringLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
