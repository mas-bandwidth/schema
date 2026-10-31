package luautable_test

// The Luau JSON leg reads and writes a timestamp exactly as docs/SPEC-TABLES.md
// §2.11 and §16.2 say: the text is RFC 3339 §5.6's date-time in its one
// canonical UTC spelling, and a text the strict parser refuses is refused by
// name, `timestamp_not_rfc3339: <field>`. The rows below are the accept and
// refuse rows of internal/rfc3339/rfc3339_test.go's TestStrictRFC3339, copied
// here so the Luau leg is held to every spelling and not to a sample. Every
// expected verdict, refusal name and written byte comes from schema-go's own
// engine (internal/tabletext) over the same unit, never typed by hand.

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const jsonTimestampSchema = `package probe

table Root
{
    at timestamp
}
`

type jsonTimestampCase struct {
	name  string
	text  string
	nanos int64
	ok    bool
}

// jsonTimestampCases is TestStrictRFC3339's table in
// internal/rfc3339/rfc3339_test.go, accepted rows first and refused rows after,
// joined into one slice.
func jsonTimestampCases() []jsonTimestampCase {
	utc := func(y int, mo time.Month, d, h, mi, s, ns int) int64 {
		return time.Date(y, mo, d, h, mi, s, ns, time.UTC).UnixNano()
	}
	accepted := []jsonTimestampCase{
		// the four-digit date, month 01-12 and a day valid for the month and year
		{"epoch", "1970-01-01T00:00:00Z", 0, true},
		{"leap day", "2024-02-29T12:34:05Z", utc(2024, time.February, 29, 12, 34, 5, 0), true},
		{"400-year leap day", "2000-02-29T00:00:00Z", utc(2000, time.February, 29, 0, 0, 0, 0), true},
		{"pre-epoch", "1969-12-31T23:59:59.999999999Z", -1, true},

		// T or t, and Z or z (§5.6's note)
		{"lowercase t and z", "2026-10-17t12:34:05z", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"lowercase with fraction", "2026-10-17t12:34:05.5z", utc(2026, time.October, 17, 12, 34, 5, 500000000), true},

		// a fraction of one or more digits after a dot, truncated to nanoseconds
		{"one fraction digit", "2026-10-17T12:34:05.5Z", utc(2026, time.October, 17, 12, 34, 5, 500000000), true},
		{"nine fraction digits", "2026-10-17T12:34:05.123456789Z", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"fraction truncated", "2026-10-17T12:34:05.1234567899Z", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"fraction longer than a scratch buffer", "2026-10-17T12:34:05." + strings.Repeat("1234567890", 10) + "Z", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"zero fraction", "2026-10-17T12:34:05.0Z", utc(2026, time.October, 17, 12, 34, 5, 0), true},

		// an offset of hours 00-23 and minutes 00-59, both signs and the ends
		{"positive offset", "2026-10-17T18:04:05.123456789+05:30", utc(2026, time.October, 17, 12, 34, 5, 123456789), true},
		{"negative offset", "2026-10-17T05:34:05-07:00", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"offset hour 23", "2026-10-18T12:33:05+23:59", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"negative offset hour 23", "2026-10-16T12:35:05-23:59", utc(2026, time.October, 17, 12, 34, 5, 0), true},
		{"unknown local offset", "1970-01-01T00:00:00-00:00", 0, true},

		// the ends of Time.UnixNano's domain are counts and are accepted
		{"UnixNano minimum", "1677-09-21T00:12:43.145224192Z", math.MinInt64, true},
		{"UnixNano maximum", "2262-04-11T23:47:16.854775807Z", math.MaxInt64, true},
	}
	refused := []jsonTimestampCase{
		// the four spellings of the red: time.Parse's hour 24, minute 60 and
		// comma fraction are not RFC 3339, and lowercase t/z are
		{"offset hour 24", "2026-10-17T12:34:05+24:00", 0, false},
		{"offset minute 60", "2026-10-17T12:34:05+05:60", 0, false},
		{"comma fraction", "2026-10-17T12:34:05,5Z", 0, false},
		{"negative offset hour 24", "2026-10-17T12:34:05-24:00", 0, false},
		{"negative offset minute 60", "2026-10-17T12:34:05-05:60", 0, false},

		// second 60 is a leap second this kind refuses (§2.11)
		{"leap second", "2016-12-31T23:59:60Z", 0, false},
		{"leap second with fraction", "2016-12-31T23:59:60.5Z", 0, false},

		// the date: month 01-12 and a day valid for that month and year
		{"month zero", "2026-00-17T12:34:05Z", 0, false},
		{"month thirteen", "2026-13-17T12:34:05Z", 0, false},
		{"day zero", "2026-10-00T12:34:05Z", 0, false},
		{"day thirty-two", "2026-10-32T12:34:05Z", 0, false},
		{"february thirty", "2026-02-30T12:34:05Z", 0, false},
		{"april thirty-one", "2026-04-31T12:34:05Z", 0, false},
		{"non-leap february twenty-nine", "2025-02-29T12:34:05Z", 0, false},
		{"century non-leap february twenty-nine", "1900-02-29T12:34:05Z", 0, false},

		// the time: hour 00-23, minute 00-59, second 00-59
		{"hour 24", "2026-10-17T24:00:00Z", 0, false},
		{"minute 60", "2026-10-17T12:60:05Z", 0, false},

		// the fraction is a dot and at least one digit
		{"empty fraction", "2026-10-17T12:34:05.Z", 0, false},
		{"two dots", "2026-10-17T12:34:05.1.2Z", 0, false},
		{"comma with digits", "2026-10-17T12:34:05,123Z", 0, false},

		// the offset is Z, z or sign HH:MM with both fields in range
		{"missing offset", "2026-10-17T12:34:05", 0, false},
		{"offset without colon", "2026-10-17T12:34:05+0530", 0, false},
		{"offset hour only", "2026-10-17T12:34:05+05", 0, false},
		{"offset minute only", "2026-10-17T12:34:05+05:30:00", 0, false},

		// nothing before or after
		{"empty", "", 0, false},
		{"date only", "2026-10-17", 0, false},
		{"missing seconds", "2026-10-17T12:34Z", 0, false},
		{"leading space", " 2026-10-17T12:34:05Z", 0, false},
		{"trailing space", "2026-10-17T12:34:05Z ", 0, false},
		{"trailing newline", "2026-10-17T12:34:05Z\n", 0, false},
		{"trailing character", "2026-10-17T12:34:05Zx", 0, false},
		{"leading character", "x2026-10-17T12:34:05Z", 0, false},
		{"space for T", "2026-10-17 12:34:05Z", 0, false},
		{"non-digit year", "20x6-10-17T12:34:05Z", 0, false},
		{"non-digit hour", "2026-10-17T1x:34:05Z", 0, false},

		// an instant outside Time.UnixNano's domain names no count
		{"year zero outside the domain", "0000-01-01T00:00:00Z", 0, false},
		{"year 9999 outside the domain", "9999-12-31T23:59:59Z", 0, false},
		{"one nanosecond below the minimum", "1677-09-21T00:12:43.145224191Z", 0, false},
		{"one nanosecond above the maximum", "2262-04-11T23:47:16.854775808Z", 0, false},
	}
	return append(accepted, refused...)
}

type jsonTimestampResult struct {
	malformed bool
	reason    string
	text      string
}

// runJsonTimestampProbe reads one text under the emitted json runtime and prints
// the report's verdict and reason, then the hex of the text ToJson writes from
// the read value (docs/SPEC-TABLES.md §16.1-§16.3).
func runJsonTimestampProbe(t *testing.T, g *generatedUnit, text string) jsonTimestampResult {
	t.Helper()
	const probe = `local json = require("./schema_json")
json.SetFlags(SCHEMA_JSON_FLAGS)
local inputs = require("./inputs")
local function hex(b)
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function report()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local r = report()
local v = json.FromJson("Root", inputs.text, r)
print("malformed=" .. tostring(r.malformed))
print("reason=" .. tostring(r.reason or ""))
if v == nil then print("text=nil"); return end
print("text=" .. hex(json.ToJson("Root", v)))
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(jsonModules(g)+jsonFlags(g)+probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": []byte(text)}))
	got := jsonTimestampResult{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "malformed":
			got.malformed = value == "true"
		case "reason":
			got.reason = value
		case "text":
			got.text = value
		}
	}
	return got
}

func TestLuauJsonTimestampStrictRFC3339(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{"Stamp.schema": jsonTimestampSchema})

	// The Go reference unit is the same schema compiled by the same compiler,
	// read and written by schema-go's own engine.
	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "Stamp.schema")
	if err := os.WriteFile(schemaPath, []byte(jsonTimestampSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("the Go reference unit does not load: %v", err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("Root")
	if st == nil {
		t.Fatal("the Go reference unit has no Root table")
	}

	for _, tc := range jsonTimestampCases() {
		text := `{"at":` + strconv.Quote(tc.text) + `}`
		inst := model.New(st)
		var rep tabletext.Report
		goOK := model.Read(inst, []byte(text), &rep)
		if goOK != tc.ok {
			t.Fatalf("%s: the Go reference accepted=%t, want %t (%+v)", tc.name, goOK, tc.ok, rep)
		}
		wantText := ""
		if goOK {
			body, err := model.Write(inst)
			if err != nil {
				t.Fatalf("%s: the Go reference ToJson: %v", tc.name, err)
			}
			wantText = hexBytes(append(body, '\n'))
		} else if !rep.Malformed || !strings.HasPrefix(rep.Reason, "timestamp_not_rfc3339: at") {
			t.Fatalf("%s: the Go reference refused as %+v, not timestamp_not_rfc3339: at", tc.name, rep)
		}

		got := runJsonTimestampProbe(t, g, text)
		if got.malformed != !tc.ok {
			t.Errorf("%s: Luau FromJson malformed=%t, want %t (reason %q)", tc.name, got.malformed, !tc.ok, got.reason)
			continue
		}
		if !tc.ok {
			if want := rep.Reason; got.reason != want {
				t.Errorf("%s: Luau refused as %q, want %q", tc.name, got.reason, want)
			}
			continue
		}
		if got.reason != "" {
			t.Errorf("%s: Luau reports %q on an accepted text", tc.name, got.reason)
		}
		if got.text != wantText {
			t.Errorf("%s: Luau ToJson is %s, want %s", tc.name, got.text, wantText)
		}
	}
}
