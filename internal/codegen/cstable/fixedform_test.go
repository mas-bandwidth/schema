package cstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func generateSchema(t *testing.T, path string) map[string][]byte {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSuffix(filepath.Base(path), ".schema")
	ast, perrs := parser.Parse(filepath.Base(path), src)
	if len(perrs) != 0 {
		t.Fatalf("parse %s: %v", path, perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: path, Name: filepath.Base(path), Base: base, Bytes: src, AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check %s: %v", path, cerrs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func tableSource(t *testing.T, files map[string][]byte) string {
	t.Helper()
	var b strings.Builder
	for name, data := range files {
		if strings.HasSuffix(name, "Table.cs") {
			b.Write(data)
		}
	}
	got := b.String()
	if got == "" {
		t.Fatal("no Table.cs in generated unit")
	}
	return got
}

func methodFrom(src, needle string) string {
	start := strings.Index(src, needle)
	if start < 0 {
		return ""
	}
	rest := src[start:]
	open := strings.Index(rest, "{")
	if open < 0 {
		return rest
	}
	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:i+1]
			}
		}
	}
	return rest
}

// FU1/FU2 generate, and FuRootFixedLoad is ONE PATH: the hash chooses the plan
// and the record loop always walks it. Empty compile is the skip, not a second
// reader, not an identity memcpy, not `if (identity)`.
func TestFu1Fu2FixedLoadIsOnePath(t *testing.T) {
	fu1 := tableSource(t, generateSchema(t, "../../../test/tables/FU1.schema"))
	fu2 := tableSource(t, generateSchema(t, "../../../test/tables/FU2.schema"))

	for _, src := range []string{fu1, fu2} {
		if !strings.Contains(src, "FuRootFixedLoad(") {
			t.Fatal("FU generate did not emit FuRootFixedLoad")
		}
		if !strings.Contains(src, "FuRootFixedSave(") {
			t.Fatal("FU generate did not emit FuRootFixedSave")
		}
		load := methodFrom(src, "public static long FuRootFixedLoad(\n")
		if load == "" {
			t.Fatal("FuRootFixedLoad span overload was not emitted")
		}
		if !strings.Contains(load, "if (hash != FuRootFixedHash)") {
			t.Error("hash does not choose the plan")
		}
		if !strings.Contains(load, "TableFixedWire.Run(") {
			t.Error("FixedLoad does not walk the winning plan")
		}
		if strings.Contains(load, "if (identity)") || strings.Contains(load, "if identity") {
			t.Error("identity flag still forks the record loop")
		}
		if strings.Count(load, "TableFixedWire.Run(") != 1 {
			t.Error("more than one Run: a second reader is in the load")
		}
		if strings.Contains(load, "BlockCopy") || strings.Contains(load, "MemoryCopy") {
			t.Error("identity memcpy is still a door")
		}
	}
	if !strings.Contains(fu2, "public int Extra") {
		t.Error("FU2 did not emit extra, so a read of FU1 cannot be a compiled plan")
	}
	if strings.Contains(fu1, "DefaultRaw = (ulong)(long)-1") {
		t.Error("signed default -1 is a ulong constant without unchecked; CS0221")
	}
	if !strings.Contains(fu1, "DefaultRaw = unchecked((ulong)(long)-1)") {
		t.Error("FU1 mark = -1 must emit unchecked DefaultRaw")
	}
}
