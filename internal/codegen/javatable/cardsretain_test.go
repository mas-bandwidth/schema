package javatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCardDescriptorsNameEveryField(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "cards", "Card.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	desc, ok := files["CardDescriptor.java"]
	if !ok {
		t.Fatal("CardDescriptor.java not emitted for tables/cards")
	}

	// The pinned vocabulary, hand-written from tables/cards/Card.schema in
	// declaration order: field name, wire kind (§3), shape (§8.1), bound.
	vocab := []struct {
		name, shape string
		kind, bound int64
	}{
		{"id", "value", 12, 64},
		{"sha", "value", 12, 40},
		{"tier", "value", 30, 0},
		{"repo", "value", 12, 96},
		{"_base", "value", 12, 96},
		{"kind", "value", 12, 24},
		{"priority", "value", 30, 0},
		{"stream", "value", 12, 32},
		{"paths", "list", 14, 0},
		{"_new", "list", 14, 0},
		{"shared", "list", 14, 0},
		{"needs", "list", 14, 0},
		{"test", "value", 12, 192},
		{"done", "blob", 17, 0},
		{"deadline_minutes", "value", 4, 0},
		{"source", "blob", 17, 0},
		{"epoch", "value", 4, 0},
		{"column", "value", 30, 0},
		{"holder", "value", 12, 32},
		{"attempt", "value", 4, 0},
		{"head", "value", 12, 40},
		{"branch", "value", 12, 128},
		{"body", "blob", 17, 0},
		{"extra", "list", 14, 0},
		{"consumers", "list", 14, 0},
	}

	text := string(desc)
	last := 0
	for _, v := range vocab {
		needle := "TableFieldInfo.of(" + `"` + v.name + `", ` + itoa(v.kind) + ", " + `"` + v.shape + `", ` + itoa(v.bound) + "L)"
		at := strings.Index(text[last:], needle)
		if at < 0 {
			t.Fatalf("descriptor for field %s missing or out of order; want %q", v.name, needle)
		}
		last += at + len(needle)
	}
	if n := strings.Count(text, "TableFieldInfo.of("); n != len(vocab) {
		t.Fatalf("descriptor carries %d fields, vocabulary pins %d (a field was added to the declaration without the vocabulary)", n, len(vocab))
	}

	// The unit registry lists every card table descriptor (§8.3).
	reg := string(files["TableRegistry.java"])
	for _, table := range []string{"CardDescriptor", "PathTextDescriptor", "CardIDDescriptor", "KeyValueDescriptor", "ConsumerDescriptor"} {
		if !strings.Contains(reg, table+".tableType()") {
			t.Fatalf("registry does not list %s", table)
		}
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// writeVarPackages generates the two declarations of testdata/cards into their
// own packages under a temp source dir, mirroring the round-trip test.
func writeVarPackages(t *testing.T, srcdir string) {
	t.Helper()
	writeVarPkg := func(pkg, schemaName string) {
		s, err := os.ReadFile(filepath.Join("testdata", "cards", schemaName))
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(unitFrom(t, string(s)))
		if err != nil {
			t.Fatal(err)
		}
		pdir := filepath.Join(srcdir, "..", pkg)
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if strings.HasSuffix(name, ".java") {
				if err := os.WriteFile(filepath.Join(pdir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	writeVarPkg("newpkg", "newer.schema")
	writeVarPkg("oldpkg", "older.schema")
}

// TestCardConstructsRetainUnknownFields (docs/SPEC-TABLES.md §6.6) proves a
// retained load and save keeps the unknown scalar and string fields the older
// declaration cannot name, re-emits them so the newer declaration rereads every
// value equal, and reports the excluded *string node index as loss — never
// retained silently. The save is idempotent.
