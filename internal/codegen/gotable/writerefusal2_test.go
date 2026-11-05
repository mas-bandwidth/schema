package gotable

import (
	"errors"
	"strings"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
)

const writeRefusalRoundTwoSchema = `package probe
table VarText {
    narrow string(8)
    wide   wstring(8)
}
fixed table FixText {
    narrow string(8)
    wide   wstring(8)
}
`

func TestWriteRefusalRoundTwoRefusesIllFormedText(t *testing.T) {
	t.Parallel()

	// 1. Live execution over the generated Go unit (fuzzunit): Save, Measure,
	// SaveMessages and MeasureMessages refuse ill-formed UTF-8 as invalid_value
	// and write nothing to a poisoned buffer.
	good := func() tabledemo.TeamConfig {
		var v tabledemo.TeamConfig
		tabledemo.TeamConfigReset(&v)
		v.SpawnCount = 4
		v.BannerLength = 2
		copy(v.Banner[:], "ok")
		return v
	}
	poisoned := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = 0xA5
		}
		return b
	}
	untouched := func(t *testing.T, where string, b []byte) {
		t.Helper()
		for i, c := range b {
			if c != 0xA5 {
				t.Fatalf("%s: refused write modified buffer at byte %d (0x%02x)", where, i, c)
			}
		}
	}

	ctrl := good()
	if n, err := tabledemo.TeamConfigMeasureReason(&ctrl); err != nil || n <= 0 {
		t.Fatalf("lawful TeamConfigMeasureReason: (%d, %v)", n, err)
	} else if got := tabledemo.TeamConfigSave(&ctrl, make([]byte, n)); got != n {
		t.Fatalf("lawful TeamConfigSave: got %d, want %d", got, n)
	}
	var ctrlRep tabledemo.TableReport
	if n := tabledemo.TeamConfigMeasureMessages([]*tabledemo.TeamConfig{&ctrl}, &ctrlRep); n <= 0 || ctrlRep.Reason != "" {
		t.Fatalf("lawful TeamConfigMeasureMessages: (%d, %+v)", n, ctrlRep)
	} else if got := tabledemo.TeamConfigSaveMessages([]*tabledemo.TeamConfig{&ctrl}, make([]byte, n), &ctrlRep); got != n || ctrlRep.Reason != "" {
		t.Fatalf("lawful TeamConfigSaveMessages: got %d, want %d (%+v)", got, n, ctrlRep)
	}

	for _, tc := range []struct {
		name  string
		bytes []byte
	}{
		{"ill-formed UTF-8 0xff 0xfe", []byte{0xff, 0xfe}},
		{"truncated UTF-8 0xc3", []byte{0xc3}},
		{"overlong UTF-8 0xc0 0x80", []byte{0xc0, 0x80}},
	} {
		v := good()
		clear(v.Banner[:])
		copy(v.Banner[:], tc.bytes)
		v.BannerLength = int32(len(tc.bytes))

		if n := tabledemo.TeamConfigMeasure(&v); n != -1 {
			t.Fatalf("TeamConfigMeasure(%s): got %d, want -1", tc.name, n)
		}
		if n, err := tabledemo.TeamConfigMeasureReason(&v); n != -1 || !errors.Is(err, tabledemo.TableRefuseInvalidValue) {
			t.Fatalf("TeamConfigMeasureReason(%s): got (%d, %v), want (-1, invalid_value)", tc.name, n, err)
		}
		if n := tabledemo.TeamConfigSave(&v, make([]byte, 256)); n != -1 {
			t.Fatalf("TeamConfigSave(%s): got %d, want -1", tc.name, n)
		}

		var mrep, srep tabledemo.TableReport
		if n := tabledemo.TeamConfigMeasureMessages([]*tabledemo.TeamConfig{&v}, &mrep); n != -1 || mrep.Reason != "invalid_value" {
			t.Fatalf("TeamConfigMeasureMessages(%s): got (%d, %q), want (-1, \"invalid_value\")", tc.name, n, mrep.Reason)
		}
		buf := poisoned(256)
		if n := tabledemo.TeamConfigSaveMessages([]*tabledemo.TeamConfig{&v}, buf, &srep); n != -1 || srep.Reason != "invalid_value" {
			t.Fatalf("TeamConfigSaveMessages(%s): got (%d, %q), want (-1, \"invalid_value\")", tc.name, n, srep.Reason)
		}
		untouched(t, "TeamConfigSaveMessages("+tc.name+")", buf)
	}

	// 2. Emitted Go writers for both string(N) (UTF-8) and wstring(N) (unpaired
	// UTF-16 surrogates) across variable Save/Measure, FixedSave and
	// SaveMessages/MeasureMessages refuse as invalid_value and write nothing.
	files, err := Generate(unitFrom(t, writeRefusalRoundTwoSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tableSrc := string(files["ProbeTable.go"])
	msgSrc := string(files["ProbeTableMessage.go"])
	for _, want := range []struct {
		where  string
		source string
		needle string
	}{
		{"variable narrow UTF-8 check", tableSrc, "!tableUTF8Valid(value.Narrow[:n])"},
		{"variable wide unpaired-surrogate check", tableSrc, "!tableUtf16UnitsValid(value.Wide[:value.WideLength])"},
		{"variable MeasureReason invalid_value", tableSrc, "return size, TableRefuseInvalidValue"},
		{"fixed narrow UTF-8 check", tableSrc, "!tableFixedUTF8Ok(value.Narrow[:value.NarrowLength])"},
		{"fixed wide unpaired-surrogate check", tableSrc, "tableFixedUnitsIllFormed(value.Wide[:value.WideLength])"},
		{"message narrow UTF-8 refusal", msgSrc, "if !utf8.Valid(value.Narrow[:value.NarrowLength]) {\n\t\t\t\treturn w.Refuse()\n\t\t\t}"},
		{"message wide unpaired-surrogate refusal", msgSrc, "u >= 0xdc00 && u <= 0xdfff {\n\t\t\t\t\treturn w.Refuse()"},
		{"message wide high-surrogate pair refusal", msgSrc, "value.Wide[i] < 0xdc00 || value.Wide[i] > 0xdfff {\n\t\t\t\t\t\treturn w.Refuse()"},
		{"message SaveMessages measures before first byte", msgSrc, "if need := FixTextMeasureMessages(values, report); need < 0 || int64(len(buffer)) < need {\n\t\treturn -1\n\t}"},
	} {
		if !strings.Contains(want.source, want.needle) {
			t.Fatalf("%s: missing %q in generated Go source", want.where, want.needle)
		}
	}
}
