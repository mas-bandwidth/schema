// THE MESSAGE CLOSURE (docs/SPEC-TABLES.md §3.3): the announcement carries
// the message closure alone, so a name no fixed-table body can carry takes
// no slot. This file is the trimmed vocabulary the announcement and the
// message encoder share; [ir.TableVocabulary] remains the whole-closure
// vocabulary the other forms and the compiler's facts settle from.
package tablewire

import (
	"encoding/binary"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageClosure is the set of structs the MESSAGE FORM can reach: every
// `fixed table` declaration plus every struct one reaches BY VALUE (nested
// fixed tables and types, array elements, union payloads), transitively.
// The message form is fixed tables only (§3.3), and a fixed table's closure
// holds no pointer, map or variable table (§2.2), so a variable table and
// anything only it reaches is outside the set.
func MessageClosure(u *ir.Unit) map[string]bool {
	closure := map[string]bool{}
	seenUnion := map[*ir.Union]bool{}
	var walk func(name string)
	var walkUnion func(un *ir.Union)
	walkUnion = func(un *ir.Union) {
		if seenUnion[un] {
			return
		}
		seenUnion[un] = true
		for _, v := range un.Variants {
			if v.F == nil || v.F.Type.Kind != ir.TNamed || v.F.Type.Pointer {
				continue
			}
			switch ref := v.F.Type.Ref.(type) {
			case *ir.Struct:
				walk(ref.Name)
			case *ir.Union:
				walkUnion(ref)
			}
		}
	}
	walk = func(name string) {
		if closure[name] {
			return
		}
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil || (st.IsTable && !st.FixedDeclared) {
			return
		}
		closure[name] = true
		for _, f := range st.Fields {
			if f.IsMap() || f.Type.Kind != ir.TNamed || f.Type.Pointer {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				walk(ref.Name)
			case *ir.Union:
				walkUnion(ref)
			}
		}
	}
	names := make([]string, 0, len(u.Tables))
	for name, st := range u.Tables {
		if st.FixedDeclared {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		walk(name)
	}
	return closure
}

// MessageVocabulary is the vocabulary the announcement carries: the entries
// of [ir.TableVocabulary] the MESSAGE CLOSURE can put on the wire, in the
// same cook-projection order, and nothing else. A variable table's fields,
// every table's name id, the reserved node-table id, the three blob type
// ids, and an enum variant or union arm that only a variable table reaches
// take no slot, because no body can carry them. A field or an arm whose
// wire id equals a reached variant name takes none either: the id match
// keeps a kind-0 name entry and nothing else.
func MessageVocabulary(u *ir.Unit) []ir.TableVocabularyEntry {
	closure := MessageClosure(u)
	keep := map[string]bool{}
	names := map[uint64]bool{}
	var reach func(fields []*ir.Field)
	var reachUnion func(un *ir.Union)
	seenUnion := map[*ir.Union]bool{}
	reachUnion = func(un *ir.Union) {
		if seenUnion[un] {
			return
		}
		seenUnion[un] = true
		for _, v := range un.Variants {
			keep[ir.TableArmEntry(v).Key()] = true
			if v.F != nil {
				reach([]*ir.Field{v.F})
			}
		}
	}
	reach = func(fields []*ir.Field) {
		for _, f := range fields {
			keep[ir.TableFieldEntry(f).Key()] = true
			if f.IsMap() && f.MapEntry != nil {
				for _, sub := range f.MapEntry.Fields {
					keep[ir.TableFieldEntry(sub).Key()] = true
				}
			}
			if f.KeyEnumRef != nil {
				for i := range f.KeyEnumRef.Variants {
					names[ir.TableWireId(f.KeyEnumRef.VariantWireName(i))] = true
				}
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Enum:
				for i := range ref.Variants {
					names[ir.TableWireId(ref.VariantWireName(i))] = true
				}
			case *ir.Union:
				reachUnion(ref)
			}
		}
	}
	for member := range closure {
		st := u.Tables[member]
		if st == nil {
			st = u.Structs[member]
		}
		if st == nil {
			continue
		}
		reach(st.Fields)
	}
	var out []ir.TableVocabularyEntry
	for _, e := range ir.TableVocabulary(u) {
		// Kind 0 is a variant name (ir.TableVocabulary places it with no
		// kind). A field or an arm can share that wire id; keep holds those.
		if keep[e.Key()] || (names[e.Id] && e.Kind == 0) {
			out = append(out, e)
		}
	}
	return out
}

// messageAnnouncement is the unit's ID TABLE MESSAGE over the MESSAGE
// CLOSURE's vocabulary, byte for byte as [ir.TableAnnouncement] spells the
// whole-closure one: a form-`1` file whose body carries the build version
// and the vocabulary under their reserved ids, and nothing else in its
// trailer. Every byte is settled by the compiler.
func messageAnnouncement(u *ir.Unit) []byte {
	vocabulary := ir.TableVocabularyBytes(MessageVocabulary(u))
	out := make([]byte, 0, 32+len(vocabulary)+24)
	out = append(out, ir.TableWireForm)
	out = append(out, 1)                      // reference 1: the reserved build-version id, first-use
	out = append(out, uint8(ir.TableKindU64)) // kind 9
	out = binary.LittleEndian.AppendUint64(out, ir.BuildVersion(u))
	out = append(out, 2, uint8(ir.TableKindArray)) // reference 2: the vocabulary
	body := make([]byte, 0, len(vocabulary)+16)
	body = append(body, uint8(ir.TableKindU8)) // the element kind: bytes
	body = appendLeb(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = appendLeb(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0) // the zero reference that ends the body
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}
