// THE --runtime GATE, BY TARGET (docs/SPEC-TABLES.md §8.4). The flag is the Go
// target's; every other target refuses it by name, set or not, because the
// gate reads presence and not value. The value itself is the compiler's: it
// refuses an unknown runtime for Go in gotable.ParseRuntime.
package main

import (
	"strings"
	"testing"
)

func TestRefuseRuntimeForTarget(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name       string
		runtimeSet bool
		lang       string
		wantErr    bool
	}{
		{"a go target accepts standalone", true, "go", false},
		{"a go target accepts no --runtime", false, "go", false},
		{"a non-go target refuses standalone", true, "cpp", true},
		{"a non-go target refuses the empty --runtime=", true, "rust", true},
		{"a non-go target refuses a bad value by name too", true, "js", true},
		{"a non-go target accepts no --runtime", false, "cpp", false},
	} {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			err := refuseRuntimeForTarget(row.runtimeSet, row.lang)
			if row.wantErr != (err != nil) {
				t.Fatalf("refuseRuntimeForTarget(%v, %q) = %v, want error: %v", row.runtimeSet, row.lang, err, row.wantErr)
			}
			if err == nil {
				return
			}
			if !strings.Contains(err.Error(), "--runtime") {
				t.Errorf("error %q does not name the --runtime flag", err)
			}
			if !strings.Contains(err.Error(), row.lang) {
				t.Errorf("error %q does not name the %s target", err, row.lang)
			}
		})
	}
}
