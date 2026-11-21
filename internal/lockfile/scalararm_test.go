// A union's SCALAR ARM under the monotone law, and `bits(N)` beside a plain
// unsigned integer (docs/FIXED-FORM-ALGORITHM.md §5.1 WIDENS).
//
// A scalar arm — `pid uint32 | min = 0, max = 1000`, `note string(24)`,
// `marks [..3]uint16` — has no block of its own, so the lock records its facts
// on the arm line and judges them with the field's own law: a narrowed range is
// refused, and a wider integer, a wider capacity or a larger bound is a
// widening `schema lock` takes. A rendering-9 lock, which recorded such an arm
// by its spelling alone, still reads and is carried forward.
//
// `bits(N)` and a plain unsigned integer of the same storage kind are compared
// by their EFFECTIVE width: a plain uintW is N = W, so `uint16 -> bits(9)` is
// "bits narrowed (16 -> 9)" and `bits(9) -> uint16` is a widening.
package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// originWith is a union with one record arm and the scalar arms given, held by
// the one fixed table every row edits.
func originWith(arms string) string {
	return rowWith("type User\n{\n    name string(16)\n}\n\nunion Origin\n{\n    user User\n"+arms+"\n}",
		"    origin Origin\n    seq uint32")
}

// lockRefusesToo is the other half of every LOCK-REFUSES row: `schema lock`
// refuses what the check refuses, with the same rule phrase.
func lockRefusesToo(t *testing.T, before, after string, want ...string) {
	t.Helper()
	dir, paths := fixture(t, before)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatalf("locking the `before` schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := lockfile.Update(load(t, paths), paths)
	if err == nil {
		t.Fatalf("`schema lock` must refuse a narrowing")
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("the refusal must name %q:\n%s", w, err)
		}
	}
}

func TestLockScalarArmNarrowedRefuses(t *testing.T) {
	t.Parallel()
	before := originWith("    pid uint32 | min = 0, max = 1000\n    note string(24)\n    delta int32 | min = -100, max = 100")
	cases := []struct {
		name  string
		after string
		want  []string
	}{
		{"range max narrowed", originWith("    pid uint32 | min = 0, max = 10\n    note string(24)\n    delta int32 | min = -100, max = 100"),
			[]string{"union Origin", "arm 2, pid", "range narrowed ([0, 1000] -> [0, 10])"}},
		{"signed range min narrowed", originWith("    pid uint32 | min = 0, max = 1000\n    note string(24)\n    delta int32 | min = -10, max = 100"),
			[]string{"union Origin", "arm 4, delta", "range narrowed ([-100, 100] -> [-10, 100])"}},
		{"integer narrowed", originWith("    pid uint16 | min = 0, max = 1000\n    note string(24)\n    delta int32 | min = -100, max = 100"),
			[]string{"union Origin", "arm 2, pid", "narrowed (uint32 -> uint16)"}},
		{"capacity narrowed", originWith("    pid uint32 | min = 0, max = 1000\n    note string(16)\n    delta int32 | min = -100, max = 100"),
			[]string{"union Origin", "arm 3, note", "capacity narrowed (24 -> 16)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rowRefuses(t, before, c.after, c.want...)
			lockRefusesToo(t, before, c.after, c.want...)
		})
	}
}

func TestLockScalarArmWidenAllows(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		before, after string
	}{
		{"uint32 to uint64", originWith("    pid uint32\n    note string(24)"), originWith("    pid uint64\n    note string(24)")},
		{"string(24) to string(32)", originWith("    pid uint32\n    note string(24)"), originWith("    pid uint32\n    note string(32)")},
		{"range widened", originWith("    pid uint32 | min = 0, max = 10\n    note string(24)"), originWith("    pid uint32 | min = 0, max = 1000\n    note string(24)")},
		{"bound widened", originWith("    marks [..3]uint16"), originWith("    marks [..4]uint16")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rowAllows(t, c.before, c.after, "Row")
		})
	}
}

func TestLockBitsAgainstPlainUnsigned(t *testing.T) {
	t.Parallel()
	t.Run("uint16 to bits(9) refused", func(t *testing.T) {
		t.Parallel()
		rowRefuses(t, rowTable("    v uint16"), rowTable("    v bits(9)"),
			"fixed table Row", "field v", "bits narrowed (16 -> 9)")
		lockRefusesToo(t, rowTable("    v uint16"), rowTable("    v bits(9)"), "bits narrowed (16 -> 9)")
	})
	t.Run("uint8 to bits(3) refused", func(t *testing.T) {
		t.Parallel()
		rowRefuses(t, rowTable("    v uint8"), rowTable("    v bits(3)"),
			"fixed table Row", "field v", "bits narrowed (8 -> 3)")
	})
	t.Run("scalar arm uint16 to bits(9) refused", func(t *testing.T) {
		t.Parallel()
		rowRefuses(t, originWith("    pid uint16"), originWith("    pid bits(9)"),
			"union Origin", "arm 2, pid", "bits narrowed (16 -> 9)")
	})
	t.Run("bits(9) to uint16 allowed", func(t *testing.T) {
		t.Parallel()
		rowAllows(t, rowTable("    v bits(9)"), rowTable("    v uint16"), "Row")
	})
	t.Run("bits(9) to bits(12) allowed", func(t *testing.T) {
		t.Parallel()
		rowAllows(t, rowTable("    v bits(9)"), rowTable("    v bits(12)"), "Row")
	})
	t.Run("bits(12) to bits(9) refused", func(t *testing.T) {
		t.Parallel()
		rowRefuses(t, rowTable("    v bits(12)"), rowTable("    v bits(9)"),
			"fixed table Row", "field v", "bits narrowed (12 -> 9)")
	})
}

// TestLockScalarArmRendering9StillReads holds the salvage of an older rendering
// (§11.8): a lock written by the rendering-9 compiler, which recorded a scalar
// arm by its spelling alone, still parses, round-trips and passes the check;
// `schema lock` carries it forward to the current rendering with its lineage
// untouched and the arm's facts written down, after which a narrowed arm range
// is refused.
func TestLockScalarArmRendering9StillReads(t *testing.T) {
	t.Parallel()
	dirV9 := filepath.Join("testdata", "scalar-arm-v9")
	source, err := os.ReadFile(filepath.Join(dirV9, "Arm.schema"))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(dirV9, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	old, err := lockfile.Parse("scalar-arm-v9", saved)
	if err != nil {
		t.Fatalf("a rendering-9 lock must still parse: %v", err)
	}
	if old.Version != 9 {
		t.Fatalf("historical fixture moved: rendering %d", old.Version)
	}
	if old.Text() != string(saved) {
		t.Fatal("parsing changed the rendering-9 fixture's bytes")
	}
	if !strings.Contains(string(saved), "arm pid payload=uint32\n") {
		t.Fatal("the fixture must hold a scalar arm recorded by spelling alone")
	}

	dir, paths := fixture(t, string(source))
	path := filepath.Join(dir, lockfile.FileName)
	if err := os.WriteFile(path, saved, 0o644); err != nil {
		t.Fatal(err)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("a rendering-9 lock over its own declaration must pass the check: %v", errs)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` must carry the rendering-9 lock forward: rewrote=%v err=%v", rewrote, err)
	}
	now := readLock(t, path)
	if now.Version != lockfile.Version {
		t.Fatalf("relocked at rendering %d, want %d", now.Version, lockfile.Version)
	}
	was, is := old.Table("Edit").Lineage, now.Table("Edit").Lineage
	if len(was) != len(is) || lockfile.LineageHash(was) != lockfile.LineageHash(is) {
		t.Fatalf("carrying the lock forward must not touch its lineage: %d entries -> %d", len(was), len(is))
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "arm pid payload=uint32 kind=8 width=4 default=0 min=0 max=1000\n") {
		t.Fatalf("the relocked file must record the scalar arm's facts:\n%s", text)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Fatalf("the relocked file is current: %v", errs)
	}
	narrowed := strings.Replace(string(source), "max = 1000", "max = 10", 1)
	if err := os.WriteFile(paths[0], []byte(narrowed), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, lockfile.Check(load(t, paths), paths), "union Origin", "arm 2, pid", "range narrowed ([0, 1000] -> [0, 10])")
}
