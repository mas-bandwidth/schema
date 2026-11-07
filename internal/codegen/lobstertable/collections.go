package lobstertable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// init joins the collections group to the descriptor emitter (docs/PORT-
// LOBSTER.md, THE CARD MAP): maps (docs/SPEC-TABLES.md §2.8), byte buffers
// `*bytes` and `*string` (§2.5) and unbounded arrays `[]T` (§2.9).
func init() { Register("collections", emitCollections) }

// emitCollections is the descriptor emitter's part for the collections
// constructs. Each construct rides the descriptor facts lobstertable.go
// already writes — a map's generated entry table (t: <Table><Field>Entry,
// whose first field is the key), a blob's sblob/bblob shape (§2.5), and an
// unbounded array's array: "list" with its element descriptor (§2.9) — so
// the load and save walkers reach every one of them through the one
// descriptor walk (docs/PORT-LOBSTER.md, THE LOAD). This part holds a
// construct to the facts its walk reads and refuses, by name, a map whose
// entry table is missing: without it the walk has no key kind to sort and
// clamp by, and the map could not be carried.
func emitCollections(g *lobster.Gen) error {
	seen := map[string]bool{}
	var visitStruct func(st *ir.Struct) error
	var visitField func(f *ir.Field) error
	visitField = func(f *ir.Field) error {
		if f.IsMap() {
			if f.MapEntry == nil {
				return fmt.Errorf("table %s field %s: a map with no entry table has no key kind for the walk (docs/SPEC-TABLES.md §2.8)", g.File.Base, f.Name)
			}
			return visitStruct(f.MapEntry)
		}
		if f.Type.Kind != ir.TNamed {
			return nil
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return visitStruct(r)
		case *ir.Union:
			if seen["u:"+r.Name] {
				return nil
			}
			seen["u:"+r.Name] = true
			for _, v := range r.Variants {
				if v.F != nil {
					if err := visitField(v.F); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	visitStruct = func(st *ir.Struct) error {
		if seen["s:"+st.Name] {
			return nil
		}
		seen["s:"+st.Name] = true
		for _, f := range st.Fields {
			if err := visitField(f); err != nil {
				return err
			}
		}
		return nil
	}
	for _, t := range g.File.Tables {
		if err := visitStruct(t); err != nil {
			return err
		}
	}
	for _, un := range g.File.TableUnions {
		if seen["u:"+un.Name] {
			continue
		}
		seen["u:"+un.Name] = true
		for _, v := range un.Variants {
			if v.F != nil {
				if err := visitField(v.F); err != nil {
					return err
				}
			}
		}
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if err := visitStruct(d); err != nil {
				return err
			}
		case *ir.Union:
			if seen["u:"+d.Name] {
				continue
			}
			seen["u:"+d.Name] = true
			for _, v := range d.Variants {
				if v.F != nil {
					if err := visitField(v.F); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
