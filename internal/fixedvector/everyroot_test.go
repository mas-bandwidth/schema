package fixedvector

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTB records what EveryRoot says when its test ends.
type fakeTB struct {
	cleanups []func()
	errors   []string
}

func (f *fakeTB) Helper()           {}
func (f *fakeTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }
func (f *fakeTB) Errorf(format string, a ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, a...))
}
func (f *fakeTB) Logf(string, ...any) {}
func (f *fakeTB) end() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func writeManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestManifestReadsEveryRow(t *testing.T) {
	dir := writeManifest(t, "file=a.bin row=a side=none root=A records=2 values=r0.x=1\nfile=b.bin row=b side=old root=B records=1 values=r0.y=2\n")
	rows, err := Manifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0] != (ManifestRow{File: "a.bin", Row: "a", Side: "none", Root: "A", Records: 2}) || rows[1].File != "b.bin" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestManifestRefusesByName(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"missing", "", "not readable"},
		{"empty", "# nothing\n", "names no file"},
		{"twice", "file=a.bin records=1\nfile=a.bin records=1\n", "names a.bin twice"},
		{"no count", "file=a.bin root=A\n", "no records= count"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.name != "missing" {
				dir = writeManifest(t, c.body)
			}
			_, err := Manifest(dir)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want it to say %q", err, c.want)
			}
		})
	}
}

func TestEveryRootHoldsTheAnswersToTheManifest(t *testing.T) {
	rows := []ManifestRow{{File: "a.bin"}, {File: "b.bin"}, {File: "c.bin"}}
	t.Run("every root", func(t *testing.T) {
		f := &fakeTB{}
		a := EveryRoot(f, "leg", rows)
		a.Answer("a.bin")
		a.Answer("b.bin")
		a.Answer("c.bin")
		f.end()
		if len(f.errors) != 0 {
			t.Fatalf("errors %q", f.errors)
		}
	})
	// THE PLANT: one root dropped from the answers is red by its name
	t.Run("dropped root", func(t *testing.T) {
		f := &fakeTB{}
		a := EveryRoot(f, "leg", rows)
		a.Answer("a.bin")
		a.Answer("c.bin")
		f.end()
		if len(f.errors) != 2 || !strings.Contains(f.errors[0], "corpus root b.bin") || !strings.Contains(f.errors[1], "answered 2 of the manifest's 3") {
			t.Fatalf("errors %q", f.errors)
		}
	})
	t.Run("answer the manifest lacks", func(t *testing.T) {
		f := &fakeTB{}
		a := EveryRoot(f, "leg", rows)
		for _, r := range []string{"a.bin", "b.bin", "c.bin", "z.bin"} {
			a.Answer(r)
		}
		f.end()
		if len(f.errors) != 2 || !strings.Contains(f.errors[0], "z.bin, which the corpus manifest does not carry") {
			t.Fatalf("errors %q", f.errors)
		}
	})
}
