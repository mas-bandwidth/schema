// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg ruby`; docs/PORT-RUBY.md is the port's design.
package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type rubyTarget struct{}

// rubyWideTableKinds is set by compiler/carries_ruby_tablewire.go once the
// fixed-point and 128-bit table kinds are carried.
var rubyWideTableKinds bool

func (rubyTarget) Names() []string { return []string{"ruby"} }

func (rubyTarget) ValidateOptions(u *ir.Unit, opts Options) error {
	surfaces, err := rubytable.ParseSurfaces(opts["surface"])
	if err != nil {
		return err
	}
	if err := rubytable.ValidateSurfaces(u, surfaces); err != nil {
		return err
	}
	return rubytable.ValidateAvailableSurfaces(surfaces)
}

func (rubyTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return rubyGenerate(u, opts, nil)
}

func (rubyTarget) GenerateLineage(u *ir.Unit, opts Options, lineage *FixedLineage) (map[string][]byte, error) {
	return rubyGenerate(u, opts, lineage)
}

func rubyGenerate(u *ir.Unit, opts Options, lineage *FixedLineage) (map[string][]byte, error) {
	surfaces, err := rubytable.ParseSurfaces(opts["surface"])
	if err != nil {
		return nil, err
	}
	if err := rubytable.ValidateSurfaces(u, surfaces); err != nil {
		return nil, err
	}
	if err := rubytable.ValidateAvailableSurfaces(surfaces); err != nil {
		return nil, err
	}
	if !rubyWideTableKinds || len(u.Tables) == 0 {
		if err := ir.RefuseWideTableKinds(u, "ruby"); err != nil {
			return nil, err
		}
	}
	if err := refuseUnported(u, "ruby"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "ruby"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "ruby"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "ruby"); err != nil {
		return nil, err
	}
	if len(u.Tables) > 0 {
		return rubytable.GenerateSurfaceLineage(u, surfaces, rubyTableLineage(u, lineage))
	}
	if err := refuseTables(u, "ruby"); err != nil {
		return nil, err
	}
	out, err := ruby.Generate(u)
	if err != nil {
		return nil, err
	}
	if surfaces.View && opts["surface"] != "" {
		if err := rubytable.AddView(u, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func rubyTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]rubytable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]rubytable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		for _, e := range lineage.Entries(st.Name) {
			out[st.Name] = append(out[st.Name], rubytable.FixedLineageEntry{
				Wire:             e.Wire,
				Layout:           e.Layout,
				Record:           e.Record,
				Retired:          e.Retired,
				Reason:           e.Reason,
				Digest:           e.Digest,
				Writer:           e.Writer,
				LegacyLayoutOnly: e.LegacyLayoutOnly,
			})
		}
	}
	return out
}

func init() {
	registerBuiltin(rubyTarget{}, true, true, true, true)
	registerListCarrier("ruby")
	registerMapCarrier("ruby")
}
