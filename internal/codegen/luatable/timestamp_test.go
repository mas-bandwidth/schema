package luatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/rfc3339"
)

// luaTimestampSchema is the unit whose text form this test drives: one
// timestamp field, so the Lua reader is J.FromJson and the writer J.ToJson.
const luaTimestampSchema = `package probe
fixed table Root
{
    at timestamp
}
`

// luaTimestampCases is the table internal/rfc3339's TestStrictRFC3339 pins,
// repeated here so the Lua text form is held to every spelling and not to a
// sample (docs/SPEC-TABLES.md §2.11, §16.2).
func luaTimestampCases() []struct {
	name string
	text string
	ok   bool
} {
	return []struct {
		name string
		text string
		ok   bool
	}{
		// the four-digit date, month 01-12 and a day valid for the month and year
		{"epoch", "1970-01-01T00:00:00Z", true},
		{"leap day", "2024-02-29T12:34:05Z", true},
		{"400-year leap day", "2000-02-29T00:00:00Z", true},
		{"pre-epoch", "1969-12-31T23:59:59.999999999Z", true},

		// T or t, and Z or z (§5.6's note)
		{"lowercase t and z", "2026-10-17t12:34:05z", true},
		{"lowercase with fraction", "2026-10-17t12:34:05.5z", true},

		// a fraction of one or more digits after a dot, truncated to nanoseconds
		{"one fraction digit", "2026-10-17T12:34:05.5Z", true},
		{"nine fraction digits", "2026-10-17T12:34:05.123456789Z", true},
		{"fraction truncated", "2026-10-17T12:34:05.1234567899Z", true},
		{"fraction longer than a scratch buffer", "2026-10-17T12:34:05." + strings.Repeat("1234567890", 10) + "Z", true},
		{"zero fraction", "2026-10-17T12:34:05.0Z", true},

		// an offset of hours 00-23 and minutes 00-59, both signs and the ends
		{"positive offset", "2026-10-17T18:04:05.123456789+05:30", true},
		{"negative offset", "2026-10-17T05:34:05-07:00", true},
		{"offset hour 23", "2026-10-18T12:33:05+23:59", true},
		{"negative offset hour 23", "2026-10-16T12:35:05-23:59", true},
		{"unknown local offset", "1970-01-01T00:00:00-00:00", true},

		// the ends of Time.UnixNano's domain are counts and are accepted
		{"UnixNano minimum", "1677-09-21T00:12:43.145224192Z", true},
		{"UnixNano maximum", "2262-04-11T23:47:16.854775807Z", true},

		// the four spellings of the red: time.Parse's hour 24, minute 60 and
		// comma fraction are not RFC 3339, and lowercase t/z are
		{"offset hour 24", "2026-10-17T12:34:05+24:00", false},
		{"offset minute 60", "2026-10-17T12:34:05+05:60", false},
		{"comma fraction", "2026-10-17T12:34:05,5Z", false},
		{"negative offset hour 24", "2026-10-17T12:34:05-24:00", false},
		{"negative offset minute 60", "2026-10-17T12:34:05-05:60", false},

		// second 60 is a leap second this kind refuses (§2.11)
		{"leap second", "2016-12-31T23:59:60Z", false},
		{"leap second with fraction", "2016-12-31T23:59:60.5Z", false},

		// the date: month 01-12 and a day valid for that month and year
		{"month zero", "2026-00-17T12:34:05Z", false},
		{"month thirteen", "2026-13-17T12:34:05Z", false},
		{"day zero", "2026-10-00T12:34:05Z", false},
		{"day thirty-two", "2026-10-32T12:34:05Z", false},
		{"february thirty", "2026-02-30T12:34:05Z", false},
		{"april thirty-one", "2026-04-31T12:34:05Z", false},
		{"non-leap february twenty-nine", "2025-02-29T12:34:05Z", false},
		{"century non-leap february twenty-nine", "1900-02-29T12:34:05Z", false},

		// the time: hour 00-23, minute 00-59, second 00-59
		{"hour 24", "2026-10-17T24:00:00Z", false},
		{"minute 60", "2026-10-17T12:60:05Z", false},

		// the fraction is a dot and at least one digit
		{"empty fraction", "2026-10-17T12:34:05.Z", false},
		{"two dots", "2026-10-17T12:34:05.1.2Z", false},
		{"comma with digits", "2026-10-17T12:34:05,123Z", false},

		// the offset is Z, z or sign HH:MM with both fields in range
		{"missing offset", "2026-10-17T12:34:05", false},
		{"offset without colon", "2026-10-17T12:34:05+0530", false},
		{"offset hour only", "2026-10-17T12:34:05+05", false},
		{"offset minute only", "2026-10-17T12:34:05+05:30:00", false},

		// nothing before or after
		{"empty", "", false},
		{"date only", "2026-10-17", false},
		{"missing seconds", "2026-10-17T12:34Z", false},
		{"leading space", " 2026-10-17T12:34:05Z", false},
		{"trailing space", "2026-10-17T12:34:05Z ", false},
		{"trailing newline", "2026-10-17T12:34:05Z\n", false},
		{"trailing character", "2026-10-17T12:34:05Zx", false},
		{"leading character", "x2026-10-17T12:34:05Z", false},
		{"space for T", "2026-10-17 12:34:05Z", false},
		{"non-digit year", "20x6-10-17T12:34:05Z", false},
		{"non-digit hour", "2026-10-17T1x:34:05Z", false},

		// an instant outside Time.UnixNano's domain names no count
		{"year zero outside the domain", "0000-01-01T00:00:00Z", false},
		{"year 9999 outside the domain", "9999-12-31T23:59:59Z", false},
		{"one nanosecond below the minimum", "1677-09-21T00:12:43.145224191Z", false},
		{"one nanosecond above the maximum", "2262-04-11T23:47:16.854775808Z", false},
	}
}

// TestLuaTimestampTextIsStrictRFC3339 drives the Lua text form over the same
// table: every accepted spelling lands the same UTC nanosecond count as
// internal/rfc3339 and writes back the generated Go writer's text, and every
// refused spelling is refused by name, timestamp_not_rfc3339: at
// (docs/SPEC-TABLES.md §2.11, §16.2).
func TestLuaTimestampTextIsStrictRFC3339(t *testing.T) {
	t.Parallel()
	cases := luaTimestampCases()

	work := t.TempDir()
	schema := filepath.Join(work, "probe.schema")
	if err := os.WriteFile(schema, []byte(luaTimestampSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)

	var probe strings.Builder
	probe.WriteString(`local T = require("` + ModuleName(u) + `")
local J = require("json")
local function hex(s)
  local out = {}
  for i = 1, #s do out[i] = string.format("%02x", s:byte(i)) end
  return table.concat(out)
end
local cases = {
`)
	for _, c := range cases {
		fmt.Fprintf(&probe, "\t{%q, %q},\n", c.name, `{"at":`+strconv.Quote(c.text)+`}`)
	}
	probe.WriteString(`}
for _, c in ipairs(cases) do
  local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
  local v = J.FromJsonTolerant(T, "Root", c[2], report)
  if v == nil or report.malformed then
    print(c[1] .. "\trefused\t" .. tostring(report.Reason or "-"))
  else
    local text = J.ToJson(T, "Root", v)
    print(c[1] .. "\tread\t" .. tostring(v.at) .. "\t" .. hex(text or ""))
  end
end
`)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) != len(cases) {
		t.Fatalf("probe printed %d lines, want %d:\n%s", len(lines), len(cases), out)
	}
	for i, c := range cases {
		fields := strings.Split(lines[i], "\t")
		if len(fields) < 3 || fields[0] != c.name {
			t.Fatalf("case %s: line %q", c.name, lines[i])
		}
		nanos, ok := rfc3339.Parse([]byte(c.text))
		if ok != c.ok {
			t.Fatalf("case %s: the oracle says accepted=%t, the table says %t", c.name, ok, c.ok)
		}
		if !c.ok {
			if fields[1] != "refused" {
				t.Errorf("case %s: %q was not refused: %q", c.name, c.text, lines[i])
				continue
			}
			if want := "timestamp_not_rfc3339: at"; fields[2] != want {
				t.Errorf("case %s: reason %q, want %q", c.name, fields[2], want)
			}
			continue
		}
		if fields[1] != "read" || len(fields) != 4 {
			t.Errorf("case %s: %q was not read: %q", c.name, c.text, lines[i])
			continue
		}
		if fields[2] != strconv.FormatInt(nanos, 10) {
			t.Errorf("case %s: landed %s, want %d", c.name, fields[2], nanos)
		}
		text, err := hex.DecodeString(fields[3])
		if err != nil {
			t.Errorf("case %s: bad hex %q: %v", c.name, fields[3], err)
			continue
		}
		// THE GENERATED GO WRITER'S TEXT (internal/codegen/gotable/json.go,
		// tableJsonWriteTimestamp): time.Unix(0, count).UTC() in RFC3339Nano,
		// one key per line in the fixed table's object.
		want := "{\n  \"at\": " + strconv.Quote(time.Unix(0, nanos).UTC().Format(time.RFC3339Nano)) + "\n}\n"
		if got := string(text); got != want {
			t.Errorf("case %s: ToJson %q, want %q", c.name, got, want)
		}
	}
}
