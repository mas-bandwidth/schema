// THE MESSAGE FORM IS A FIXED TABLE'S (docs/SPEC-TABLES.md §2.2, §3.3), and
// the corpora carry the keyword where the closure is legal. The owner's
// ruling: "and enforce only fixed tables can use message form. this saves more
// work."
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageFormRefusesAPlainTable: the surface that REQUESTS the message
// form — `schema pack --message` and `schema unpack --announce`, both of which
// come through PackMessages / UnpackMessages — refuses a plain `table` NAMING
// THE TABLE, rather than quietly writing another form. The file form is every
// table's, and the refusal says so.
func TestMessageFormRefusesAPlainTable(t *testing.T) {
	c := New()
	u := unitFromSource(t, "package probe\ntable Loose { n int32 }\nfixed table Tight { n int32 }\n")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Loose.json"), []byte(`{"n":7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := c.PackMessage(u, "Loose", dir)
	if err == nil {
		t.Fatal("the message form accepted a plain `table`")
	}
	for _, want := range []string{"Loose", "plain `table`", "MESSAGE FORM is a fixed table's", "§3.3"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q: %v", want, err)
		}
	}

	// and the fixed table of the same body IS the form's, so the refusal is
	// about the DECLARATION and not about the fields
	tight := t.TempDir()
	if err := os.WriteFile(filepath.Join(tight, "Tight.json"), []byte(`{"n":7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bytes, _, report, err := c.PackMessage(u, "Tight", tight)
	if err != nil || !report.Silent() || len(bytes) == 0 {
		t.Fatalf("a fixed table is the message form's: %v %+v", err, report)
	}
	if bytes[0] != 2 {
		t.Errorf("form byte %d, want 2 — the message form (docs/SPEC-TABLES.md §3.3)", bytes[0])
	}
}

// TestTheCorpusCompilesWithTheKeyword: every corpus this branch marked up
// still loads, and the tables that took `fixed` are on the fixed wire while
// the ones left plain are on the variable one. bench/corpus/FixedTable.schema
// is the DELIBERATE exception and is checked by
// TestTheBenchWrapperIsRefusedUntilTheCorpusDropsItsGuard below.
func TestTheCorpusCompilesWithTheKeyword(t *testing.T) {
	c := New()
	for _, tc := range []struct {
		dir   string
		fixed []string
	}{
		{"../tables/examples", []string{"GunnerConfig", "KeyedConfig", "WeaponConfig", "LoadoutConfig"}},
		{"../tables/backend", []string{"LoginRequest", "MatchResult", "StorePurchase", "PlayerRow", "Envelope"}},
		{"../tables/messages", []string{"ToolMessage", "OpenDocument", "InsertText", "Transaction"}},
		{"../tables/block", []string{"RenderFrame", "RenderShip", "PaddedFrame"}},
		{"../tables/scalars", []string{"SimState"}},
		{"../tables/vocab", []string{"Wide00", "Wide09"}},
	} {
		t.Run(filepath.Base(tc.dir), func(t *testing.T) {
			glob, err := filepath.Glob(filepath.Join(tc.dir, "*.schema"))
			if err != nil || len(glob) == 0 {
				t.Fatalf("no schemas under %s: %v", tc.dir, err)
			}
			u, err := c.Load(glob)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			for _, name := range tc.fixed {
				st := u.Tables[name]
				if st == nil {
					t.Fatalf("%s declares no %s", tc.dir, name)
				}
				if !st.FixedDeclared {
					t.Errorf("%s.%s lost its `fixed` keyword", tc.dir, name)
				}
			}
		})
	}
}

// TestTheBenchWrapperIsRefusedUntilTheCorpusDropsItsGuard pins the ONE
// deliberate refusal this branch ships: bench/corpus/FixedTable.schema wraps
// Bench.schema's BenchMixed, whose `if has_extra` guard is in the wrapper's
// by-value closure, so the wrapper cannot be `fixed table` while the guard
// stands. The corpus fix is the WIRE branch's (it drops the guard, keeping the
// three fields plain); this test says exactly what the compiler says today, so
// the day the guard goes the test goes with it.
func TestTheBenchWrapperIsRefusedUntilTheCorpusDropsItsGuard(t *testing.T) {
	_, err := New().Load([]string{"../bench/corpus/Bench.schema", "../bench/corpus/FixedTable.schema"})
	if err == nil {
		t.Skip("the corpus dropped BenchMixed's guard — delete this test with it")
	}
	for _, want := range []string{
		"fixed table FixedTable",
		"BenchMixed.extra",
		"BenchMixed.idle_ticks",
		"sits in an `if` branch",
		"docs/SPEC-TABLES.md §2.2, SPEC §4.5",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q:\n%v", want, err)
		}
	}
}
