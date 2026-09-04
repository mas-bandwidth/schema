// Maps in the checker (docs/SPEC-TABLES.md §2.8): the `map[K]V` spelling, the
// key rules, the generated entry table and the name it claims.
//
// A map is a LOOKUP over ENTRIES the wire carries as an array of one generated
// `{ key, value }` table, so almost nothing here is new machinery: the value
// resolves through the ordinary field path, the entry is a real table of the
// closure, and what this file adds is the key's own rules and the refusals
// §2.8 states, each by name.
package check

import (
	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// resolveMapField resolves `ships map[string(32)]ShipConfig` (docs/SPEC-TABLES.md
// §2.8). The field's own type is [ir.TMap] and everything about the construct
// hangs off the generated ENTRY: its first field is the key, its second the
// value — a whole field spelling, so a map of arrays, of pointers and of maps
// all come back through this same function.
func (c *checker) resolveMapField(owner string, f *ast.Field, inTable bool) *ir.Field {
	m := f.Map
	if !inTable {
		c.errf(m.Pos, "field %s: a MAP is a table-only construct — a `type`'s wire is positional and fixed-size, and a map is a variable-length lookup with a count; hold the map in a `table` body, or declare a bounded array of a `{ key, value }` type (docs/SPEC-TABLES.md §2.8)",
			f.Name)
		return nil
	}
	if f.Array != nil {
		c.errf(m.Pos, "field %s: a map takes no array bound — a map's count is UNBOUNDED by design, its entries live in the arena and in the node's own extent, and a bound would buy only a clamp that drops entries by key order; declare the map bare, or wrap it in a table and bound an array of that (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	}
	if f.Type.Optional {
		c.errf(m.Pos, "field %s: there is no ?map — a fresh map is empty, an empty map is elided, and its count's zero IS its absence, so a presence bit beside it would be two answers to one question; drop the ? (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	}
	if f.Default != nil {
		c.errf(f.Pos, "field %s: a map takes no specified default — a fresh map is empty, and empty is the only value a default could name (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	}
	for i := range f.Attrs {
		a := &f.Attrs[i]
		switch a.Key {
		case "was", "json":
			// a map is renamed under `was` as any field is, and takes a `json`
			// key as any field does: both are about the field, not the
			// construct (docs/SPEC-TABLES.md §2.8, §5, §16.4)
		default:
			c.errf(a.Pos, "field %s: %s does not apply to a map — a map has no value to bound and no default to name, and a bound on a KEY would clamp an identity, which merges two entries; drop the attribute (docs/SPEC-TABLES.md §2.8, §11)",
				f.Name, a.Key)
			return nil
		}
	}

	entryName := ir.MapEntryName(owner, f.Name)
	if !c.claimMapEntryName(owner, f, entryName) {
		return nil
	}

	key := c.resolveMapKey(f, m)
	if key == nil {
		return nil
	}
	value := c.resolveField(entryName, m.Value, true)
	if value == nil {
		return nil
	}
	value.Name = ir.MapValueFieldName

	entry := &ir.Struct{
		Name:       entryName,
		IsTable:    true,
		MapEntryOf: owner + "." + f.Name,
		Fields:     []*ir.Field{key, value},
		Items:      []ir.Item{&ir.FieldItem{F: key}, &ir.FieldItem{F: value}},
	}
	c.tables[entryName] = entry
	c.declFile[entryName] = c.declFile[owner]
	c.mapEntries = append(c.mapEntries, entry)

	out := &ir.Field{Name: f.Name, Type: ir.FieldType{Kind: ir.TMap}, MapEntry: entry}
	c.resolveAttrs(f, out)
	return out
}

// claimMapEntryName holds the `<Table><Field>Entry` claim (docs/SPEC-TABLES.md
// §2.8, §11). The name is claimed ON THE TABLE THAT DECLARES THE MAP, on the
// terms `<field>_present` is claimed for an optional: a unit that declares
// anything under that spelling beside the map is refused, naming the map. The
// claim lands with the construct rather than ahead of it, and that is safe —
// a stored file never carries the name, because an entry rides under kinds and
// field ids and never under a type id.
func (c *checker) claimMapEntryName(owner string, f *ast.Field, entryName string) bool {
	if d, declared := c.astDecls[entryName]; declared {
		c.errf(f.Map.Pos, "field %s: the map generates a table named %s, and this unit already declares a %s under that name — the entry name is CLAIMED on %s by the map, as %s_present is claimed by an optional; rename the declaration, or rename the map field (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name, entryName, declKindName(d), owner, f.Name)
		return false
	}
	if prior := c.tables[entryName]; prior != nil && prior.MapEntryOf != "" {
		c.errf(f.Map.Pos, "field %s: the map generates a table named %s, and %s already generated one under that name — two maps cannot claim one entry; rename one of the two map fields (docs/SPEC-TABLES.md §2.8)",
			f.Name, entryName, prior.MapEntryOf)
		return false
	}
	return true
}

// resolveMapKey resolves and holds the KEY (docs/SPEC-TABLES.md §2.8). Keys are
// BOUNDED STRINGS and INTEGERS, and nothing else: a key is an identity, so
// everything refused here is refused because it cannot BE one — it has no
// total order a byte compare survives, it names a set rather than one thing,
// or another construct already does its job better.
func (c *checker) resolveMapKey(f *ast.Field, m *ast.MapType) *ir.Field {
	k := m.Key
	switch k.Kind {
	case ast.ScalarString:
		if k.Pointer {
			c.errf(k.Pos, "field %s: a map key is `string(N)` at a declared bound, and *string is a POINTER to a blob node — a key is stored inside the entry at a fixed width, so it has a bound; write map[string(N)] (docs/SPEC-TABLES.md §2.8, §2.5)",
				f.Name)
			return nil
		}
	case ast.ScalarInt:
		if k.Width == 128 {
			c.errf(k.Pos, "field %s: a 128-bit integer is not a map key — the table wire carries it under its own kind and a key is compared as one of the integer kinds a port can order without emulation; key the map with one of int8..int64 or uint8..uint64 (docs/SPEC-TABLES.md §2.8)",
				f.Name)
			return nil
		}
	case ast.ScalarBool:
		c.errf(k.Pos, "field %s: a `bool` key is refused — two slots, which is an array of two; declare [2]T, or a table with the two fields named (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	case ast.ScalarFloat32, ast.ScalarFloat64:
		c.errf(k.Pos, "field %s: a floating-point key is refused — NaN has no equality and -0.0 == 0.0 are two bit patterns, so no total order survives a byte compare; key the map with an integer, or with string(N) (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	case ast.ScalarBits:
		c.errf(k.Pos, "field %s: a bits(N) key is refused — bits(N) is a packing width for the packet wire and rides on the table wire as the narrowest unsigned kind that holds it, so it names no key kind of its own; key the map with that integer kind (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	case ast.ScalarBytes:
		c.errf(k.Pos, "field %s: a bytes(N) key is refused — bytes is opaque storage with no order the language defines; key the map with string(N), whose keys compare by BYTES, unsigned, shortest-prefix first (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	case ast.ScalarFixed:
		c.errf(k.Pos, "field %s: a fixed-point key is refused — a fixed(I, F) is a scaled integer whose scale is a declaration-side fact, so two builds could order one key set two ways; key the map with the integer kind the storage has (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	case ast.ScalarNamed:
		return c.refuseNamedMapKey(f, k)
	default:
		c.errf(k.Pos, "field %s: this is not a map key — keys are `string(N)` and the integer kinds, and nothing else (docs/SPEC-TABLES.md §2.8, §11)", f.Name)
		return nil
	}

	if k.Optional {
		c.errf(k.Pos, "field %s: an OPTIONAL key is refused — a key is an identity and every entry has one, so there is no absence to express; drop the ? (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	}

	// the key resolves through the ordinary field path, so `string(N)`'s bound
	// is validated once, in one place
	keyAst := &ast.Field{Name: ir.MapKeyFieldName, Pos: k.Pos, Type: k}
	key := c.resolveField(f.Name, keyAst, true)
	if key == nil {
		return nil
	}
	key.Name = ir.MapKeyFieldName
	return key
}

// refuseNamedMapKey handles a key that names a declaration: an enum, a flags
// set, a type, a table or a union — each refused with its own reason
// (docs/SPEC-TABLES.md §2.8).
func (c *checker) refuseNamedMapKey(f *ast.Field, k ast.ScalarType) *ir.Field {
	if k.Pointer {
		c.errf(k.Pos, "field %s: a POINTER key is refused — a key is an identity the wire carries and compares, and a node index is an identity of the wire's own making; key the map with the identity the node holds, string(N) or an integer (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	}
	d, declared := c.astDecls[k.Name]
	if !declared {
		c.errf(k.Pos, "field %s: undefined type %s in the map key (docs/SPEC-TABLES.md §2.8)", f.Name, k.Name)
		return nil
	}
	switch d.(type) {
	case *ast.EnumDecl:
		// the one refusal that names a REPLACEMENT, because the replacement is
		// strictly better: `[E]T` is complete by construction, positional, one
		// subtract to reach a slot, no count, no sort, and the FIXED class
		c.errf(k.Pos, "field %s: an ENUM key is refused — that is [%s]T's job and [%s]T does it better: one slot per named variant, complete by construction, positional storage, one subtract to reach a slot, no count, no sort, and the FIXED class, where a map keyed by an enum would pay a variable holder, a sort and log n compares to express an absence ?T already expresses for one bool a slot; declare [%s]%s, and [%s]?%s where a slot may be absent (docs/SPEC-TABLES.md §2.4, §2.8, §11)",
			f.Name, k.Name, k.Name, k.Name, mapValueSpelling(f), k.Name, mapValueSpelling(f))
		return nil
	case *ast.FlagsDecl:
		c.errf(k.Pos, "field %s: a `flags` key is refused — a mask names a SET, not one thing, so it identifies no single entry; key the map with the integer kind the mask rides as, or with string(N) (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	case *ast.TypeDecl:
		c.errf(k.Pos, "field %s: a `type` key is refused — a key is compared as one value at a fixed width, and a composite has no order the language defines; key the map with the identifying member of %s, string(N) or an integer (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name, k.Name)
		return nil
	case *ast.TableDecl:
		c.errf(k.Pos, "field %s: a `table` key is refused — a key is compared as one value at a fixed width, and a table has no order the language defines; key the map with the identifying field of %s, string(N) or an integer (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name, k.Name)
		return nil
	case *ast.UnionDecl:
		c.errf(k.Pos, "field %s: a UNION key is refused — a union is one of several shapes and its arms have no order across them; key the map with string(N) or an integer, and hold the union in the value (docs/SPEC-TABLES.md §2.8, §11)",
			f.Name)
		return nil
	}
	c.errf(k.Pos, "field %s: %s is not a map key — keys are `string(N)` and the integer kinds, and nothing else (docs/SPEC-TABLES.md §2.8, §11)",
		f.Name, k.Name)
	return nil
}

// mapValueSpelling renders a map's VALUE as the author wrote it, for the
// enum-key diagnostic's `[E]T` replacement. A value that is itself a map has
// no `[E]T` spelling to suggest, so the rendering says `T` and the sentence
// still reads.
func mapValueSpelling(f *ast.Field) string {
	v := f.Map.Value
	if v.Map != nil {
		return "T"
	}
	return scalarSpelling(v.Type)
}
