// Refusal of retired field, table, and enum-value name reuse across
// tables.baseline and schema.lock (docs/VERSIONING.md, docs/SPEC.md §4.1, §4.6, §4.7).
package check

import (
	"os"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func (c *checker) checkRetiredNames() {
	var paths []string
	for _, f := range c.files {
		if f.Path != "" {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return
	}
	c.checkBaselineRetiredNames(paths)
	c.checkLockRetiredNames(paths)
}

func (c *checker) declPos(name string) ast.Pos {
	if d, ok := c.astDecls[name]; ok {
		return d.DeclPos()
	}
	for _, st := range c.unit.Tables {
		if st.WasName == name {
			if d, ok := c.astDecls[st.Name]; ok {
				return d.DeclPos()
			}
		}
	}
	if len(c.files) > 0 && c.files[0].AST != nil {
		return c.files[0].AST.PkgPos
	}
	return ast.Pos{}
}

func (c *checker) declaresOrAliasesName(name string) bool {
	if _, ok := c.astDecls[name]; ok {
		return true
	}
	for _, st := range c.unit.Tables {
		if st.WasName == name {
			return true
		}
	}
	return false
}

func (c *checker) closureStruct(wireOrDeclName string) *ir.Struct {
	if st, ok := c.unit.Tables[wireOrDeclName]; ok {
		return st
	}
	for _, st := range c.unit.Tables {
		if st.WireName() == wireOrDeclName {
			return st
		}
	}
	if st, ok := c.unit.Structs[wireOrDeclName]; ok {
		return st
	}
	return nil
}

func findBaselineTable(u *baseline.Unit, name string) *baseline.Table {
	for i := range u.Tables {
		t := &u.Tables[i]
		if t.Name == name || (t.Declared != "" && t.Declared == name) {
			return t
		}
	}
	return nil
}

func findBaselineField(t *baseline.Table, name string, id uint64) *baseline.Field {
	if t == nil {
		return nil
	}
	for i := range t.Fields {
		f := &t.Fields[i]
		if f.Name == name || (id != 0 && f.Id == id) {
			return f
		}
	}
	return nil
}

func findBaselineEnum(u *baseline.Unit, name string) *baseline.Enum {
	for i := range u.Enums {
		if u.Enums[i].Name == name {
			return &u.Enums[i]
		}
	}
	return nil
}

func findBaselineUnion(u *baseline.Unit, name string) *baseline.Union {
	for i := range u.Unions {
		if u.Unions[i].Name == name {
			return &u.Unions[i]
		}
	}
	return nil
}

func equalBaselineTable(a, b *baseline.Table) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Name != b.Name || a.Declared != b.Declared || len(a.Fields) != len(b.Fields) {
		return false
	}
	for i := range a.Fields {
		af, bf := a.Fields[i], b.Fields[i]
		if af.Name != bf.Name || af.Id != bf.Id || !slices.Equal(af.Tokens, bf.Tokens) {
			return false
		}
	}
	return true
}

func (c *checker) checkBaselineRetiredNames(paths []string) {
	bpath, ok, err := baseline.Locate(paths)
	if err != nil || !ok {
		return
	}
	data, err := os.ReadFile(bpath)
	if err != nil {
		return
	}
	base, err := baseline.Parse(bpath, data)
	if err != nil || base.Package != c.unit.Package || len(base.Retired) == 0 {
		return
	}
	live := baseline.Render(c.unit)

	// Enums reached by a packet `type` or a `fixed table` encode variants by
	// dense ordinal on the wire.
	positionalEnum := map[string]bool{}
	for _, lv := range lockfile.Render(c.unit).Values {
		if lv.Decl == lockfile.DeclEnum {
			positionalEnum[lv.Name] = true
		}
	}
	for _, st := range c.unit.Structs {
		for _, f := range st.Fields {
			if f.Type.Kind == ir.TNamed {
				if en, ok := f.Type.Ref.(*ir.Enum); ok {
					positionalEnum[en.Name] = true
				}
			}
		}
	}

	refusedTables := map[string]bool{}
	for _, r := range base.Retired {
		switch r.Vocab {
		case "table":
			tableName := r.Name
			if refusedTables[tableName] || !c.declaresOrAliasesName(tableName) {
				continue
			}
			bt := findBaselineTable(base, tableName)
			lt := findBaselineTable(live, tableName)
			if r.Revived != "" && equalBaselineTable(bt, lt) {
				continue
			}
			refusedTables[tableName] = true
			c.errf(c.declPos(tableName), "table %s (id=0x%016x) is recorded as retired in %s — reusing a retired table name in the same scope with a new meaning (or without acknowledging a same-meaning resurrection via `schema tables-baseline --update --reason \"...\"`) is refused (docs/VERSIONING.md)",
				tableName, r.Id, baseline.FileName)

		case "field":
			tableName, fieldName, ok := strings.Cut(r.Name, ".")
			if !ok {
				continue
			}
			bt := findBaselineTable(base, tableName)
			lt := findBaselineTable(live, tableName)
			// If the enclosing table itself left the baseline when its fields
			// were retired, and the table name is now re-declared or aliased in
			// the unit without a matching acknowledged resurrection, refuse the
			// retired table reuse by name.
			if bt == nil && c.declaresOrAliasesName(tableName) && !refusedTables[tableName] {
				refusedTables[tableName] = true
				c.errf(c.declPos(tableName), "table %s is recorded as retired in %s (retired %s id=0x%016x) — reusing a retired table name in the same scope with a new meaning (or without acknowledging a same-meaning resurrection via `schema tables-baseline --update --reason \"...\"`) is refused (docs/VERSIONING.md)",
					tableName, baseline.FileName, r.Name, r.Id)
			}

			lf := findBaselineField(lt, fieldName, r.Id)
			st := c.closureStruct(tableName)
			var irField *ir.Field
			if st != nil {
				for _, f := range st.Fields {
					if f.Name == fieldName || f.WasName == fieldName || ir.TableFieldWireId(f) == r.Id {
						irField = f
						break
					}
				}
			}
			if lf == nil && irField == nil {
				continue
			}
			bf := findBaselineField(bt, fieldName, r.Id)
			if r.Revived != "" &&
				bf != nil && lf != nil &&
				bf.Name == fieldName && lf.Name == fieldName &&
				bf.Id == r.Id && lf.Id == r.Id &&
				slices.Equal(bf.Tokens, lf.Tokens) {
				continue
			}
			c.errf(c.declPos(tableName), "field %s (id=0x%016x) is recorded as retired in %s — reusing a retired field name in the same scope with a new type or meaning (or without acknowledging a same-meaning resurrection via `schema tables-baseline --update --reason \"...\"`) is refused (docs/VERSIONING.md)",
				r.Name, r.Id, baseline.FileName)

		case "enum-variant":
			enumName, variantName, ok := strings.Cut(r.Name, ".")
			if !ok {
				continue
			}
			en := c.unit.Enums[enumName]
			if en == nil {
				continue
			}
			matchedIdx := -1
			for i := range en.Variants {
				var was string
				if i < len(en.Was) {
					was = en.Was[i]
				}
				if en.Variants[i] == variantName || was == variantName || ir.TableWireId(en.VariantWireName(i)) == r.Id {
					matchedIdx = i
					break
				}
			}
			if matchedIdx < 0 {
				continue
			}
			var was string
			if matchedIdx < len(en.Was) {
				was = en.Was[matchedIdx]
			}
			wireId := ir.TableWireId(en.VariantWireName(matchedIdx))
			be := findBaselineEnum(base, enumName)
			baseOrd := -1
			var baseVar *baseline.Variant
			if be != nil {
				for i := range be.Variants {
					if be.Variants[i].Name == variantName && be.Variants[i].Id == r.Id {
						baseOrd = i
						baseVar = &be.Variants[i]
						break
					}
				}
			}
			sameMeaning := r.Revived != "" &&
				en.Variants[matchedIdx] == variantName &&
				was == "" &&
				wireId == r.Id &&
				baseVar != nil &&
				baseVar.Was == "" &&
				(!positionalEnum[enumName] || matchedIdx == baseOrd)
			if sameMeaning {
				continue
			}
			c.errf(c.declPos(enumName), "enum variant %s (id=0x%016x) is recorded as retired in %s — reusing a retired enum value name in the same scope with a new meaning (or without acknowledging a same-meaning resurrection via `schema tables-baseline --update --reason \"...\"`) is refused (docs/VERSIONING.md)",
				r.Name, r.Id, baseline.FileName)

		case "union-arm":
			unionName, armName, ok := strings.Cut(r.Name, ".")
			if !ok {
				continue
			}
			lu := findBaselineUnion(live, unionName)
			if lu == nil {
				continue
			}
			var la *baseline.Field
			for i := range lu.Arms {
				if lu.Arms[i].Name == armName || lu.Arms[i].Id == r.Id {
					la = &lu.Arms[i]
					break
				}
			}
			if la == nil {
				continue
			}
			bu := findBaselineUnion(base, unionName)
			var ba *baseline.Field
			if bu != nil {
				for i := range bu.Arms {
					if bu.Arms[i].Name == armName && bu.Arms[i].Id == r.Id {
						ba = &bu.Arms[i]
						break
					}
				}
			}
			if r.Revived != "" && ba != nil && la.Name == armName && la.Id == r.Id && slices.Equal(ba.Tokens, la.Tokens) {
				continue
			}
			c.errf(c.declPos(unionName), "union arm %s (id=0x%016x) is recorded as retired in %s — reusing a retired union arm name in the same scope with a new meaning is refused (docs/VERSIONING.md)",
				r.Name, r.Id, baseline.FileName)
		}
	}
}

func equalLockEntryMeaning(want, got lockfile.Entry) bool {
	want.Deprecated = got.Deprecated
	return want == got
}

func (c *checker) checkLockRetiredNames(paths []string) {
	locked, ok, err := lockfile.Open(paths)
	if err != nil || !ok || locked == nil || locked.Package != c.unit.Package {
		return
	}
	liveLock := lockfile.Render(c.unit)

	// 1. Whole tables marked `retired` in `schema.lock`.
	retiredTables := map[string]*lockfile.Table{}
	for i := range locked.Tables {
		lk := &locked.Tables[i]
		if !lk.Retired {
			continue
		}
		retiredTables[lk.Name] = lk
		if c.declaresOrAliasesName(lk.Name) {
			c.errf(c.declPos(lk.Name), "table %s is recorded as retired in %s — reusing a retired table name in the same scope is refused; table retirement in %s is one-way, so give a replacement declaration a new name (docs/VERSIONING.md)",
				lk.Name, lockfile.FileName, lockfile.FileName)
		}
	}

	// 2. Fields marked `deprecated` (retired in place) in `schema.lock`.
	for i := range locked.Tables {
		lk := &locked.Tables[i]
		if lk.Retired {
			continue
		}
		st := c.closureStruct(lk.Name)
		if st == nil {
			continue
		}
		lv := liveLock.Table(lk.Name)
		for entryIdx, want := range lk.Entries {
			if !want.Deprecated {
				continue
			}
			for fIdx, f := range st.Fields {
				if f.Name != want.Name && f.WasName != want.Name && ir.TableFieldWireId(f) != want.Id {
					continue
				}
				// Keeping the retired field at the same slot with the exact
				// same type, width, offset, range, bound, and default is
				// allowed here (a bare undeprecation with identical meaning is
				// reported by lockfile.Check as "undeprecated — deprecation is ONE WAY").
				if fIdx == entryIdx &&
					f.Name == want.Name &&
					f.WasName == "" &&
					lv != nil &&
					entryIdx < len(lv.Entries) &&
					equalLockEntryMeaning(want, lv.Entries[entryIdx]) {
					continue
				}
				c.errf(c.declPos(lk.Name), "%s %s: field %s (id=0x%016x) is recorded as retired (deprecated) in %s — reusing a retired field name in the same scope with a new type or meaning is refused (docs/VERSIONING.md)",
					lk.Decl, lk.Name, want.Name, want.Id, lockfile.FileName)
			}
		}
	}

	// 3. Enum values in `schema.lock`: determine which locked enums belonged
	// only to retired fixed tables (orphaned when the table was retired), and
	// check both orphaned and live locked enums for retired variant reuse with
	// a new meaning (shifted ordinal on a retired enum, or re-bound wire id via `was`).
	keepValues := map[string]bool{}
	var walkLocked func(name string)
	walkLocked = func(name string) {
		if keepValues[name] {
			return
		}
		keepValues[name] = true
		if t := locked.Table(name); t != nil {
			for _, e := range t.Entries {
				if !e.Deprecated {
					if e.HeldName != "" {
						walkLocked(e.HeldName)
					}
					if e.KeyName != "" {
						walkLocked(e.KeyName)
					}
				}
			}
		}
		if v := locked.List(name); v != nil {
			for _, p := range v.Payloads {
				if at := strings.Index(p, "@"); at > 0 {
					walkLocked(p[:at])
				}
			}
		}
	}
	for i := range locked.Tables {
		lk := &locked.Tables[i]
		if lk.Decl == lockfile.DeclFixedTable && !lk.Retired {
			walkLocked(lk.Name)
		}
	}

	for i := range locked.Values {
		lkVal := &locked.Values[i]
		if lkVal.Decl != lockfile.DeclEnum {
			continue
		}
		en := c.unit.Enums[lkVal.Name]
		if en == nil {
			continue
		}
		retiredEnum := !keepValues[lkVal.Name]
		for ord, wantName := range lkVal.Values {
			for j, gotName := range en.Variants {
				var was string
				if j < len(en.Was) {
					was = en.Was[j]
				}
				if gotName == wantName && was != "" {
					c.errf(c.declPos(lkVal.Name), "enum %s: variant %s is recorded in %s and re-bound via `was = %q` — reusing a locked or retired enum value name in the same scope with a new meaning is refused (docs/VERSIONING.md)",
						lkVal.Name, wantName, lockfile.FileName, was)
					break
				}
				if retiredEnum && gotName == wantName && j != ord {
					c.errf(c.declPos(lkVal.Name), "enum %s: variant %s is recorded as retired in %s at ordinal %d and re-added at ordinal %d — reusing a retired enum value name in the same scope with a new meaning is refused (docs/VERSIONING.md)",
						lkVal.Name, wantName, lockfile.FileName, ord, j)
					break
				}
			}
		}
	}
}
