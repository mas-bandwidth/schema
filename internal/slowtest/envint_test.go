package slowtest

import (
	"fmt"
	"strings"
	"testing"
)

type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper()                      {}
func (f *fatalTB) Fatalf(s string, args ...any) { f.msg = fmt.Sprintf(s, args...) }

func TestEnvIntRefusesWhatIsNotANumber(t *testing.T) {
	t.Setenv("SCHEMA_PROBE_N", "")
	if got := EnvInt(&fatalTB{}, "SCHEMA_PROBE_N", 7); got != 7 {
		t.Errorf("unset: %d, want the default 7", got)
	}
	t.Setenv("SCHEMA_PROBE_N", "12")
	if got := EnvInt(&fatalTB{}, "SCHEMA_PROBE_N", 7); got != 12 {
		t.Errorf("12: %d", got)
	}
	t.Setenv("SCHEMA_PROBE_N", "1e3")
	f := &fatalTB{}
	EnvInt(f, "SCHEMA_PROBE_N", 7)
	if !strings.Contains(f.msg, `SCHEMA_PROBE_N="1e3"`) {
		t.Errorf("a value that is no integer was not refused by name: %q", f.msg)
	}
}

func TestEnvSeedRangeRefusesAnEmptyRange(t *testing.T) {
	t.Setenv("SCHEMA_TABLE_DIFF_FROM", "5")
	t.Setenv("SCHEMA_TABLE_DIFF_TO", "")
	f := &fatalTB{}
	EnvSeedRange(f, 0, 4)
	if !strings.Contains(f.msg, "holds no seed") {
		t.Errorf("FROM=5 over the default TO=4 was not refused: %q", f.msg)
	}
	t.Setenv("SCHEMA_TABLE_DIFF_FROM", "2")
	f = &fatalTB{}
	if from, to := EnvSeedRange(f, 0, 4); from != 2 || to != 4 || f.msg != "" {
		t.Errorf("2..4: got %d..%d %q", from, to, f.msg)
	}
}
