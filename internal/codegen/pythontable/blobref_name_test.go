package pythontable

import (
	"strings"
	"testing"
)

func TestPythonBlobRefName(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"package nametest\ntable BlobRef { x int32 }\n",
		"package nametest\ntype BlobRef { x int32 }\ntable Root { x int32 }\n",
	} {
		u := loadUnit(t, source)
		if _, err := Generate(u); err == nil || !strings.Contains(err.Error(), "BlobRef") {
			t.Errorf("expected named BlobRef collision, got %v", err)
		}
	}
}
