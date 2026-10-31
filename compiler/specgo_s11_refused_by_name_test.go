// TestSpecGoS11RefusedByName holds docs/SPEC-TABLES.md §11 to the rows the
// spec-vs-Go review of 2026-10-18 found: each row states what the Go backend
// already does, and none still carries the sentence it replaced.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecGoS11RefusedByName(t *testing.T) {
	t.Parallel()
	page, err := os.ReadFile(filepath.Join("..", "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatalf("SPEC-TABLES.md: %v", err)
	}
	text := string(page)
	start := strings.Index(text, "## 11. Refused by name")
	end := strings.Index(text, "## 12. The expressiveness gate")
	if start < 0 || end <= start {
		t.Fatal("§11 or §12 was not found in the page — the scan, not the page, is what broke")
	}
	section := text[start:end]

	cases := []struct {
		name  string
		want  []string
		stale []string
	}{
		{
			name:  "variable-length wire surface",
			want:  []string{"Java, JavaScript and Rust backends", "`<Base>Table.java`"},
			stale: []string{"Elixir, Go,", "`<Base>Table.go`"},
		},
		{
			name:  "table-closure union",
			want:  []string{"carried by C, C++, C# and Go", "backend but C, C++, C# and Go"},
			stale: []string{"carried by C and C++", "backend but C and C++"},
		},
		{
			name:  "array of unions",
			want:  []string{"the remaining ports'", "under every backend but C, C++"},
			stale: []string{"under every backend but C++"},
		},
		{
			name:  "byte buffers",
			want:  []string{"C, C++, C#, Go, Elixir, JavaScript and Rust carry `*bytes`", "Dart and Java carry `*string` only"},
			stale: []string{"under every backend but C++", "`*wstring` is the third spelling"},
		},
		{
			name:  "optional fields",
			want:  []string{"`?T` and a by-value `?[N]T` over a variable closure are legal on Go, C", "an optional COUNTED array or a MAP VALUE over a"},
			stale: []string{"on any value whose closure is VARIABLE", "is legal on Go"},
		},
		{
			name:  "Go message paragraph",
			want:  []string{"Go's message form is fixed roots only", "no variable root gets a message verb"},
			stale: []string{"spells message region sizing", "or a slice of root pointers plus one caller-owned region for a variable root"},
		},
		{
			name: "on an arm",
			want: []string{"TABLES, `[..N]T` or `[N]T`", "an arm being one table"},
		},
		{
			name: "default read refusals",
			want: []string{"`blob_shared`", "`non_finite_float`", "a blob shared by two slots (§2.5, `blob_shared`)"},
		},
		{
			name: "compile-time refusals with no line",
			want: []string{"This section is not exhaustive", "`timestamp` outside a bare\n  table field body"},
		},
		{
			name: "central Go-first refusal",
			want: []string{"The central Go-first refusal", "a one-sided integer range (§4.3)", "testgen (§16.11)"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, want := range c.want {
				if !strings.Contains(section, want) {
					t.Errorf("§11 does not hold the row %q", want)
				}
			}
			for _, stale := range c.stale {
				if strings.Contains(section, stale) {
					t.Errorf("§11 still holds the stale text %q", stale)
				}
			}
		})
	}
}
