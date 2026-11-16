package cpptable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The report driver must route a variable ToolEnvelope through its region
// codec while preserving the fixed ToolMessage codec in the same unit.
func TestCppConformanceToolEnvelopeReport(t *testing.T) {
	root := cppConformanceRoot(t)
	cppConformanceMake(t, root, "build/conformance-cpp")
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest")
	rows := []struct{ name, typ, wire, verdict string }{
		{"exact", "ToolEnvelope", "testdata/wire/tables/nested_arm_exact.bin", "read"},
		{"inside", "ToolEnvelope", "testdata/wire/tables/nested_arm_trailing.bin", "refused"},
		{"outside", "ToolEnvelope", "testdata/wire/tables/nested_arm_trailing_top.bin", "refused"},
		{"fixed", "ToolMessage", "testdata/conformance/tables/wire/message_open.bin", "read"},
	}
	var text strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&text, "report %s messagedemo %s %s\n", r.name, r.typ, r.wire)
	}
	if err := os.WriteFile(manifest, []byte(text.String()), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(root, "build/conformance-cpp"), manifest, "report", dir)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("report driver: %v\n%s", err, out)
	}
	for _, r := range rows {
		answer, err := os.ReadFile(filepath.Join(dir, r.name))
		if err != nil {
			t.Fatal(err)
		}
		if got := cppFramingVerdict(answer); got != r.verdict {
			t.Errorf("%s: verdict %q, want %q: %s", r.name, got, r.verdict, answer)
		}
		if r.verdict == "read" {
			wire, err := os.ReadFile(filepath.Join(dir, r.name+".wire"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(root, r.wire))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(wire, want) {
				t.Errorf("%s: canonical write-back differs", r.name)
			}
			if r.name == "exact" {
				var decoded struct {
					Sequence int `json:"sequence"`
					Body     struct {
						Origin struct {
							PID int `json:"pid"`
						} `json:"origin"`
					} `json:"body"`
				}
				_, value, ok := bytes.Cut(answer, []byte("\n"))
				if !ok || json.Unmarshal(value, &decoded) != nil || decoded.Sequence != 41 || decoded.Body.Origin.PID != 5 {
					t.Errorf("exact: wrong decoded nested value: %s", answer)
				}
			}
		} else if _, err := os.Stat(filepath.Join(dir, r.name+".wire")); !os.IsNotExist(err) {
			t.Errorf("%s: refused row wrote a wire", r.name)
		}
	}
}
