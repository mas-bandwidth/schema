package slowtest

import (
	"fmt"
	"strings"
	"testing"
)

type recordTB struct {
	testing.TB
	fatal string
	logs  []string
}

func (r *recordTB) Helper()                      {}
func (r *recordTB) Fatalf(f string, args ...any) { r.fatal = fmt.Sprintf(f, args...) }
func (r *recordTB) Logf(f string, args ...any)   { r.logs = append(r.logs, fmt.Sprintf(f, args...)) }

func TestRewritingIsLoudUnderCIAndOnATypo(t *testing.T) {
	for _, c := range []struct {
		name, value, ci string
		want            bool
		fatal           string
	}{
		{"unset", "", "", false, ""},
		{"local rewrite", "1", "", true, ""},
		{"under CI", "1", "true", false, "under CI"},
		{"a typo", "yes", "", false, `"yes"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SCHEMA_UPDATE_PROBE_GOLDEN", c.value)
			t.Setenv("CI", c.ci)
			r := &recordTB{}
			got := Rewriting(r, "SCHEMA_UPDATE_PROBE_GOLDEN")
			if got != c.want {
				t.Errorf("Rewriting = %v, want %v", got, c.want)
			}
			if (c.fatal == "") != (r.fatal == "") || !strings.Contains(r.fatal, c.fatal) {
				t.Errorf("fatal %q, want one naming %q", r.fatal, c.fatal)
			}
			if c.want && len(r.logs) == 0 {
				t.Error("a local rewrite logged nothing: it must say it compared nothing")
			}
		})
	}
}
