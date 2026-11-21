package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AN EXISTING DESTINATION IS NEVER MISSING. A reader that opens the side file
// or the payload at any point of a pair publish over an earlier pair finds a
// whole file there, the earlier one or the new one, never no file: not
// between the two renames, and not while a failed publish puts the earlier
// pair back. The publisher's file operations are the seam: the destinations
// are looked at after every one, so the test needs no timing. The cases are
// the committed corpus testdata/paired_publish/observed.json, over the real
// old and new pairs beside the other paired_publish cases.
func TestPairedPublishNeverHidesAnExistingDestination(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join("testdata", "paired_publish")
	var cases []struct {
		Name, Verb, Fail, Expected string
	}
	if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(corpus, "observed.json"))), &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("the corpus holds no case")
	}
	errInjected := errors.New("rename refused (injected)")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			flags := map[string]string{"pack": "--announce", "cook": "--attribution"}
			if flags[tc.Verb] == "" {
				t.Fatalf("the corpus names verb %q", tc.Verb)
			}
			dir := t.TempDir()
			side, payload := filepath.Join(dir, "side.bin"), filepath.Join(dir, "payload.bin")
			olds := map[string]string{
				side:    mustRead(t, filepath.Join(corpus, "initial", tc.Verb, "side.bin")),
				payload: mustRead(t, filepath.Join(corpus, "initial", tc.Verb, "payload.bin")),
			}
			news := map[string]string{
				side:    mustRead(t, filepath.Join(corpus, "expected", tc.Verb, "side.bin")),
				payload: mustRead(t, filepath.Join(corpus, "expected", tc.Verb, "payload.bin")),
			}
			for p, data := range olds {
				writeTestFile(t, p, data)
			}

			steps := 0
			look := func(step string) {
				steps++
				for _, p := range []string{side, payload} {
					got, err := os.ReadFile(p)
					if errors.Is(err, fs.ErrNotExist) {
						t.Errorf("after %s: %s is missing: an existing destination must hold a whole file at every step", step, p)
						continue
					}
					if err != nil {
						t.Errorf("after %s: %s: %v", step, p, err)
						continue
					}
					if string(got) != olds[p] && string(got) != news[p] {
						t.Errorf("after %s: %s holds %d bytes: neither the earlier file nor the new one", step, p, len(got))
					}
				}
			}
			failOnto := map[string]string{"side": side, "payload": payload}[tc.Fail]
			failed := false
			f := outputFiles()
			wrap := func(op string, err error) error { look(op); return err }
			create, write, rename, remove, removeAll := f.Create, f.Write, f.Rename, f.Remove, f.RemoveAll
			f.Create = func(name string, mode fs.FileMode) (*os.File, error) {
				file, err := create(name, mode)
				look("create " + name)
				return file, err
			}
			f.Write = func(file *os.File, name string, data []byte) error {
				return wrap("write "+name, write(file, name, data))
			}
			f.Rename = func(oldname, newname string) error {
				if newname == failOnto && !failed {
					failed = true
					return wrap("a refused rename onto "+newname, errInjected)
				}
				return wrap("rename "+oldname+" to "+newname, rename(oldname, newname))
			}
			f.Remove = func(name string) error { return wrap("remove "+name, remove(name)) }
			f.RemoveAll = func(name string) error { return wrap("remove "+name, removeAll(name)) }

			err := publishOutputsWith(f, tc.Verb, []pairOutput{
				{outputPath{flags[tc.Verb], side}, []byte(news[side])},
				{outputPath{"--out", payload}, []byte(news[payload])},
			}, nil)
			switch {
			case tc.Fail == "" && err != nil:
				t.Fatal(err)
			case tc.Fail != "" && !errors.Is(err, errInjected):
				t.Fatalf("publish = %v; want the injected failure", err)
			case tc.Fail != "" && !failed:
				t.Fatal("the injected rename was never reached")
			}
			if steps == 0 {
				t.Fatal("the publish made no file operation to look between")
			}
			for _, name := range []string{"side.bin", "payload.bin"} {
				want := mustRead(t, filepath.Join(corpus, tc.Expected, tc.Verb, name))
				if got := mustRead(t, filepath.Join(dir, name)); got != want {
					t.Errorf("%s ends holding %d bytes; want the %s pair's %d", name, len(got), tc.Expected, len(want))
				}
			}
			wantEntries(t, dir, "payload.bin", "side.bin")
			if tc.Fail != "" && !strings.Contains(err.Error(), "nothing was changed") {
				t.Errorf("the failure does not say nothing was changed: %v", err)
			}
		})
	}
}
