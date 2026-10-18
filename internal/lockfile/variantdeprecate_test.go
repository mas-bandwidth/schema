// A deprecated ENUM VARIANT is deprecation one level in (docs/SPEC-TABLES.md
// §2.15): the lock records the marker, adding it widens, and taking it off is
// refused by the same one-way rule the field's own marker keeps (§21.1).
package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

func deprecatedTier() string {
	return strings.Replace(tierEnum, "    Silver\n", "    Silver | deprecated\n", 1)
}

func tierRow(decl string) string {
	return rowWith(decl, "    tier Tier")
}

// TestLockVariantUndeprecateRefuses: the lock carries `| deprecated` on a
// variant and the declaration does not. Deprecation is ONE WAY, and the
// refusal names the enum and the variant, worded as the field's is.
func TestLockVariantUndeprecateRefuses(t *testing.T) {
	t.Parallel()
	rowRefuses(t, tierRow(deprecatedTier()), tierRow(tierEnum),
		"enum Tier", "Silver", "undeprecated", "deprecation is ONE WAY")

	// REVERSED WITNESS: the same entry, a lock where the variant was NEVER
	// deprecated. The refusal above is the rule firing, not the comparison
	// refusing every enum.
	t.Run("witness: a variant never deprecated is not refused", func(t *testing.T) {
		t.Parallel()
		if errs := lockThen(t, tierRow(tierEnum), tierRow(tierEnum)); len(errs) != 0 {
			t.Fatalf("a live variant that was never deprecated must not be refused: %v", errs)
		}
	})
}

// TestLockVariantDeprecateWidens: on a locked enum, adding `| deprecated` to a
// variant is allowed. The check refuses it as stale until the lock carries it,
// `schema lock` writes it, and the recorded variant keeps its place.
func TestLockVariantDeprecateWidens(t *testing.T) {
	t.Parallel()
	dir, paths := fixture(t, tierRow(tierEnum))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatalf("locking the live schema: %v", err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(tierRow(deprecatedTier())), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, err := range lockfile.Check(load(t, paths), paths) {
		if !strings.Contains(err.Error(), "write it with `schema lock`") {
			t.Fatalf("a deprecation is a widening the record must carry, not a break: %v", err)
		}
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("`schema lock` takes the deprecation: rewrote=%v err=%v", rewrote, err)
	}
	lk := readLock(t, path)
	list := lk.List("Tier")
	if list == nil || len(list.Deprecated) != len(tierEnumVariants()) {
		t.Fatalf("the lock records one deprecation flag per variant: %+v", list)
	}
	if !list.Deprecated[1] || list.Deprecated[0] || list.Deprecated[2] {
		t.Errorf("only Silver is deprecated in the lock: %v", list.Deprecated)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Errorf("the widened lock is current: %v", errs)
	}
}

func tierEnumVariants() []string {
	return []string{"Bronze", "Silver", "Gold"}
}
