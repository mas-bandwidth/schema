// Shared gate S3: the floor is the operator's, schema lock --retire with reason,
// and a second retire of the same hash is idempotent (docs/roadmap.sexp shared/S3).
//
// Clauses from the title:
//  1. the floor is the operator's — retiring entries moves the floor (lockfile.Floor)
//  2. schema lock --retire T@0x<hash> --reason "..." — retire a layout by hash
//  3. a second retire of the same hash is idempotent
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// sharedS3Schema is the fixture: one fixed table we will lock, widen, and retire.
const sharedS3Schema = `package s3gate

fixed table Config
{
    version int32
    name    string(64)
}
`

// sharedS3Widened appends a field, which moves the layout hash.
const sharedS3Widened = `package s3gate

fixed table Config
{
    version int32
    name    string(64)
    flags   uint32
}
`

// lockWidenLock creates a temp dir, writes the schema, locks it, widens it,
// locks again, and returns the dir, paths, the widened unit, and the two
// wire hashes (older, current).
func lockWidenLock(t *testing.T) (dir string, paths []string, w *ir.Unit, olderHash, currentHash uint64) {
	t.Helper()
	dir = t.TempDir()
	schemaPath := filepath.Join(dir, "Config.schema")
	if err := os.WriteFile(schemaPath, []byte(sharedS3Schema), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	paths, err = compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	c := compiler.New()

	first, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	st := first.Tables["Config"]
	if st == nil {
		t.Fatal("fixture must declare Config")
	}
	olderHash = ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)

	if _, _, err := compiler.UpdateSchemaLock(first, paths); err != nil {
		t.Fatalf("first lock: %v", err)
	}

	if err := os.WriteFile(schemaPath, []byte(sharedS3Widened), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err = c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	st = w.Tables["Config"]
	if st == nil {
		t.Fatal("fixture must declare Config")
	}
	currentHash = ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)

	if olderHash == currentHash {
		t.Fatal("the widening must change the layout hash")
	}

	if _, _, err := compiler.UpdateSchemaLock(w, paths); err != nil {
		t.Fatalf("second lock: %v", err)
	}

	lockP := filepath.Join(dir, lockfile.FileName)
	lk := readLockFile(t, lockP)
	lin := lockfile.Lineage(lk, "Config")
	if len(lin) != 2 {
		t.Fatalf("lineage must have 2 entries after lock-widen-lock, got %d", len(lin))
	}
	if lin[0].Wire != olderHash {
		t.Fatalf("oldest entry hash mismatch: got 0x%016x, want 0x%016x", lin[0].Wire, olderHash)
	}
	if lin[1].Wire != currentHash {
		t.Fatalf("current entry hash mismatch: got 0x%016x, want 0x%016x", lin[1].Wire, currentHash)
	}

	return dir, paths, w, olderHash, currentHash
}

func readLockFile(t *testing.T, path string) *lockfile.Unit {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lk, err := lockfile.Parse(path, data)
	if err != nil {
		t.Fatal(err)
	}
	return lk
}

// hex64 formats a uint64 as 16-char lowercase hex.
func hex64(v uint64) string {
	return fmt.Sprintf("%016x", v)
}

// TestSharedS3Gate drives every clause of the shared/S3 roadmap node through
// the production entry point (schema lock --retire) and the library path
// (lockfile.Floor, compiler.RetireSchemaLock).
func TestSharedS3Gate(t *testing.T) {
	t.Run("the floor is the operator's", func(t *testing.T) {
		// The floor is §5.2's one number: one past the highest retired entry
		// index, 0 when none is retired (internal/lockfile/lineage.go:Floor).
		// The operator moves the floor by retiring entries.
		dir, paths, w, olderHash, currentHash := lockWidenLock(t)
		lockP := filepath.Join(dir, lockfile.FileName)

		// Floor is 0 before any retirement.
		lk := readLockFile(t, lockP)
		if f := lockfile.Floor(lk, "Config"); f != 0 {
			t.Fatalf("floor before retirement: got %d, want 0", f)
		}

		// Retire the older entry via the production API.
		reason := "v1 client retired 2026-09"
		if _, rewrote, err := compiler.RetireSchemaLock(w, paths, "Config@0x"+hex64(olderHash), reason); err != nil || !rewrote {
			t.Fatalf("retire older entry: rewrote=%v err=%v", rewrote, err)
		}

		// Reload the lock and check the floor moved to 1.
		lk2 := readLockFile(t, lockP)
		if f := lockfile.Floor(lk2, "Config"); f != 1 {
			t.Fatalf("floor after retiring entry 0: got %d, want 1", f)
		}

		// The current layout cannot be retired — a reader built from this lock
		// reads its own records.
		if _, _, err := compiler.RetireSchemaLock(w, paths, "Config@0x"+hex64(currentHash), "no"); err == nil {
			t.Fatal("retiring the current layout must be refused")
		}
	})

	t.Run("schema lock --retire T@0x<hash> --reason \"...\"", func(t *testing.T) {
		// Production: main.go lines 216-230 (schema lock --retire / --reason flags)
		//             compiler.RetireSchemaLock -> lockfile.Retire
		bin := buildCLI(t)
		dir, _, _, olderHash, _ := lockWidenLock(t)

		reason := "the v1 client is gone"
		out := run(t, bin, "lock", "--retire", "Config@0x"+hex64(olderHash), "--reason", reason, "--verbose", dir)
		if !strings.Contains(out, "retired Config@0x") {
			t.Fatalf("schema lock --retire must print the retired entry: got %q", out)
		}

		// Verify the lock file has the retired entry.
		lockP := filepath.Join(dir, lockfile.FileName)
		lk := readLockFile(t, lockP)
		lin := lockfile.Lineage(lk, "Config")
		if len(lin) != 2 {
			t.Fatalf("lineage must still have 2 entries: got %d", len(lin))
		}
		if !lin[0].Retired {
			t.Fatal("the older entry must be marked retired")
		}
		if lin[0].Reason != reason {
			t.Fatalf("retired entry reason: got %q, want %q", lin[0].Reason, reason)
		}
		if lin[1].Retired {
			t.Fatal("the current entry must not be retired")
		}

		// --reason without --retire must fail.
		cmd := exec.Command(bin, "lock", "--reason", "orphan", dir)
		if err := cmd.Run(); err == nil {
			t.Fatal("schema lock --reason without --retire must fail")
		}

		// --retire without --reason must fail.
		cmd = exec.Command(bin, "lock", "--retire", "Config@0x"+hex64(olderHash), dir)
		if err := cmd.Run(); err == nil {
			t.Fatal("schema lock --retire without --reason must fail")
		}

		// Retiring a table the lock does not carry must fail.
		cmd = exec.Command(bin, "lock", "--retire", "Nope@0x0000000000000001", "--reason", "no", dir)
		if err := cmd.Run(); err == nil {
			t.Fatal("schema lock --retire for an unknown table must fail")
		}

		// Retiring the current layout must fail.
		currentHash := lin[1].Wire
		cmd = exec.Command(bin, "lock", "--retire", "Config@0x"+hex64(currentHash), "--reason", "no", dir)
		if err := cmd.Run(); err == nil {
			t.Fatal("schema lock --retire for the current layout must fail")
		}
	})

	t.Run("a second retire of the same hash is idempotent", func(t *testing.T) {
		// Production: lockfile.Retire lines 718-719 — if the rendered text
		// is unchanged, returns rewrote=false with no error.
		dir, paths, w, olderHash, _ := lockWidenLock(t)
		lockP := filepath.Join(dir, lockfile.FileName)

		reason := "v1 client retired"
		// First retire.
		_, rewrote1, err := compiler.RetireSchemaLock(w, paths, "Config@0x"+hex64(olderHash), reason)
		if err != nil {
			t.Fatalf("first retire: %v", err)
		}
		if !rewrote1 {
			t.Fatal("the first retire must rewrite the file")
		}

		// Record the file state after first retire.
		data1, err := os.ReadFile(lockP)
		if err != nil {
			t.Fatal(err)
		}

		// Second retire of the same hash — must be idempotent.
		_, rewrote2, err := compiler.RetireSchemaLock(w, paths, "Config@0x"+hex64(olderHash), reason)
		if err != nil {
			t.Fatalf("second retire (idempotent): %v", err)
		}
		if rewrote2 {
			t.Fatal("the second retire must NOT rewrite the file (idempotent)")
		}

		// The file must be unchanged.
		data2, err := os.ReadFile(lockP)
		if err != nil {
			t.Fatal(err)
		}
		if string(data1) != string(data2) {
			t.Fatal("the lock file must be byte-identical after the second retire")
		}

		// CLI-level idempotency: second retire prints "already retired".
		bin := buildCLI(t)
		out := run(t, bin, "lock", "--retire", "Config@0x"+hex64(olderHash), "--reason", reason, "--verbose", dir)
		if !strings.Contains(out, "already retired") {
			t.Fatalf("second CLI retire must say 'already retired': got %q", out)
		}
	})
}
