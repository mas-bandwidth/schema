package compiler

import (
	cgen "github.com/mas-bandwidth/schema/internal/codegen/c"
	"github.com/mas-bandwidth/schema/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/internal/codegen/js"
	"github.com/mas-bandwidth/schema/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/ir"
)

// builtins is the set [New] registers. The per-language emitters stay
// internal — they are implementations, not API, and freezing six emitter
// packages under semver would buy nothing — but they reach the driver through
// [Generator] and nothing else, which is what makes that interface a door
// rather than a decoration: if a built-in target can be expressed as a
// registered generator, so can anyone's.
func builtins() []Generator {
	return []Generator{
		cTarget{},
		cppTarget{},
		csTarget{},
		goTarget{},
		jsTarget{},
		rustTarget{},
	}
}

// cTarget emits C99 (SPEC §6.1).
type cTarget struct{}

func (cTarget) Names() []string { return []string{"c"} }

func (cTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return cgen.Generate(u)
}

// cppTarget emits C++17, in either message representation.
type cppTarget struct{}

func (cppTarget) Names() []string { return []string{"cpp"} }

// Generate reads one option: "cpp-message" selects the C++ message dispatch
// representation, "union" (the default) or "variant" (SPEC §4.8).
func (cppTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return cpp.Generate(u, cpp.Options{MessageRepr: opts["cpp-message"]})
}

// csTarget emits C#.
type csTarget struct{}

func (csTarget) Names() []string { return []string{"cs", "csharp"} }

func (csTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return csharp.Generate(u)
}

// goTarget emits Go.
type goTarget struct{}

func (goTarget) Names() []string { return []string{"go"} }

func (goTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return golang.Generate(u)
}

// jsTarget emits JavaScript ES modules.
type jsTarget struct{}

func (jsTarget) Names() []string { return []string{"js", "javascript"} }

func (jsTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return js.Generate(u)
}

// rustTarget emits Rust.
type rustTarget struct{}

func (rustTarget) Names() []string { return []string{"rust"} }

func (rustTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return rust.Generate(u)
}
