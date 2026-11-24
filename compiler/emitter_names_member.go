package compiler

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A field named exactly like its own named type (an enum `kind` beside a field
// `kind kind`) cannot be spelled in two targets (SPEC §4.6, Emitter name
// rules): C++ declares `kind kind = kind::None;` in the struct, which g++
// refuses as a declaration that changes the meaning of `kind`, and Dart
// declares `kind kind = kind.none;` in the class, where the member hides the
// type and the analyzer reports an undefined class. A unit schema check
// accepts builds in every target, so it is refused for every target, naming
// both. A field and a type that differ by case (`kind Kind`) are two names
// and build.
func init() {
	check.RegisterEmitterNameRule("C++", memberNamedLikeItsType("the member declaration changes the meaning of the type name (-Wchanges-meaning)"))
	check.RegisterEmitterNameRule("Dart", memberNamedLikeItsType("the field hides its own type in the class body"))
}

func memberNamedLikeItsType(why string) func(*ir.Unit) error {
	return func(u *ir.Unit) error {
		var owners []string
		byName := map[string]*ir.Struct{}
		for name, s := range u.Structs {
			owners = append(owners, name)
			byName[name] = s
		}
		for name, s := range u.Tables {
			owners = append(owners, name)
			byName[name] = s
		}
		sort.Strings(owners)
		for _, owner := range owners {
			for _, f := range byName[owner].Fields {
				if f.Type.Kind == ir.TNamed && f.Type.Name == f.Name {
					return fmt.Errorf("%s.%s is a field named like its own type %s: %s; rename the field or the type (SPEC §4.6)", owner, f.Name, f.Type.Name, why)
				}
			}
		}
		return nil
	}
}
