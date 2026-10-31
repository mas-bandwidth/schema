package cpp

import "github.com/mas-bandwidth/schema/v2/ir"

// fileNestsTypes reports whether any struct in this file carries a field of
// a struct or union type — the fields whose wire is a call to another
// generated Write/Read function, spelled through the object macros below.
func (g *gen) fileNestsTypes() bool {
	for _, d := range g.file.Decls {
		st, ok := d.(*ir.Struct)
		if !ok {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch f.Type.Ref.(type) {
			case *ir.Struct, *ir.Union:
				return true
			}
		}
	}
	return false
}

// emitObjectMacros emits the one-way macro pair a nested type's wire is
// spelled with. A generated body has ONE spelling for every stream
// operation: the serialize read_*/write_* family (read_bits, read_int,
// write_double, ...), whose refusal returns false from the enclosing
// function — Glenn's serialize idiom, the companions serialize.h provides
// for exactly this split read/write shape. serialize's own read_object and
// write_object call a Serialize MEMBER; generated types carry free
// Write<T>/Read<T> functions instead, so these take the function as an
// argument and keep the same do/while shape. Unlike write_object, the write
// twin propagates a false: a nested write's result is the caller's result.
// What stays an explicit if is a check the generated code itself makes on a
// decoded value (const, reserved, an offset bound, text content): those are
// refusals, not stream operations, and each carries its SPEC citation.
// Guarded against redefinition because several wire headers can land in one
// translation unit.
func (g *gen) emitObjectMacros() {
	g.pf("#ifndef SCHEMA_OBJECT_MACROS_DEFINED\n#define SCHEMA_OBJECT_MACROS_DEFINED\n")
	g.pf("// schema_write_object / schema_read_object — a nested type's Write/Read in\n")
	g.pf("// the serialize one-way macro idiom (read_bits, read_int, read_object): a\n")
	g.pf("// refusal returns false from the enclosing function, so every stream\n")
	g.pf("// operation in a generated body has the one spelling. serialize's own\n")
	g.pf("// read_object/write_object call a Serialize member; generated types carry\n")
	g.pf("// free Write/Read functions, so the function rides as an argument.\n")
	g.pf("#define schema_write_object( stream, function, value ) \\\n")
	g.pf("    do \\\n")
	g.pf("    { \\\n")
	g.pf("        if ( !function( stream, value ) ) \\\n")
	g.pf("        { \\\n")
	g.pf("            return false; \\\n")
	g.pf("        } \\\n")
	g.pf("    } while ( 0 )\n\n")
	g.pf("#define schema_read_object( stream, function, value ) \\\n")
	g.pf("    do \\\n")
	g.pf("    { \\\n")
	g.pf("        if ( !function( stream, value ) ) \\\n")
	g.pf("        { \\\n")
	g.pf("            return false; \\\n")
	g.pf("        } \\\n")
	g.pf("    } while ( 0 )\n")
	g.pf("#endif // SCHEMA_OBJECT_MACROS_DEFINED\n\n")
}
