package tabletext_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// §2.11, §16.2: `timestamp` is an int64 count of nanoseconds in storage and an
// RFC 3339 string in the text form, written with a Z offset and nanoseconds and
// read from any RFC 3339 spelling of the same instant. The strings below are
// the generated Go leg's own output (internal/codegen/gotable/json.go's
// tableJsonWriteTimestamp), pinned so the engine and the leg cannot drift.
func timestampUnit(t *testing.T) *tabletext.Model {
	t.Helper()
	const src = "package probe\ntable Root\n{\n    at timestamp\n}\n"
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

// timestampInstance is a fresh Root holding one count.
func timestampInstance(t *testing.T, m *tabletext.Model, st *ir.Struct, nanos int64) *tabletext.Instance {
	t.Helper()
	inst := m.New(st)
	fv, ok := inst.FieldByKey("at")
	if !ok {
		t.Fatal("Root has no field keyed at")
	}
	fv.Cell.I = nanos
	fv.Cell.U = uint64(nanos)
	return inst
}

// packTimestampFile packs one field value from a tree holding only at.json.
func packTimestampFile(t *testing.T, m *tabletext.Model, text string) ([]byte, tabletext.Report, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "at.json"), []byte(text+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wire, _, report, err := tablepack.Pack(m, "Root", dir)
	return wire, report, err
}

func TestEngineTimestampIsRFC3339Text(t *testing.T) {
	t.Parallel()
	m := timestampUnit(t)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("no table Root")
	}

	// The text the generated Go writer spells for each count, and the wire that
	// count writes. `unpack` of the wire gives the text, and `pack` of that text
	// gives the wire back byte for byte with a silent report.
	values := []struct {
		name  string
		nanos int64
		text  string
	}{
		{"zero time", 0, `"1970-01-01T00:00:00Z"`},
		{"nanoseconds", 1792240445123456789, `"2026-10-17T12:34:05.123456789Z"`},
		{"whole second", 1792240445000000000, `"2026-10-17T12:34:05Z"`},
		{"pre-1970", -1, `"1969-12-31T23:59:59.999999999Z"`},
		{"UnixNano minimum", math.MinInt64, `"1677-09-21T00:12:43.145224192Z"`},
		{"UnixNano maximum", math.MaxInt64, `"2262-04-11T23:47:16.854775807Z"`},
	}
	for _, tc := range values {
		t.Run(tc.name, func(t *testing.T) {
			inst := timestampInstance(t, m, st, tc.nanos)
			wire, err := tablewire.Encode(m, inst)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			dir := t.TempDir()
			report, err := tablepack.Unpack(m, "Root", wire, dir)
			if err != nil {
				t.Fatalf("unpack: %v", err)
			}
			if !report.Silent() {
				t.Fatalf("unpack report is not silent: %+v", report)
			}
			got, err := os.ReadFile(filepath.Join(dir, "at.json"))
			if err != nil {
				t.Fatal(err)
			}
			if text := strings.TrimSuffix(string(got), "\n"); text != tc.text {
				t.Fatalf("unpack wrote %s, want %s", text, tc.text)
			}
			back, _, report, err := tablepack.Pack(m, "Root", dir)
			if err != nil {
				t.Fatalf("pack: %v", err)
			}
			if !report.Silent() {
				t.Fatalf("pack report is not silent: %+v", report)
			}
			if !bytes.Equal(back, wire) {
				t.Fatalf("pack gave %x, want the wire %x", back, wire)
			}
		})
	}

	// §16.2: any RFC 3339 spelling of the same instant lands the same UTC count,
	// so an offset spelling packs to the Z spelling's wire.
	t.Run("offset spelling", func(t *testing.T) {
		wire, err := tablewire.Encode(m, timestampInstance(t, m, st, 1792240445123456789))
		if err != nil {
			t.Fatal(err)
		}
		back, report, err := packTimestampFile(t, m, `"2026-10-17T18:04:05.123456789+05:30"`)
		if err != nil {
			t.Fatalf("pack: %v", err)
		}
		if !report.Silent() {
			t.Fatalf("pack report is not silent: %+v", report)
		}
		if !bytes.Equal(back, wire) {
			t.Fatalf("the offset spelling gave %x, want the Z spelling's wire %x", back, wire)
		}
	})

	// §16.2: a string that does not parse, or names an instant outside
	// Time.UnixNano's domain, is refused BY NAME and the report is Malformed.
	for _, tc := range []struct{ name, text string }{
		{"impossible day", `"2026-02-30T00:00:00Z"`},
		{"past the count", `"9999-12-31T23:59:59Z"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := m.New(st)
			var report tabletext.Report
			if m.Read(inst, []byte(`{"at": `+tc.text+`}`), &report) {
				t.Fatalf("%s was accepted", tc.text)
			}
			if !report.Malformed {
				t.Fatalf("%s did not set Malformed: %+v", tc.text, report)
			}
			if want := "timestamp_not_rfc3339: at"; report.Reason != want {
				t.Fatalf("%s was refused as %q, want %q", tc.text, report.Reason, want)
			}
		})
	}

	// §16.1, §16.2: a NUMBER token is the dialect's wrong-JSON-type rule —
	// skipped, counted as a kind mismatch, never coerced to a count.
	t.Run("number token", func(t *testing.T) {
		inst := m.New(st)
		var report tabletext.Report
		if !m.Read(inst, []byte(`{"at": 0}`), &report) {
			t.Fatalf("the object itself should read: %+v", report)
		}
		if report.Malformed || report.KindMismatch != 1 {
			t.Fatalf("a NUMBER token should be one kind_mismatch, got %+v", report)
		}
		if got := inst.Fields[0].Cell.I; got != 0 {
			t.Fatalf("the number was coerced into the field: %d", got)
		}
	})
}
