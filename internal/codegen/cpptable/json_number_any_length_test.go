package cpptable

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const cppJsonNumberAnyLengthSchema = `package countcheck
fixed table ProfileConfig
{
    experience  uint32 = 0
    precision   float64
}
`

func TestCPPJsonNumberAnyLength(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (c++)")
	t.Parallel()
	u := loadUnitFromSource(t, cppJsonNumberAnyLengthSchema)
	model := tabletext.NewModel(u)
	zeros := func(n int) string { return strings.Repeat("0", n) }
	rows := []struct {
		name  string
		key   string
		token string
	}{
		{name: "experience-509", key: "experience", token: "1." + zeros(509)},
		{name: "experience-510", key: "experience", token: "1." + zeros(510)},
		{name: "experience-511", key: "experience", token: "1." + zeros(511)},
		{name: "experience-600", key: "experience", token: "1." + zeros(600)},
		{name: "precision-600", key: "precision", token: "1." + zeros(600)},
		{name: "unknown-5000", key: "other", token: "1" + zeros(4999)},
		{name: "experience-600-digits", key: "experience", token: "1" + zeros(599)},
	}
	var body strings.Builder
	body.WriteString(`
    int failures = 0;
    #define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`)
	for _, row := range rows {
		text := fmt.Sprintf(`{"%s":%s}`, row.key, row.token)
		inst := model.New(model.Lookup("ProfileConfig"))
		var report tabletext.Report
		ok := model.Read(inst, []byte(text), &report)
		if ok == report.Refused {
			t.Fatalf("%s: oracle is neither a read nor a refusal: ok=%v report=%+v", row.name, ok, report)
		}
		experience, _ := inst.FieldByKey("experience")
		precision, _ := inst.FieldByKey("precision")
		wantExperience := experience.Cell.U
		wantPrecision := precision.Cell.F
		fmt.Fprintf(&body, `
    {
        static const uint8_t text[] = { %s };
        ProfileConfig value; ProfileConfigReset(value);
        TableReport report;
        bool ok = ProfileConfigFromJson(value, (const char *)text, (int64_t)sizeof(text), &report);
        printf("NUMBER_CASE %s: %%s experience=%%llu precision=%%.17g unknown=%%d\n",
            ok ? "read" : "refused", (unsigned long long)value.experience, value.precision, report.unknown);
        CHECK(ok == %s, "%s verdict");
        CHECK(report.unknown == %d, "%s unknown");
        CHECK(value.experience == %dULL, "%s experience");
        CHECK(value.precision == %s, "%s precision");
        if (!%s) { CHECK(report.refused, "%s refusal"); }
    }
`, cppBytes([]byte(text)), row.name,
			strconv.FormatBool(ok), row.name,
			report.Unknown, row.name,
			wantExperience, row.name,
			strconv.FormatFloat(wantPrecision, 'g', 17, 64), row.name,
			strconv.FormatBool(ok), row.name)
	}
	body.WriteString("    if (failures != 0) return 1;\n")
	out, err := runCPPCountRefusalProbe(t, u, body.String())
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("number-any-length probe: %v\n%s", err, out)
	}
}
