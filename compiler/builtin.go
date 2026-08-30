package compiler

import (
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// builtins is the set [New] registers. The per-language emitters stay
// internal — they are implementations, not API, and freezing eight emitter
// packages under semver would buy nothing — but they reach the driver through
// [Generator] and nothing else, which is what makes that interface a door
// rather than a decoration: if a built-in target can be expressed as a
// registered generator, so can anyone's.
func builtins() []Generator {
	return []Generator{
		cTarget{},
		cppTarget{},
		csTarget{},
		dartTarget{},
		elixirTarget{},
		goTarget{},
		javaTarget{},
		jsTarget{},
		rustTarget{},
	}
}

// dartTarget emits Dart 3.
type dartTarget struct{}

func (dartTarget) Names() []string { return []string{"dart"} }

func (dartTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return dart.Generate(u)
}

// elixirTarget emits Elixir 1.20.
type elixirTarget struct{}

func (elixirTarget) Names() []string { return []string{"elixir"} }

func (elixirTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return elixir.Generate(u)
}

// cTarget emits C99 (SPEC §6.1).
type cTarget struct{}

func (cTarget) Names() []string { return []string{"c"} }

func (cTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return cgen.Generate(u)
}

// cppTarget emits C++17.
type cppTarget struct{}

func (cppTarget) Names() []string { return []string{"cpp"} }

func (cppTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return cpp.Generate(u)
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

// javaTarget emits Java 17.
type javaTarget struct{}

func (javaTarget) Names() []string { return []string{"java"} }

func (javaTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return java.Generate(u)
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
