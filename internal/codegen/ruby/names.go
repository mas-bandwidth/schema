package ruby

import (
	"fmt"
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
// points, the constructor, and the Object methods the generated code and its
// callers call on a value. A field spelled like one would replace it.
var generatedMembers = wordSet(`read read_bits write load save descriptor initialize inspect hash
	class freeze dup clone send method object_id`)

// unionMembers are the names a generated union class defines beside its arms:
// type, the arm number (docs/PORT-RUBY.md, THE VALUE MAPPING).
var unionMembers = wordSet(`type`)

// runtimeNames are the constants the unit module holds besides its
// declarations: the runtime's (Refused, InvalidValue, LoadReport) and
// serialize.rb's streams, which the generated code names unqualified inside
// the unit module, and the unit's PROTOCOL_ID.
var runtimeNames = wordSet(`Refused InvalidValue LoadReport Stream WriteStream ReadStream MeasureStream
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
	ruleKeyword          = "keyword"
	ruleGeneratedMember  = "generated-member"
)

// CheckNames holds every name the generated namespace spells to
// docs/PORT-RUBY.md, IDENTIFIERS, and refuses the first that breaks a rule, by
// name. The reserved words every target refuses are the checker's
// (internal/check, Ruby's row); the keywords are held here again for the
// names a method takes, because the row leaves out next and alias, which the
// shared corpus spells as fields. The rest are the rules only the Ruby target
// has: the package and its module constant, the file names, constant
// spellings, core constants and runtime names, double-underscore and helper
// prefixes, and the generated members.
func CheckNames(u *ir.Unit) error {
	return checkNames(u, "")
}

// checker is CheckNames with one rule, off, turned off: the negative control's
// handle on each rule. CheckNames turns none off.
type checker struct{ off string }

func (c checker) on(rule string) bool { return rule != c.off }

func checkNames(u *ir.Unit, off string) error {
	c := checker{off: off}
	if err := c.checkPackage(u.Package); err != nil {
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
		for _, v := range d.Variants {
			if err := c.checkVariant(file, d.Name+"'s variant "+v, v); err != nil {
				return err
			}
		}
	case *ir.Flags:
		if err := c.checkConstant(file, "flags "+d.Name, d.Name); err != nil {
			return err
		}
		for _, v := range d.Variants {
			if err := c.checkVariant(file, d.Name+"'s flag "+v, v); err != nil {
				return err
			}
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
	for _, a := range un.Variants {
		what := "union " + un.Name + "'s arm " + a.Name
		if err := c.checkMethod(file, what, a.Name); err != nil {
			return err
		}
		if (generatedMembers[a.Name] || unionMembers[a.Name]) && c.on(ruleGeneratedMember) {
			return fmt.Errorf("ruby: %s.schema: %s is the name of a member the generated union class defines (%s); rename the arm", file, what, memberList(unionMembers))
		}
	}
	return nil
}

// checkConstant holds a module-level name: a type, table, union, enum, flags
// set or const is a constant of the unit module.
func (c checker) checkConstant(file, what, name string) error {
	if err := c.checkVariant(file, what, name); err != nil {
		return err
	}
	if (rubyCoreConstants[name] || runtimeNames[name]) && c.on(ruleCoreConstant) {
		return fmt.Errorf("ruby: %s.schema: %s is a constant the Ruby interpreter or the generated runtime defines, and the generated unit module names those unqualified; rename it", file, what)
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

// checkMethod holds a name the generated code spells as a method: a field or
// a union arm.
func (c checker) checkMethod(file, what, name string) error {
	if rubyKeywords[name] && c.on(ruleKeyword) {
		return fmt.Errorf("ruby: %s.schema: %s is a Ruby keyword; rename it", file, what)
	}
	if strings.HasPrefix(name, "__") && c.on(ruleDoubleUnderscore) {
		return fmt.Errorf("ruby: %s.schema: %s starts with __, which Ruby reserves for the interpreter and reflection; rename it", file, what)
	}
	if strings.HasPrefix(name, HelperPrefix) && c.on(ruleHelperPrefix) {
		return fmt.Errorf("ruby: %s.schema: %s starts with %s, the prefix of the generated code's own names; rename it", file, what, HelperPrefix)
	}
	return nil
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
