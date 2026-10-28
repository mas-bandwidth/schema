// THE MESSAGE FORM IS EVERY TABLE'S (docs/SPEC-TABLES.md §3.3): a fixed root
// rides it by value and a plain (variable) root with its node table first, as
// the generated C++ and C carry it and the conformance corpus pins. The
// corpora carry the `fixed` keyword where the closure is legal (§2.2).
package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMessageFormCarriesEveryTable: the surface that REQUESTS the message
// form — `schema pack --message` and `schema unpack --announce`, both of which
// come through PackMessages / UnpackMessages — writes form `2` for a plain
// `table` exactly as it does for a `fixed table` of the same body: the form is
// not the declaration's to refuse.
func TestMessageFormCarriesEveryTable(t *testing.T) {
	c := New()
	u := unitFromSource(t, "package probe\ntable Loose { n int32 }\nfixed table Tight { n int32 }\n")
	for _, root := range []string{"Loose", "Tight"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, root+".json"), []byte(`{"n":7}`), 0o600); err != nil {
			t.Fatal(err)
		}
		bytes, _, report, err := c.PackMessage(u, root, dir)
		if err != nil || !report.Silent() || len(bytes) == 0 {
			t.Fatalf("%s: the message form refused or reported: %v %+v", root, err, report)
		}
		if bytes[0] != 2 {
			t.Errorf("%s: form byte %d, want 2 — the message form (docs/SPEC-TABLES.md §3.3)", root, bytes[0])
		}
	}
}

// TestTheCorpusCompilesWithTheKeyword: every corpus this branch marked up
// still loads, and the tables that took `fixed` are on the fixed wire while
// the ones left plain are on the variable one. The guard-carrying corpus
// tables dropped their guards to take the keyword (#823), so `RootConfig`,
// `ProfileConfig` and `ArchiveConfig` are pinned here beside the rest;
// `tables/examples/Guarded.schema`'s `Patrol` is what stays plain, and it is
// the one this suite does NOT name.
func TestTheCorpusCompilesWithTheKeyword(t *testing.T) {
	c := New()
	for _, tc := range []struct {
		dir   string
		fixed []string
	}{
		{"../tables/examples", []string{"GunnerConfig", "KeyedConfig", "WeaponConfig", "LoadoutConfig", "RootConfig", "ProfileConfig", "ArchiveConfig"}},
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
