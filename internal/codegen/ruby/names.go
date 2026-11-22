package ruby

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// HelperPrefix starts every name the generated code invents for itself. A
// schema name that starts with it is refused, so no schema name collides with
// a helper.
const HelperPrefix = "_s_"

// generatedMembers are the names the generated classes define or rely on
// beside their fields (docs/PORT-RUBY.md, IDENTIFIERS): the codec's entry
// points, the constructor, to_h, and the Object methods the generated code,
// its runtimes and its callers call on a value (public_send is how the
// descriptor codec and the table runtime reach a field by name). A field
// spelled like one would replace it, and the attr_accessor or the def that
// comes second wins, so the field or the member would lose its meaning.
var generatedMembers = wordSet(`read read_bits write load save descriptor initialize inspect hash
	class freeze dup clone send public_send method to_h`)

// tableMembers are the instance methods a generated table class defines beside
// generatedMembers (internal/codegen/rubytable, emitTable): its measures, its
// retaining save, its JSON and text writers, and its cook. A table field
// spelled like one would replace it.
var tableMembers = wordSet(`measure measure_retain save_retain save_text to_json cook cook_measure`)

// unionMembers are the names a generated union class defines beside its arms:
// type, the arm number, the packet union codec entry points, and the Object
// methods generated code and its runtimes rely on (docs/PORT-RUBY.md,
// IDENTIFIERS).
var unionMembers = wordSet(`type read read_bits write initialize inspect hash
	class freeze dup clone send public_send method`)

// unionTagConstants are the constants a union's tag module (<Union>Type)
// defines beside its arms' constants. None and Max are refused by the checker
// for every target; NAMES is the Ruby tag module's own, so an arm whose
// constant spells it would redefine the name table.
var unionTagConstants = map[string]string{"None": "built-in", "Max": "built-in", "NAMES": "built-in"}

// VariantConstant returns the Ruby constant name for an enum or flags variant:
// verbatim when it already starts with an uppercase ASCII letter, otherwise
// ir.GoExportName as union tag modules spell theirs (docs/PORT-RUBY.md).
func VariantConstant(v string) string {
	if v != "" && v[0] >= 'a' && v[0] <= 'z' {
		return ir.GoExportName(v)
	}
	return v
}

// runtimeNames are the constants the unit module holds besides its
// declarations: the runtime's (Refused, InvalidValue, LoadReport) and
// serialize.rb's streams, which the generated code names unqualified inside
// the unit module, and the unit's PROTOCOL_ID.
var runtimeNames = wordSet(`Refused InvalidValue LoadReport Serialize Stream WriteStream ReadStream MeasureStream
	PROTOCOL_ID`)

// The rules of docs/PORT-RUBY.md, IDENTIFIERS, by name. A refusal names its
// rule's subject; the negative control of TestRubySkeleton turns each rule
// off in turn (checkNames) and requires the case it guards to pass.
const (
	rulePackageSpelling  = "package-spelling"
	rulePackageConstant  = "package-constant"
	ruleFileIdentifier   = "file-identifier"
	ruleFileReserved     = "file-reserved"
	ruleFileCaseFold     = "file-case-fold"
	ruleConstantSpelling = "constant-spelling"
	ruleCoreConstant     = "core-constant"
	ruleDoubleUnderscore = "double-underscore"
	ruleHelperPrefix     = "helper-prefix"
	ruleGeneratedMember  = "generated-member"
	ruleConstantMapping  = "constant-mapping"
)

// CheckNames holds every name the generated namespace spells to
// docs/PORT-RUBY.md, IDENTIFIERS, and refuses the first that breaks a rule, by
// name. The reserved words every target refuses are the checker's
// (internal/check, Ruby's row). No name is refused here for being a Ruby
// keyword: a field or arm is reached only through the field access rule
// (fields.go), which carries every keyword, so next and alias, which the row
// leaves out, are carried as written. The rest are the rules only the Ruby
// target has: the package and its module constant, the file names, constant
// spellings, core constants and runtime names, double-underscore and helper
// prefixes, and the generated members. A declaration is held as the Ruby
// constant it is spelled as (ClassName, ConstName), so a lowercase name is
// carried, and refused only when its constant is one the interpreter or
// runtime defines or one another declaration spells too. schema check runs
// it for every target (SPEC §4.6), through rubytable.CheckNamesAtCheck.
func CheckNames(u *ir.Unit) error {
	return checkNames(u, "")
}

// checker is CheckNames with one rule, off, turned off: the negative control's
// handle on each rule. CheckNames turns none off.
type checker struct {
	off string
}

func (c checker) on(rule string) bool { return rule != c.off }

func checkNames(u *ir.Unit, off string) error {
	return checker{off: off}.check(u)
}

func (c checker) check(u *ir.Unit) error {
	if err := c.checkPackage(u.Package); err != nil {
		return err
	}
	if err := c.checkModuleScope(u); err != nil {
		return err
	}
	folded := map[string]string{}
	bases := make([]string, 0, len(u.Files))
	for _, f := range u.Files {
		bases = append(bases, f.Base)
	}
	sort.Strings(bases)
	for _, base := range bases {
		if err := c.checkFile(base); err != nil {
			return err
		}
		key := strings.ToLower(base)
		if other, ok := folded[key]; ok && c.on(ruleFileCaseFold) {
			return fmt.Errorf("ruby: the schema files %s and %s become %s.rb and %s.rb, which differ only in case; macOS and Windows fold case, so rename one", other, base, other, base)
		}
		folded[key] = base
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if err := c.checkDecl(f.Base, d); err != nil {
				return err
			}
		}
		for _, t := range f.Tables {
			if err := c.checkConstant(f.Base, "table "+t.Name, t.Name); err != nil {
				return err
			}
			if err := c.checkFields(f.Base, t.Name, t.Fields); err != nil {
				return err
			}
			if err := c.checkTableFields(f.Base, t.Name, t.Fields); err != nil {
				return err
			}
		}
		for _, un := range f.TableUnions {
			if err := c.checkUnion(f.Base, un); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkPackage holds the package: it names the directory and <package>.rb
// verbatim, and its module constant is its segments capitalized. Each segment
// must start with a letter, so the constant is a constant and no two packages
// map to one (game_2 and game2 would both be Game2), and the constant must not
// be one the interpreter already defines, which the unit would reopen.
func (c checker) checkPackage(pkg string) error {
	if c.on(rulePackageSpelling) {
		for seg := range strings.SplitSeq(pkg, "_") {
			if seg == "" || !isLetter(seg[0]) || !isIdentifier(seg) {
				return fmt.Errorf("ruby: the package %q does not map to a Ruby module constant: each _-separated segment must start with a letter (the module is its segments capitalized); rename the package", pkg)
			}
		}
	}
	if mod := ModuleName(pkg); c.on(rulePackageConstant) && rubyCoreConstants[mod] {
		return fmt.Errorf("ruby: the package %s becomes the module %s, a constant the Ruby interpreter already defines; the unit would reopen it rather than create a namespace, so rename the package", pkg, mod)
	}
	return nil
}

// checkFile holds a schema file's base name: <package>/<Base>.rb.
func (c checker) checkFile(base string) error {
	if !isIdentifier(base) && c.on(ruleFileIdentifier) {
		return fmt.Errorf("ruby: the schema file %s.schema becomes %s.rb, and %s is not a Ruby identifier; rename the file", base, base, base)
	}
	if low := strings.ToLower(base); (low == RuntimeBase || low == SerializeBase) && c.on(ruleFileReserved) {
		return fmt.Errorf("ruby: the schema file %s.schema becomes %s.rb, which folds to the runtime's %s.rb beside it; rename the file", base, base, low)
	}
	return nil
}

func (c checker) checkDecl(file string, d ir.Decl) error {
	switch d := d.(type) {
	case *ir.Const:
		return c.checkConstant(file, "const "+d.Name, d.Name)
	case *ir.Enum:
		if err := c.checkConstant(file, "enum "+d.Name, d.Name); err != nil {
			return err
		}
		seen := map[string]string{"None": "built-in", "Max": "built-in", "NAMES": "built-in"}
		for _, v := range d.Variants {
			vc := VariantConstant(v)
			if err := c.checkVariant(file, d.Name+"'s variant "+v, vc); err != nil {
				return err
			}
			if prev, dup := seen[vc]; dup && c.on(ruleConstantSpelling) {
				return fmt.Errorf("ruby: %s.schema: enum %s's variant %s spells constant %s, colliding with %s; rename it", file, d.Name, v, vc, prev)
			}
			seen[vc] = v
		}
	case *ir.Flags:
		if err := c.checkConstant(file, "flags "+d.Name, d.Name); err != nil {
			return err
		}
		seen := map[string]string{"Count": "built-in", "NAMES": "built-in"}
		for _, v := range d.Variants {
			vc := VariantConstant(v)
			if err := c.checkVariant(file, d.Name+"'s flag "+v, vc); err != nil {
				return err
			}
			if prev, dup := seen[vc]; dup && c.on(ruleConstantSpelling) {
				return fmt.Errorf("ruby: %s.schema: flags %s's flag %s spells constant %s, colliding with %s; rename it", file, d.Name, v, vc, prev)
			}
			seen[vc] = v
		}
	case *ir.Struct:
		if err := c.checkConstant(file, "type "+d.Name, d.Name); err != nil {
			return err
		}
		return c.checkFields(file, d.Name, d.Fields)
	case *ir.Union:
		return c.checkUnion(file, d)
	}
	return nil
}

func (c checker) checkUnion(file string, un *ir.Union) error {
	if err := c.checkConstant(file, "union "+un.Name, un.Name); err != nil {
		return err
	}
	seen := maps.Clone(unionTagConstants)
	for _, a := range un.Variants {
		what := "union " + un.Name + "'s arm " + a.Name
		ac := ir.GoExportName(a.Name)
		if prev, dup := seen[ac]; dup && c.on(ruleConstantSpelling) {
			return fmt.Errorf("ruby: %s.schema: %s spells the constant %sType::%s, colliding with %s; rename the arm", file, what, ClassName(un.Name), ac, prev)
		}
		seen[ac] = a.Name
		if err := c.checkMethod(file, what, a.Name); err != nil {
			return err
		}
		if unionMembers[a.Name] && c.on(ruleGeneratedMember) {
			return fmt.Errorf("ruby: %s.schema: %s is the name of a member the generated union class defines (%s); rename the arm", file, what, unionMemberList())
		}
	}
	return nil
}

// checkConstant holds a module-level name: a type, table, union, enum, flags
// set or const is a constant of the unit module, spelled as declConstant maps
// it. what starts with the declaration's kind ("const ", "type ", ...).
func (c checker) checkConstant(file, what, name string) error {
	rb := declConstant(strings.HasPrefix(what, "const "), name)
	spelled := what
	if rb != name {
		spelled = what + ", spelled " + rb + " in Ruby,"
	}
	if err := c.checkVariant(file, spelled, rb); err != nil {
		return err
	}
	if (rubyCoreConstants[rb] || runtimeNames[rb]) && c.on(ruleCoreConstant) {
		return fmt.Errorf("ruby: %s.schema: %s is a constant the Ruby interpreter or the generated runtime defines, and the generated unit module names those unqualified; rename it", file, spelled)
	}
	return nil
}

// declConstant is the unit-module constant a declaration is spelled as: a
// const by ConstName, everything else by ClassName.
func declConstant(isConst bool, name string) string {
	if isConst {
		return ConstName(name)
	}
	return ClassName(name)
}

// checkModuleScope refuses two declarations whose Ruby constants are one
// constant of the unit module (every file of a unit opens the same module):
// foo_bar and FooBar both spell FooBar, and a union's tag module is its class
// name and Type, so union Event and type EventType collide too. The mapping
// keeps a name that is already a constant, so only a mapped name can collide.
func (c checker) checkModuleScope(u *ir.Unit) error {
	if !c.on(ruleConstantMapping) {
		return nil
	}
	type owner struct{ file, what, name string }
	seen := map[string]owner{}
	claim := func(file, what, name, rb string) error {
		if prev, ok := seen[rb]; ok && prev.name != name {
			return fmt.Errorf("ruby: %s.schema: %s spells the Ruby constant %s, which %s.schema's %s spells too; rename one", file, what, rb, prev.file, prev.what)
		}
		seen[rb] = owner{file, what, name}
		return nil
	}
	union := func(file, name string) error {
		what := "union " + name
		if err := claim(file, what, name, ClassName(name)); err != nil {
			return err
		}
		return claim(file, what+"'s tag module", name+"Type", ClassName(name)+"Type")
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			var err error
			switch d := d.(type) {
			case *ir.Const:
				err = claim(f.Base, "const "+d.Name, d.Name, ConstName(d.Name))
			case *ir.Enum:
				err = claim(f.Base, "enum "+d.Name, d.Name, ClassName(d.Name))
			case *ir.Flags:
				err = claim(f.Base, "flags "+d.Name, d.Name, ClassName(d.Name))
			case *ir.Struct:
				err = claim(f.Base, "type "+d.Name, d.Name, ClassName(d.Name))
			case *ir.Union:
				err = union(f.Base, d.Name)
			}
			if err != nil {
				return err
			}
		}
		for _, t := range f.Tables {
			if err := claim(f.Base, "table "+t.Name, t.Name, ClassName(t.Name)); err != nil {
				return err
			}
		}
		for _, un := range f.TableUnions {
			if err := union(f.Base, un.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkVariant holds a constant's spelling: an uppercase ASCII letter first.
// An enum or flags variant is a constant of its enum's module.
func (c checker) checkVariant(file, what, name string) error {
	if (name == "" || name[0] < 'A' || name[0] > 'Z') && c.on(ruleConstantSpelling) {
		return fmt.Errorf("ruby: %s.schema: %s is not a Ruby constant, which must start with an uppercase letter; rename it", file, what)
	}
	return nil
}

func (c checker) checkFields(file, owner string, fields []*ir.Field) error {
	for _, fd := range fields {
		what := owner + "'s field " + fd.Name
		if err := c.checkMethod(file, what, fd.Name); err != nil {
			return err
		}
		if generatedMembers[fd.Name] && c.on(ruleGeneratedMember) {
			return fmt.Errorf("ruby: %s.schema: %s is the name of a member the generated class defines (%s); rename the field", file, what, memberList(nil))
		}
	}
	return nil
}

// checkTableFields holds a table's fields to the members only a table class
// defines.
func (c checker) checkTableFields(file, owner string, fields []*ir.Field) error {
	for _, fd := range fields {
		if tableMembers[fd.Name] && c.on(ruleGeneratedMember) {
			return fmt.Errorf("ruby: %s.schema: %s's field %s is the name of a member the generated table class defines (%s); rename the field", file, owner, fd.Name, memberList(tableMembers))
		}
	}
	return nil
}

// checkMethod holds a name the generated code spells as a method: a field or
// a union arm.
func (c checker) checkMethod(file, what, name string) error {
	if strings.HasPrefix(name, "__") && c.on(ruleDoubleUnderscore) {
		return fmt.Errorf("ruby: %s.schema: %s starts with __, which Ruby reserves for the interpreter and reflection; rename it", file, what)
	}
	if strings.HasPrefix(name, HelperPrefix) && c.on(ruleHelperPrefix) {
		return fmt.Errorf("ruby: %s.schema: %s starts with %s, the prefix of the generated code's own names; rename it", file, what, HelperPrefix)
	}
	return nil
}

func unionMemberList() string {
	names := make([]string, 0, len(unionMembers))
	for n := range unionMembers {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func memberList(extra map[string]bool) string {
	names := make([]string, 0, len(generatedMembers)+len(extra))
	for n := range generatedMembers {
		names = append(names, n)
	}
	for n := range extra {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isIdentifier reports whether s is an ASCII Ruby identifier. Schema names are
// ASCII ([A-Za-z_][A-Za-z0-9_]*), so the ASCII rule is the whole rule; a file
// name is checked the same way.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_', isLetter(c):
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func wordSet(words string) map[string]bool {
	out := map[string]bool{}
	for w := range strings.FieldsSeq(words) {
		out[w] = true
	}
	return out
}

// ClassName is the Ruby constant a type, table, enum, flags or union is
// spelled as (Glenn's ruling, 2026-11-22: what is native for Ruby): its
// schema name when that already starts with an uppercase ASCII letter, so a
// unit that was legal before generates byte for byte as it did, and
// otherwise its UpperCamelCase (kind -> Kind, max_items -> MaxItems). The
// wire, the descriptors, every id and every diagnostic keep the schema name.
func ClassName(name string) string {
	if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
		return name
	}
	return ir.GoExportName(name)
}

// ConstName is the Ruby constant a const is spelled as: its schema name when
// that already starts with an uppercase ASCII letter, and otherwise its
// SCREAMING_SNAKE_CASE (max_items -> MAX_ITEMS).
func ConstName(name string) string {
	if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
		return name
	}
	return ir.RustConstName(name)
}
