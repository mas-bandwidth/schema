package compiler

import (
	"fmt"
	"sort"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cstable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// refuseTables is the named refusal every target without a table backend
// gives a unit that declares tables (docs/SPEC-TABLES.md): C++ and C# carry table
// backends today, and each remaining per-language one is a named follow-on —
// refused loudly here rather than silently emitting a unit with the tables
// missing.
func refuseTables(u *ir.Unit, target string) error {
	if len(u.Tables) == 0 {
		return nil
	}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Errorf("unit declares tables (%s) — tables are C++ and C# only today, and the %s table backend is a named follow-on; generate with --lang cpp or --lang cs, or move the tables to their own unit (docs/SPEC-TABLES.md)",
		englishList(names), target)
}

// builtins is the set [New] registers. The per-language emitters stay
// internal — they are implementations, not API, and freezing nine emitter
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
	if err := refuseTables(u, "dart"); err != nil {
		return nil, err
	}
	return dart.Generate(u)
}

// elixirTarget emits Elixir 1.20.
type elixirTarget struct{}

func (elixirTarget) Names() []string { return []string{"elixir"} }

func (elixirTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTables(u, "elixir"); err != nil {
		return nil, err
	}
	return elixir.Generate(u)
}

// cTarget emits C99 (SPEC §6.1).
type cTarget struct{}

func (cTarget) Names() []string { return []string{"c"} }

func (cTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTables(u, "c"); err != nil {
		return nil, err
	}
	return cgen.Generate(u)
}

// cppTarget emits C++17.
type cppTarget struct{}

func (cppTarget) Names() []string { return []string{"cpp"} }

func (cppTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	files, err := cpp.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.h per file — the
	// TABLE-wire codecs (docs/SPEC-TABLES.md); a table-free unit's output is
	// byte-identical to what the packet emitter alone produces
	tables, err := cpptable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table header; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

// csTarget emits C#.
type csTarget struct{}

func (csTarget) Names() []string { return []string{"cs", "csharp"} }

func (csTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	files, err := csharp.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.cs per file — the
	// TABLE-wire codecs, FIXED class (docs/SPEC-TABLES.md); a table-free unit's
	// output is byte-identical to what the packet emitter alone produces
	tables, err := cstable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table source; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

// goTarget emits Go.
type goTarget struct{}

func (goTarget) Names() []string { return []string{"go"} }

func (goTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTables(u, "go"); err != nil {
		return nil, err
	}
	return golang.Generate(u)
}

// javaTarget emits Java 17.
type javaTarget struct{}

func (javaTarget) Names() []string { return []string{"java"} }

func (javaTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTables(u, "java"); err != nil {
		return nil, err
	}
	return java.Generate(u)
}

// jsTarget emits JavaScript ES modules.
type jsTarget struct{}

func (jsTarget) Names() []string { return []string{"js", "javascript"} }

func (jsTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTables(u, "js"); err != nil {
		return nil, err
	}
	return js.Generate(u)
}

// rustTarget emits Rust.
type rustTarget struct{}

func (rustTarget) Names() []string { return []string{"rust"} }

func (rustTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTables(u, "rust"); err != nil {
		return nil, err
	}
	return rust.Generate(u)
}
