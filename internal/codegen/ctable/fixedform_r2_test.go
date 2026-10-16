package ctable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateR2DoesNotPanic is vt/c/generator-panic: R2.schema's `union
// Effect` carries a payload-free arm, `pong | was = "ping"` — a field line with
// no type, so v.F is nil and v.Void() is true (docs/SPEC-TABLES.md §2.6). The
// fixed-form emitter must ask no type of that arm. The red at BASE is a nil
// dereference in fixedCollectTypes (internal/codegen/ctable/fixedform.go:70
// reads v.F.Type for every arm); R2.schema is the prerequisite of every
// tables-c-* target, so this is the first blocker of the C variable-tables
// work.
//
// It is a unit test: it reads one repository file and calls the generator in
// process, no clock, socket or service.
func TestGenerateR2DoesNotPanic(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "R2.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(data))

	var files map[string][]byte
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("GenerateLineage panicked on R2.schema: %v", r)
			}
		}()
		files, err = GenerateLineage(u, nil)
	}()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	header := ""
	for name, out := range files {
		if strings.HasSuffix(name, "Table.h") {
			header += string(out)
		}
	}
	if header == "" {
		t.Fatal("no *Table.h was emitted for R2.schema")
	}

	// The fixed writer for Cfg is where the union switch is laid inline; the
	// payload-free arm must be a case that writes nothing under the tag — "a
	// union writes the tag and the taken arm only" (docs/FIXED-FORM-ALGORITHM.md
	// §3.1), and a payload-free arm's whole storage is the tag (§2.6).
	body := fixedWriteBody(t, header, "schema_tblr2_cfg_fixed_write_body_")
	arm, ok := caseArm(body, "case EFFECT_TYPE_PONG:")
	if !ok {
		t.Fatal("the fixed-form writer has no case for the pong arm")
	}
	if strings.Contains(arm, "table_fixed_put") || strings.Contains(arm, "memcpy(") {
		t.Fatalf("the pong arm writes a payload, not only the tag:\n%s", arm)
	}
	if !strings.Contains(arm, "break;") {
		t.Fatalf("the pong arm's case is not closed by break:\n%s", arm)
	}
}

// fixedWriteBody returns the named fixed write body, from its definition to the
// matching closing brace, so an assertion about an arm reads the fixed-form
// writer and never a same-named case in the variable writer. The header also
// carries a semicolon-ended forward declaration of the same name, so an
// occurrence whose first following delimiter is `;` is skipped.
func fixedWriteBody(t *testing.T, header, name string) string {
	t.Helper()
	from := 0
	for {
		i := strings.Index(header[from:], name+"( uint8_t")
		if i < 0 {
			t.Fatalf("no fixed write body named %s", name)
		}
		i += from
		j := strings.Index(header[i:], "{")
		if j >= 0 {
			j += i
			if semi := strings.Index(header[i:], ";"); semi < 0 || semi > j-i {
				depth := 0
				for k := j; k < len(header); k++ {
					switch header[k] {
					case '{':
						depth++
					case '}':
						depth--
						if depth == 0 {
							return header[i : k+1]
						}
					}
				}
				t.Fatalf("unbalanced braces in %s", name)
			}
		}
		from = i + 1
	}
}

// caseArm returns the text between label and the next case or default label.
func caseArm(body, label string) (string, bool) {
	_, after, ok := strings.Cut(body, label)
	if !ok {
		return "", false
	}
	rest := after
	end := len(rest)
	for _, marker := range []string{"case ", "default:"} {
		if j := strings.Index(rest, marker); j >= 0 && j < end {
			end = j
		}
	}
	return rest[:end], true
}
