package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/irtest"
)

func TestPythonCookBlobRefName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src   string
		table bool
	}{
		{"package nametest\ntable Placeholder { x int32 }\n", true},
		{"package nametest\ntype Placeholder { x int32 }\ntable Root { x int32 }\n", false},
	} {
		checkRefuses(t, strings.ReplaceAll(tc.src, "Placeholder", "BlobRef"), "the Python target cannot express")
		u := loadUnit(t, tc.src)
		if tc.table {
			renameTable(t, u, "Placeholder", "BlobRef")
		} else {
			irtest.RenameStruct(t, u, "Placeholder", "BlobRef")
		}
		if _, err := Generate(u); err == nil || !strings.Contains(err.Error(), "BlobRef") {
			t.Errorf("expected named BlobRef collision, got %v", err)
		}
	}
}
