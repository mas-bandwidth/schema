package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestCookWriteFixturesAreTheToolsBytes holds every committed cook-write
// fixture to compiler.Cook, the same writer harness generate uses. Resolving
// the manifest paths against the repository root avoids changing process cwd
// in this parallel test.
func TestCookWriteFixturesAreTheToolsBytes(t *testing.T) {
	t.Parallel()

	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, path := range m.Units[i].Paths {
			if !filepath.IsAbs(path) {
				m.Units[i].Paths[j] = filepath.Join(root, path)
			}
		}
	}
	u := newUnits(m)
	for _, cw := range m.CookWrites {
		t.Run(cw.Instance, func(t *testing.T) {
			inst, err := m.LookupInstance(cw.Instance)
			if err != nil {
				t.Fatal(err)
			}
			unit, err := u.get(inst.Unit)
			if err != nil {
				t.Fatal(err)
			}
			wirePath := inst.Wire
			if !filepath.IsAbs(wirePath) {
				wirePath = filepath.Join(root, wirePath)
			}
			wire, err := os.ReadFile(wirePath)
			if err != nil {
				t.Fatal(err)
			}
			for _, arm := range []struct {
				path string
				big  bool
				name string
			}{
				{cw.Little, false, "little-endian"},
				{cw.Big, true, "big-endian"},
			} {
				cooked, _, report, err := u.c.Cook(unit, inst.Root, wire, compiler.CookOptions{Big: arm.big})
				if err != nil {
					t.Errorf("%s: cook: %v", arm.name, err)
					continue
				}
				if !cookedReportClean(inst, report) {
					t.Errorf("%s: tool output does not read clean: %+v", arm.name, report)
					continue
				}
				fixturePath := arm.path
				if !filepath.IsAbs(fixturePath) {
					fixturePath = filepath.Join(root, fixturePath)
				}
				want, err := os.ReadFile(fixturePath)
				if err != nil {
					t.Errorf("%s: read fixture: %v", arm.name, err)
					continue
				}
				if bytes.Equal(cooked, want) {
					continue
				}
				first := 0
				limit := min(len(want), len(cooked))
				for first < limit && cooked[first] == want[first] {
					first++
				}
				t.Errorf("%s: %s differs at byte offset %d (tool=%d bytes, fixture=%d bytes)",
					cw.Instance, arm.name, first, len(cooked), len(want))
			}
		})
	}
}
