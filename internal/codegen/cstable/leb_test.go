package cstable

import (
	"strings"
	"testing"
)

func TestCsharpLebFastPathShape(t *testing.T) {
	requiredPatterns := []string{
		"if (v < 128) { Byte((byte)v); return; }",
		"if ((uint)Offset < (uint)Buffer.Length)",
		"byte b0 = Buffer[Offset];",
		"if (b0 < 128)",
		"Offset++;",
		"v = b0;",
		"return true;",
		"for (int i = 0; i < 10; i++)",
		"if (i == 9 && b > 1)",
		"if (b < 128) { if (i == 0 || b != 0) { return true; }",
	}
	for _, pattern := range requiredPatterns {
		if !strings.Contains(tableWireSource, pattern) {
			t.Fatalf("tableWireSource missing expected LEB pattern: %q", pattern)
		}
	}
}
