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
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/mas-bandwidth/serialize.go\n"), 0o600); err != nil {
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

	t.Run("absent under SCHEMA_SLOW alone skips by name", func(t *testing.T) {
		setEnv(t, "SCHEMA_REQUIRE_CORPUS", "")
		setEnv(t, "SCHEMA_SLOW", "1")
		fake := &fakeTB{}
		if got := SerializeGo(fake, filepath.Join(t.TempDir(), "repo")); got != "" {
			t.Fatalf("SerializeGo = %q, want empty on the skip branch", got)
		}
		if fake.failed {
			t.Fatal("SerializeGo under SCHEMA_SLOW without SCHEMA_REQUIRE_CORPUS failed")
		}
		if !fake.skipped {
			t.Fatal("SerializeGo under SCHEMA_SLOW without SCHEMA_REQUIRE_CORPUS did not skip")
		}
		assertAbsentMessage(t, fake.msg)
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
	for _, want := range []string{"serialize.go", "build/serialize.go", "make tables-fixedform-corpus"} {
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
