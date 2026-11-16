//go:build schema_leg_python

package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// pythonTableLineage hands the lock's whole record size to the backend without
// another hash-width addition (docs/FIXED-FORM-ALGORITHM.md §5.2).
func pythonTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]pythontable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]pythontable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		for _, e := range lineage.Entries(st.Name) {
			out[st.Name] = append(out[st.Name], pythontable.FixedLineageEntry{Wire: e.Wire, Layout: e.Layout, Record: e.Record, Retired: e.Retired, Reason: e.Reason, Digest: e.Digest, Writer: e.Writer})
		}
	}
	return out
}
