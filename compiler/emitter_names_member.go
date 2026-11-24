package compiler

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A member spelled like a named type its own declaration uses cannot be
// spelled in two targets (SPEC §4.6, Emitter name rules). The member is a
// field of a `type` or a `table`, or an arm of a union (a packet union or a
// table-closure one), and the type is its own or a sibling member's:
//
//   - C++ declares the member in the struct (`kind kind = kind::None;`, or
//     `kind mode;` before `Kind2 kind;`), and g++ refuses a member that
//     changes the meaning of a name the class already used as a type
//     (-Wchanges-meaning); the union arm is a member of the union's storage.
//   - Dart spells the member lowerCamel (ir.GoExportName, first letter
//     lowered), so a field `Kind` of type `kind` is `kind kind = kind.none;`,
//     and a member hides the type of that name everywhere in the class body:
//     the analyzer reports an undefined class.
//
// C++ compares the member name as written, Dart its lowerCamel spelling, so
// `Kind kind` is refused for Dart and `kind Kind` (two names in both) is not.
// A unit schema check accepts builds in every target, so the check refuses it
// for every target, naming the target.
func init() {
	check.RegisterEmitterNameRule("C++", memberNamedLikeAType(
		func(name string) string { return name },
		"the member declaration changes the meaning of the type name (-Wchanges-meaning)"))
	check.RegisterEmitterNameRule("Dart", memberNamedLikeAType(dart.MemberName,
		"the member, spelled %q in Dart, hides that type in the class body"))
}

// memberOwner is one declaration whose members share a class body: a type or
// table and its fields, or a union and its arms.
type memberOwner struct {
	kind, name string
	members    []ownedMember
}

type ownedMember struct {
	name string
	typ  string // the named type the member is declared with; "" for none
}

func memberOwners(u *ir.Unit) []memberOwner {
	var out []memberOwner
	fields := func(kind string, s *ir.Struct) memberOwner {
		o := memberOwner{kind: kind, name: s.Name}
		for _, f := range s.Fields {
			m := ownedMember{name: f.Name}
			if f.Type.Kind == ir.TNamed {
				m.typ = f.Type.Name
			}
			o.members = append(o.members, m)
		}
		return o
	}
	for _, s := range u.Structs {
		out = append(out, fields("type", s))
	}
	for _, s := range u.Tables {
		out = append(out, fields("table", s))
	}
	for _, unions := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for _, un := range unions {
			o := memberOwner{kind: "union", name: un.Name}
			for _, v := range un.Variants {
				if v.F == nil {
					continue // a tag-only arm declares no member
				}
				m := ownedMember{name: v.Name}
				if v.F.Type.Kind == ir.TNamed {
					m.typ = v.F.Type.Name
				}
				o.members = append(o.members, m)
			}
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return out[i].kind < out[j].kind
	})
	return out
}

// memberNamedLikeAType refuses a member whose target spelling (spell) is the
// name of a named type some member of the same declaration is declared with.
// why may hold one %q, the member's target spelling.
func memberNamedLikeAType(spell func(string) string, why string) func(*ir.Unit) error {
	return func(u *ir.Unit) error {
		for _, o := range memberOwners(u) {
			used := map[string]bool{}
			for _, m := range o.members {
				if m.typ != "" {
					used[m.typ] = true
				}
			}
			for _, m := range o.members {
				s := spell(m.name)
				if !used[s] {
					continue
				}
				reason := why
				if strings.Contains(why, "%q") {
					reason = fmt.Sprintf(why, s)
				}
				if m.typ == s {
					return fmt.Errorf("%s %s: member %s is named like its own type %s: %s; rename the member or the type", o.kind, o.name, m.name, s, reason)
				}
				return fmt.Errorf("%s %s: member %s is named like the type %s another member of %s is declared with: %s; rename the member or the type", o.kind, o.name, m.name, s, o.name, reason)
			}
		}
		return nil
	}
}
