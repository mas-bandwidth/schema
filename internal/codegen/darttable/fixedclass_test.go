package darttable

import (
	"path/filepath"
	"testing"
)

// TestFixedClassOnTableWireDart pins form 1 for fixed-table closures to the
// committed Go oracle bytes (docs/SPEC-TABLES.md §3, §5). W1/W2 carry
// string, bytes and flags defaults plus a renamed table; R1/R2 carry renamed
// enum variants, union arms and type fields; V1/V2 cover fixed-class evolution.
func TestFixedClassOnTableWireDart(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	for _, c := range []struct{ unit, schema string }{
		{"tblv1", "V1"},
		{"tblv2", "V2"},
		{"tblw1", "W1"},
		{"tblw2", "W2"},
		{"tblr1", "R1"},
		{"tblr2", "R2"},
	} {
		c := c
		t.Run(c.unit, func(t *testing.T) {
			t.Parallel()
			rows := manifestRows(t, repo, c.unit)
			if len(rows) == 0 {
				t.Fatalf("no committed Go oracle rows for %s", c.unit)
			}
			u := loadUnit(t, filepath.Join(repo, "test", "tables", c.schema+".schema"))
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("Generate fixed-class form 1: %v", err)
			}
			wireName := runtimeHome(u) + "Wire.dart"
			wire := files[wireName]
			if len(wire) == 0 {
				t.Fatalf("no %s form-1 codec", wireName)
			}
			resaveOracleRows(t, wireName, wire, repo, rows)
		})
	}
}
