package slowtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSerializeGoFindsTheRuntime is the unit gate over SerializeGo: it runs on
// t.TempDir() trees only, no network, no sockets, no real time. It pins the
// order the probes depend on — the sibling checkout before the fetched copy —
// and that a directory of the right name without a go.mod is not a runtime.
func TestSerializeGoFindsTheRuntime(t *testing.T) {
	// Every candidate belongs to this test, regardless of the caller runtime pin.
	t.Setenv("SERIALIZE_GO", "")

	mkRuntime := func(t *testing.T, dir string) string {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/mas-bandwidth/serialize.go/v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("the sibling wins over the fetched copy", func(t *testing.T) {
		tmp := t.TempDir()
		root := filepath.Join(tmp, "repo")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		sibling := mkRuntime(t, filepath.Join(tmp, "serialize.go"))
		mkRuntime(t, filepath.Join(root, "build", "serialize.go"))
		if got := SerializeGo(t, root); got != sibling {
			t.Fatalf("SerializeGo = %q, want the sibling %q", got, sibling)
		}
	})

	t.Run("the fetched copy is used when there is no sibling", func(t *testing.T) {
		tmp := t.TempDir()
		root := filepath.Join(tmp, "repo")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		fetched := mkRuntime(t, filepath.Join(root, "build", "serialize.go"))
		if got := SerializeGo(t, root); got != fetched {
			t.Fatalf("SerializeGo = %q, want the fetched copy %q", got, fetched)
		}
	})

	t.Run("a directory without go.mod is not a runtime", func(t *testing.T) {
		tmp := t.TempDir()
		root := filepath.Join(tmp, "repo")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(tmp, "serialize.go"), 0o755); err != nil {
			t.Fatal(err)
		}
		fetched := mkRuntime(t, filepath.Join(root, "build", "serialize.go"))
		if got := SerializeGo(t, root); got != fetched {
			t.Fatalf("SerializeGo = %q, want the fetched copy %q (the bare sibling is not a runtime)", got, fetched)
		}
	})

	t.Run("absent fails by name", func(t *testing.T) {
		fake := &fakeTB{}
		if got := SerializeGo(fake, filepath.Join(t.TempDir(), "repo")); got != "" {
			t.Fatalf("SerializeGo = %q, want empty on the fail branch", got)
		}
		if !fake.failed {
			t.Fatal("SerializeGo with no runtime did not fail")
		}
		assertAbsentMessage(t, fake.msg)
	})

	t.Run("the absolute environment override wins over the sibling", func(t *testing.T) {
		tmp := t.TempDir()
		root := filepath.Join(tmp, "repo")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		want := mkRuntime(t, filepath.Join(tmp, "configured"))
		mkRuntime(t, filepath.Join(tmp, "serialize.go"))
		t.Setenv("SERIALIZE_GO", want)
		if got := SerializeGo(t, root); got != want {
			t.Fatalf("SerializeGo = %q, want override %q", got, want)
		}
	})
}

func assertAbsentMessage(t *testing.T, msg string) {
	t.Helper()
	for _, want := range []string{"SERIALIZE_GO", "serialize.go", "build/serialize.go", "run make deps"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the absent message %q does not name %q", msg, want)
		}
	}
}

// fakeTB stands in for a *testing.T so the fail branch can be
// exercised without failing the real test. Its embedded testing.TB
// supplies the interface's unexported method; the methods SerializeGo calls
// are overridden to record instead of exiting.
type fakeTB struct {
	testing.TB
	failed bool
	msg    string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
	f.msg = fmt.Sprintf(format, args...)
}
