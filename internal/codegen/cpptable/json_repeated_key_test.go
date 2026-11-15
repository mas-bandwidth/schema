package cpptable

// A REPEATED KEY REFUSES IN EVERY OBJECT OF THE TEXT (docs/SPEC-TABLES.md
// §16.2, §16.12): a known field's key, a key that names no field, and a key
// inside a value the reader only skips, at any depth. Each row is compared on
// the verdict alone with the Go engine's verdict on the same text.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestCPPJsonRepeatedKeyEveryObject(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	t.Parallel()
	u := loadHeldUnitPaths(t, "../../..", []string{"tables/examples/Tables.schema"})
	model := tabletext.NewModel(u)
	// An object's key set spans its whole text, not a fixed number of keys: a
	// repeat past many distinct keys still refuses.
	var many strings.Builder
	many.WriteByte('{')
	for i := range 513 {
		fmt.Fprintf(&many, `"zz%d":1,`, i)
	}
	many.WriteString(`"zz512":2}`)
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "known-field-repeated", text: `{"experience":1,"experience":2}`, want: false},
		{name: "unknown-repeated", text: `{"zz":1,"zz":2}`, want: false},
		{name: "empty-key-repeated", text: `{"":1,"":2}`, want: false},
		{name: "reserved-spelling-repeated", text: `{"__proto__":1,"__proto__":2}`, want: false},
		{name: "nested-value-repeated", text: `{"zz":{"a":1,"a":2}}`, want: false},
		{name: "escaped-spelling-repeated", text: `{"zz":1,"\u007a\u007a":2}`, want: false},
		{name: "array-object-repeated", text: `{"zz":[{"a":1,"a":2}]}`, want: false},
		{name: "unknown-repeated-past-many-distinct-keys", text: many.String(), want: false},
		{name: "distinct-unknown-keys", text: `{"zz":1,"yy":2}`, want: true},
		{name: "same-key-two-objects", text: `{"zz":{"a":1},"yy":{"a":2}}`, want: true},
		{name: "same-key-outer-and-nested", text: `{"zz":{"zz":{"zz":1}}}`, want: true},
		{name: "same-key-outer-and-array-object", text: `{"zz":[{"zz":1},{"zz":2}]}`, want: true},
	}
	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	for _, tc := range cases {
		var rep tabletext.Report
		got := model.Read(model.New(model.Lookup("ProfileConfig")), []byte(tc.text), &rep)
		if got != tc.want || rep.Refused != !tc.want {
			t.Fatalf("%s: Go engine verdict ok=%v report=%+v, want read=%v", tc.name, got, rep, tc.want)
		}
		fmt.Fprintf(&body, `
    {
        static const uint8_t text[] = { %s };
        ProfileConfig value; TableReport report;
        bool ok = ProfileConfigFromJson(value, (const char *)text, (int64_t)sizeof(text), &report);
        printf("REPEAT_KEY %s: %%s\n", ok ? "read" : "refused");
        CHECK(ok == %t, "%s");
    }
`, cppBytes([]byte(tc.text)), tc.name, tc.want, tc.name)
	}
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("repeated-key probe: %v\n%s", err, out)
	}
}
