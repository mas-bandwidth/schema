package gotable

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

	// 2. The generated VarText/FixText Go writers are compiled and executed with
	// ill-formed UTF-8 and an unpaired surrogate: variable Save/Measure and
	// FixedSave refuse as invalid_value and leave a poisoned buffer untouched.
	files, err := Generate(unitFrom(t, writeRefusalRoundTwoSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files["go.mod"] = []byte("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/schema/v2 => " + repo + "\n")
	files["refusal_test.go"] = []byte(`package probe

import "testing"

func poisoned(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 0xA5
	}
	return b
}

func untouched(t *testing.T, where string, b []byte) {
	t.Helper()
	for i, c := range b {
		if c != 0xA5 {
			t.Fatalf("%s: refused write modified buffer at byte %d (0x%02x)", where, i, c)
		}
	}
}

func TestGeneratedWritersRefuseIllFormedText(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(v *VarText)
	}{
		{"narrow ill-formed UTF-8", func(v *VarText) { v.Narrow[0] = 0xff; v.NarrowLength = 1 }},
		{"wide unpaired high surrogate", func(v *VarText) { v.Wide[0] = 0xd800; v.WideLength = 1 }},
		{"wide unpaired low surrogate", func(v *VarText) { v.Wide[0] = 0xdc00; v.WideLength = 1 }},
	} {
		var v VarText
		VarTextReset(&v)
		tc.set(&v)
		if n := VarTextMeasure(&v); n != -1 {
			t.Fatalf("%s: VarTextMeasure got %d, want -1", tc.name, n)
		}
		if n := VarTextSave(&v, poisoned(256)); n != -1 {
			t.Fatalf("%s: VarTextSave got %d, want -1", tc.name, n)
		}
	}
	for _, tc := range []struct {
		name string
		set  func(f *FixText)
	}{
		{"narrow ill-formed UTF-8", func(f *FixText) { f.Narrow[0] = 0xff; f.NarrowLength = 1 }},
		{"wide unpaired low surrogate", func(f *FixText) { f.Wide[0] = 0xdc00; f.WideLength = 1 }},
	} {
		var f FixText
		FixTextReset(&f)
		tc.set(&f)
		if n := FixTextMeasure(&f); n != -1 {
			t.Fatalf("%s: FixTextMeasure got %d, want -1", tc.name, n)
		}
		if n := FixTextSave(&f, poisoned(256)); n != -1 {
			t.Fatalf("%s: FixTextSave got %d, want -1", tc.name, n)
		}
	}
}
`)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated table refusal unit: %v\n%s", err, out)
	}
}
