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
	t.Parallel()

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
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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

	t.Run("absent under SCHEMA_REQUIRE_CORPUS fails by name", func(t *testing.T) {
		setEnv(t, "SCHEMA_REQUIRE_CORPUS", "1")
		fake := &fakeTB{}
		if got := SerializeGo(fake, filepath.Join(t.TempDir(), "repo")); got != "" {
			t.Fatalf("SerializeGo = %q, want empty on the fail branch", got)
		}
		if !fake.failed {
			t.Fatal("SerializeGo under SCHEMA_REQUIRE_CORPUS did not fail")
		}
		if fake.skipped {
			t.Fatal("SerializeGo under SCHEMA_REQUIRE_CORPUS also skipped")
		}
		assertAbsentMessage(t, fake.msg)
	})

	t.Run("absent with slow gate enabled fails by name", func(t *testing.T) {
		setEnv(t, "SCHEMA_REQUIRE_CORPUS", "")
		setEnv(t, "SCHEMA_SLOW", "1")
		fake := &fakeTB{}
		if got := SerializeGo(fake, filepath.Join(t.TempDir(), "repo")); got != "" {
			t.Fatalf("SerializeGo = %q, want empty on the fail branch", got)
		}
		if !fake.failed || fake.skipped {
			t.Fatalf("SerializeGo with SCHEMA_SLOW: failed=%v skipped=%v", fake.failed, fake.skipped)
		}
		assertAbsentMessage(t, fake.msg)
	})

	t.Run("absent with slow gate disabled skips by marker", func(t *testing.T) {
		setEnv(t, "SCHEMA_REQUIRE_CORPUS", "")
		setEnv(t, "SCHEMA_SLOW", "")
		fake := &fakeTB{}
		if got := SerializeGo(fake, filepath.Join(t.TempDir(), "repo")); got != "" {
			t.Fatalf("SerializeGo = %q, want empty on the skip branch", got)
		}
		if fake.failed || !fake.skipped {
			t.Fatalf("SerializeGo without slow gate: failed=%v skipped=%v", fake.failed, fake.skipped)
		}
		if !strings.HasPrefix(fake.msg, gateSkipMarker) || !strings.HasSuffix(fake.msg, "run make deps") {
			t.Fatalf("skip message %q must start with %q and end with run make deps", fake.msg, gateSkipMarker)
		}
	})

	t.Run("the absolute environment override wins over the sibling", func(t *testing.T) {
		tmp := t.TempDir()
		root := filepath.Join(tmp, "repo")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		want := mkRuntime(t, filepath.Join(tmp, "configured"))
		mkRuntime(t, filepath.Join(tmp, "serialize.go"))
		setEnv(t, "SERIALIZE_GO", want)
		if got := SerializeGo(t, root); got != want {
			t.Fatalf("SerializeGo = %q, want override %q", got, want)
		}
	})
}

// setEnv sets a process variable and restores it when the test ends. The
// parent test is parallel, so the testing package's t.Setenv (which forbids
// parallel) cannot be used; the two subtests that need it do not call
// t.Parallel and run inline before the parallel ones resume.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
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

// fakeTB stands in for a *testing.T so the fail and skip branches can be
// exercised without failing or skipping the real test. Its embedded testing.TB
// supplies the interface's unexported method; the two methods SerializeGo calls
// are overridden to record instead of exiting.
type fakeTB struct {
	testing.TB
	failed  bool
	skipped bool
	msg     string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
	f.msg = fmt.Sprintf(format, args...)
}

func (f *fakeTB) Skipf(format string, args ...any) {
	f.skipped = true
	f.msg = fmt.Sprintf(format, args...)
}
func TestSerializeGoSkipMarkerMatchesSlowGateScanner(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "tools", "slowgatescan", "main.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "const gateSkipMarker = \"" + gateSkipMarker + "\""
	if !strings.Contains(string(data), want) {
		t.Fatalf("slowtest skip marker %q differs from tools/slowgatescan's marker; expected %q", gateSkipMarker, want)
	}
}
