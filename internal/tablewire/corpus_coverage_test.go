package tablewire_test

import (
	"errors"
	"github.com/mas-bandwidth/schema/v2/ir"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestCorpusEmptyRoundTrip(t *testing.T) {
	t.Parallel()
	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	tables := []string{
		"Patrol", "TeamConfig", "GunnerConfig", "TurretConfig", "HullConfig",
		"KeyedConfig", "ArchiveConfig", "GunnerSettings", "ShipEntry",
		"GlobalSettings", "PackConfig", "RangedSigned", "RangedUnsigned",
		"RangedWidths", "RootConfig", "WeaponConfig", "LoadoutConfig",
		"ProfileConfig", "WideBlob",
	}
	for _, name := range tables {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := m.Lookup(name)
			inst := m.New(st)
			wire, err := tablewire.Encode(m, inst)
			if st.FixedDeclared && ir.TableFixedTypeBytes(st) > ir.TableFixedRecordMaxBytes {
				var refusal *tablewire.FixedRefusal
				if !errors.As(err, &refusal) || refusal.Reason != "layout_unsupported" || len(wire) != 0 {
					t.Fatalf("oversized fixed corpus root must have no file form: %x %v", wire, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			back := m.New(m.Lookup(name))
			var r tabletext.Report
			ok, err := tablewire.Decode(m, back, wire, &r)
			if err != nil || !ok {
				t.Fatalf("decode stopped: %v %+v", err, r)
			}
		})
	}
}
