package lobstertable

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const unitLockOldSchema = `package probe

fixed table Root
{
    lead uint32 = 1
    vals [1..4]int32
}
`

const unitLockNewSchema = `package probe

fixed table Root
{
    lead uint32 = 1
    vals [1..8]int32
}
`

func unitLockUnit(t *testing.T, path string) *ir.Unit {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse(path, src)
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: filepath.Base(path), Base: "Probe", Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func unitLockWrite(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

// unitLockGenerate generates u against the older layout, named by wire hash
// alone, so the writer the older build used comes from the unit's lock.
func unitLockGenerate(t *testing.T, u *ir.Unit, older FixedLineageEntry) string {
	t.Helper()
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"Root": {older}})
	if err != nil {
		t.Fatal(err)
	}
	return string(files["ProbeTable.lobster"])
}

// The fixed evolution's writer lookup reads the unit's lock for one
// generation call and keeps nothing after it. Generating again from the SAME
// unit after its lock changed reads the new lock (a lookup cached past the
// call would answer with the lock, or the absence of one, it saw first), and
// a unit generation has finished with is collectable (a lookup keyed by the
// unit for ever would hold every unit a long-lived process generated).
func TestLobsterFixedEvolveLockLivesForOneGeneration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	unitLockWrite(t, path, unitLockOldSchema)
	oldEntry, ok := FixedLineageOf(unitLockUnit(t, path), "Root")
	if !ok {
		t.Fatal("no lineage entry for the older Root")
	}
	oldEntry.Writer = nil
	unitLockWrite(t, path, unitLockNewSchema)
	u := unitLockUnit(t, path)

	unlocked := unitLockGenerate(t, u, oldEntry)
	if strings.Contains(unlocked, "RootFixedWriterCheck0") {
		t.Fatal("an unlocked unit emitted a writer check: there is no recorded writer to check")
	}

	unitLockWrite(t, path, unitLockOldSchema)
	if _, _, err := lockfile.Update(unitLockUnit(t, path), []string{path}); err != nil {
		t.Fatalf("lock the older generation: %v", err)
	}
	unitLockWrite(t, path, unitLockNewSchema)
	if _, rewrote, err := lockfile.Update(unitLockUnit(t, path), []string{path}); err != nil || !rewrote {
		t.Fatalf("lock the newer generation: rewrote=%v err=%v", rewrote, err)
	}

	again := unitLockGenerate(t, u, oldEntry)
	fresh := unitLockGenerate(t, unitLockUnit(t, path), oldEntry)
	if !strings.Contains(fresh, "RootFixedWriterCheck0") {
		t.Fatal("a fresh unit under the lock emitted no writer check for the older [1..4] count: the fixture tests nothing")
	}
	if again != fresh {
		t.Fatal("generating again from the same unit after its lock was written did not read the lock: the writer lookup outlived its generation")
	}

	var collected atomic.Bool
	func() {
		gone := unitLockUnit(t, path)
		runtime.SetFinalizer(gone, func(*ir.Unit) { collected.Store(true) })
		unitLockGenerate(t, gone, oldEntry)
	}()
	for i := 0; i < 100 && !collected.Load(); i++ {
		runtime.GC()
		runtime.Gosched()
	}
	if !collected.Load() {
		t.Fatal("a unit stayed reachable after its generation returned: something generation keeps holds it")
	}
}
