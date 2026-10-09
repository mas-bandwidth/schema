package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestEmptyRootFirstLockDisposition(t *testing.T) {
	for _, tc := range []struct {
		declaration string
		emitted     bool
	}{{"fixed table", true}, {"table", false}} {
		t.Run(tc.declaration, func(t *testing.T) {
			dir, paths := fixture(t, pkg(tc.declaration+" Row\n{\n}\n"))
			u := load(t, paths)
			if got := ir.TableFixedEmitted(u, u.Tables["Row"]); got != tc.emitted {
				t.Fatalf("fixture emission=%v, want %v", got, tc.emitted)
			}
			_, rewrote, err := lockfile.Update(u, paths)
			if !tc.emitted {
				if err != nil || !rewrote {
					t.Fatalf("Form1-only empty table should remain supported: rewrote=%v err=%v", rewrote, err)
				}
				if errs := lockfile.Check(u, paths); len(errs) != 0 {
					t.Fatalf("Form1-only lock: %v", errs)
				}
				return
			}
			if err == nil || rewrote || !strings.Contains(err.Error(), "layout_record_too_large") {
				t.Fatalf("emitted zero root must refuse before first write: rewrote=%v err=%v", rewrote, err)
			}
			if _, err := os.Stat(filepath.Join(dir, lockfile.FileName)); !os.IsNotExist(err) {
				t.Fatalf("refusal created a lock: %v", err)
			}
		})
	}
}

func TestRecordedEmittedEmptyRootCannotBypassValidation(t *testing.T) {
	dir, paths := fixture(t, rowTable(""))
	u := load(t, paths)
	// Model an already recorded zero root with correct hashes and rollup.
	before := []byte(lockfile.Render(u).Text())
	file := filepath.Join(dir, lockfile.FileName)
	if err := os.WriteFile(file, before, 0600); err != nil {
		t.Fatal(err)
	}
	errs := lockfile.Check(u, paths)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "layout_record_too_large") {
		t.Fatalf("recorded emitted zero root must refuse: %v", errs)
	}
	if _, rewrote, err := lockfile.Update(u, paths); err == nil || rewrote || !strings.Contains(err.Error(), "layout_record_too_large") {
		t.Fatalf("invalid recorded root must not be rewritten: rewrote=%v err=%v", rewrote, err)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("refusal changed recorded bytes")
	}
}
