package compiler

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A retirement is visible immediately to the unit that served the historical
// file. Other supported layouts must still return the same canonical value.
func TestRetireLoadedLayoutCorpus(t *testing.T) {
	type readCase struct {
		Input string
		Want  string // "refuse" or the expected canonical write-back file
	}
	var sequence struct {
		Root, Target            string
		Before, After, Reloaded []readCase
	}
	data, err := os.ReadFile("testdata/lock-retirement/retire-loaded-layout.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &sequence); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../test/tables/fixedevo")); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	load := func() *ir.Unit {
		t.Helper()
		unit, err := c.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		return unit
	}
	files := os.DirFS("../testdata/conformance/tables")
	check := func(phase string, unit *ir.Unit, cases []readCase) {
		t.Helper()
		if len(cases) == 0 {
			t.Fatalf("%s: missing corpus reads", phase)
		}
		for _, tc := range cases {
			t.Run(phase+"/"+filepath.Base(tc.Input), func(t *testing.T) {
				wire, err := fs.ReadFile(files, tc.Input)
				if err != nil {
					t.Fatal(err)
				}
				report, err := c.ReadReport(unit, sequence.Root, wire)
				if tc.Want == "refuse" {
					if err == nil && !report.Refused {
						t.Fatal("read accepted a retired layout")
					}
					return
				}
				if err != nil || report.Refused {
					t.Fatalf("read refused a supported layout: report=%+v err=%v", report, err)
				}
				tree := t.TempDir()
				if report, err := c.UnpackOneFile(unit, sequence.Root, wire, tree); err != nil || report.Refused {
					t.Fatalf("unpack: report=%+v err=%v", report, err)
				}
				got, _, report, err := c.Pack(unit, sequence.Root, tree)
				if err != nil || report.Refused {
					t.Fatalf("repack: report=%+v err=%v", report, err)
				}
				want, err := fs.ReadFile(files, tc.Want)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("canonical write-back differs from %s", tc.Want)
				}
			})
		}
	}
	unit := load()
	check("before", unit, sequence.Before)
	if _, changed, err := RetireSchemaLock(unit, paths, sequence.Target, "the v1 writer is retired"); err != nil || !changed {
		t.Fatalf("retire %s: changed=%v err=%v", sequence.Target, changed, err)
	}
	check("after", unit, sequence.After)
	check("reloaded", load(), sequence.Reloaded)
}
