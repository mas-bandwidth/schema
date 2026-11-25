package tablepack_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testvolume"
)

// foldingWork is a fresh, empty directory on a volume that takes Probe and
// probe for one entry, as APFS on macOS and NTFS on Windows do: on Linux the
// fleet's case-folding scratch, or $SCHEMA_CASEFOLD_DIR (testvolume.Folding).
// Where there is none the test fails, naming it.
func foldingWork(t *testing.T) string {
	t.Helper()
	return testvolume.Folding(t)
}
