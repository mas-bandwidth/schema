package ruby

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

func emptyUnit(pkg string, bases ...string) *ir.Unit {
	u := &ir.Unit{Package: pkg, ProtocolId: 0x0123456789abcdef}
	for _, b := range bases {
		u.Files = append(u.Files, &ir.File{Base: b})
	}
	return u
}

func withDecl(d ir.Decl) *ir.Unit {
	u := emptyUnit("game", "Game")
	u.Files[0].Decls = []ir.Decl{d}
	return u
}

func withTable(st *ir.Struct) *ir.Unit {
	u := emptyUnit("game", "Game")
	u.Files[0].Tables = []*ir.Struct{st}
	return u
}

func fields(names ...string) []*ir.Field {
	var out []*ir.Field
	for _, n := range names {
		out = append(out, &ir.Field{Name: n})
	}
	return out
}

// writeTree writes the generated files under dir.
func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for rel, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func generate(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// requireRelativeOnly is the gate on THE RUNTIME LAYOUT's first rule: a
// generated file reaches its siblings and the runtime with require_relative
// alone, never require, load or autoload, which search the load path.
func requireRelativeOnly(files map[string][]byte) error {
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		for n, line := range strings.Split(string(files[rel]), "\n") {
			word, _, _ := strings.Cut(strings.TrimSpace(line), " ")
			switch word {
			case "require", "load", "autoload":
				return fmt.Errorf("%s:%d reaches a file with %s, not require_relative: %s", rel, n+1, word, strings.TrimSpace(line))
			}
		}
	}
	return nil
}

// driverAnswersAbsent is the gate on the skeleton's conformance driver: it
// lists no surface, and every surface it is asked for is exit 2 (ABSENT).
// keywordFields is every Ruby keyword the field access rule carries, sorted:
// the whole of rubyKeywords, which holds no word a schema name cannot spell.
func keywordFields() []string {
	return slices.Sorted(maps.Keys(rubyKeywords))
}

// keywordRecord is a module holding the record class whose fields are every
// keyword, emitted by recordClass.
func keywordRecord() string {
	return "# frozen_string_literal: true\n\nmodule KeywordFields\n" + recordClass("Keywords", keywordFields(), "  ") + "end\n"
}

// keywordProbe writes each field through an explicit receiver, copies the
// record, then answers the field count, whether the copy equals the original
// and reads back every value, and the hash's keys in order.
func keywordProbe(words []string) string {
	var b strings.Builder
	b.WriteString("# frozen_string_literal: true\n\nrequire_relative \"keyword_fields\"\n\ndef hex(text) = text.to_s.unpack1(\"H*\")\n\na = KeywordFields::Keywords.new\n")
	reads := make([]string, len(words))
	values := make([]string, len(words))
	for i, w := range words {
		fmt.Fprintf(&b, "%s\n", fieldWrite("a", w, fmt.Sprint(i+1)))
		reads[i] = fieldRead("b", w)
		values[i] = fmt.Sprint(i + 1)
	}
	b.WriteString("b = KeywordFields::Keywords.new." + HelperPrefix + "copy_from(a)\n")
	b.WriteString("puts hex(b.to_h.size)\n")
	fmt.Fprintf(&b, "puts hex(b == a && [%s] == [%s])\n", strings.Join(reads, ", "), strings.Join(values, ", "))
	b.WriteString("puts hex(b.to_h.keys.join(\" \"))\n")
	return b.String()
}

// noBareField refuses a line that writes a field as a bare local (`next = `)
// or reads one bare at the end of an assignment (`= next`): the two shapes a
// keyword cannot take. The interpreter run is the whole proof; this names the
// line first.
func noBareField(src string, words []string) error {
	for line := range strings.SplitSeq(src, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, w := range words {
			if strings.HasPrefix(trimmed, w+" = ") || strings.HasSuffix(trimmed, "= "+w) {
				return fmt.Errorf("the record reaches the field %s bare: %s", w, trimmed)
			}
		}
	}
	return nil
}

// plantBareNext is the field access rule's fault: next written as a bare
// local, in a copy of the record.
func plantBareNext(t *testing.T, src string) string {
	t.Helper()
	planted := strings.Replace(src, "self.next = other.next", "next = other.next", 1)
	if planted == src {
		t.Fatal("the keyword record has no self.next = other.next to plant the fault in")
	}
	return planted
}

func driverSurfaceContract(t *testing.T, driver, surfacesDir, dir string) error {
	slowtest.Gate(t, "Ruby conformance driver")
	rubyBin := rubyrun.Interpreter(t)
	manifest := filepath.Join(dir, "MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte("# empty manifest\n"), 0o644); err != nil {
		return err
	}
	list := testproc.Command(t, "sh", driver, manifest, "list")
	list.Dir = dir
	list.Env = append(os.Environ(), "RUBY="+rubyBin)
	out, err := list.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s list: %w\n%s", driver, err, out)
	}
	listed := strings.Fields(string(out))
	if len(listed) == 0 {
		return fmt.Errorf("%s list returned no surfaces", driver)
	}
	for _, surface := range listed {
		if _, statErr := os.Stat(filepath.Join(surfacesDir, surface+".rb")); statErr != nil {
			return fmt.Errorf("%s lists the unimplemented surface %q", driver, surface)
		}
	}
	cmd := testproc.Command(t, "sh", driver, manifest, "unknown-absent-surface", filepath.Join(dir, "out"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "RUBY="+rubyBin)
	out, err = cmd.CombinedOutput()
	if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != 2 {
		return fmt.Errorf("%s unknown-absent-surface: %w, want exit 2 (ABSENT)\n%s", driver, err, out)
	}
	return nil
}

const loadProbe = `require_relative "hello"

def hex(text) = text.to_s.unpack1("H*")

puts hex(format("%016x", Hello::PROTOCOL_ID))
puts hex([Hello::Refused.superclass, Hello::InvalidValue.superclass].join(" "))
refused = Hello::Refused.new("past_end", "a short read")
puts hex("#{refused.reason} #{refused.message}")
puts hex(Hello::Refused.new("past_end").message)
invalid = Hello::InvalidValue.new("Player.name", "a lone surrogate")
puts hex("#{invalid.path} #{invalid.rule} #{invalid.message}")
puts hex([Hello::Refused.name, Hello::InvalidValue.name].join(" "))
`

var loadWant = []string{
	"0123456789abcdef",
	"StandardError StandardError",
	"past_end past_end: a short read",
	"past_end",
	"Player.name a lone surrogate Player.name: a lone surrogate",
	"Hello::Refused Hello::InvalidValue",
}

const twoUnitsProbe = `require_relative "hello"
require_relative "my_game"

def hex(text) = text.to_s.unpack1("H*")

puts hex(Hello::Refused.equal?(MyGame::Refused))
puts hex(format("%016x %016x", Hello::PROTOCOL_ID, MyGame::PROTOCOL_ID))
begin
  raise MyGame::Refused, "past_end"
rescue Hello::Refused
  puts hex("caught by the other unit")
rescue StandardError => e
  puts hex(e.class.name)
end
`

const pinsProbe = `require "rubygems"
require "did_you_mean"
require "error_highlight"
require "syntax_suggest"

def hex(text) = text.to_s.unpack1("H*")

puts hex(RUBY_VERSION)
puts hex(Object.constants.map(&:to_s).sort.join(" "))
# Ripper is the probe's own tool, loaded after the constants are read.
require "ripper"
words = File.read("keywords.txt").split
puts hex(["unlexed:", *words.reject { |w| Ripper.lex(w).dig(0, 1) == :on_kw }].join(" "))
`

// TestRubySkeleton is the Ruby leg's skeleton (docs/plans/ruby-leg.md, R3;
// docs/PORT-RUBY.md). The pure half: a unit that declares nothing generates
// its namespace deterministically, with require_relative only; every
// declaration kind is refused by name, naming the layer that carries it; every
// identifier rule refuses its case by name and its legal neighbours generate,
// and each rule, turned off, lets its case through (the rules' negative
// control); the checker's Ruby row is the keyword list less next and alias;
// a record whose fields are every keyword reaches none bare; the conformance
// driver answers every surface absent. The slow half (SCHEMA_SLOW, make
// test-ruby) runs the pinned interpreters: the namespace loads under both with
// no warning under ruby -w, two units keep their own errors, the keyword
// record runs, the name lists are the pins', and the warning, load and keyword
// gates each go red on a planted fault. Every control is a subtest named
// *-negative-control (make ruby-skeleton-negative-control).
func TestRubySkeleton(t *testing.T) {
	t.Parallel()

	t.Run("generates", func(t *testing.T) {
		t.Parallel()
		files := generate(t, emptyUnit("hello", "Wire", "Hello"))
		want := []string{"hello.rb", "hello.rbs", "hello/Hello.rb", "hello/Wire.rb", "hello/_schema.rb", "hello/serialize.rb", "hello/serialize/core.rb"}
		if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
			t.Fatalf("emitted %v\nwant    %v", got, want)
		}
		for rel, data := range files {
			if !bytes.HasPrefix(data, []byte("# frozen_string_literal: true\n")) {
				t.Errorf("%s does not open with the frozen_string_literal magic comment:\n%s", rel, data)
			}
		}
		unit := string(files["hello.rb"])
		for _, line := range []string{
			"# Code generated by the schema compiler from the unit hello. DO NOT EDIT.",
			`require_relative "hello/_schema"`,
			`require_relative "hello/Hello"`,
			`require_relative "hello/Wire"`,
			"module Hello",
			"  PROTOCOL_ID = 0x0123456789abcdef",
		} {
			if !strings.Contains(unit, line+"\n") {
				t.Errorf("hello.rb lacks the line %q:\n%s", line, unit)
			}
		}
		if f := string(files["hello/Wire.rb"]); !strings.Contains(f, "from Wire.schema. DO NOT EDIT.\n") || !strings.Contains(f, "\nmodule Hello\nend\n") {
			t.Errorf("hello/Wire.rb does not reopen the unit module:\n%s", f)
		}
		runtime := string(files["hello/_schema.rb"])
		if !strings.Contains(runtime, "\nmodule Hello\n") || strings.Contains(runtime, "SchemaRuntimeUnit") {
			t.Errorf("hello/_schema.rb does not open the unit's own module:\n%s", runtime)
		}
		if err := requireRelativeOnly(files); err != nil {
			t.Error(err)
		}
		again := generate(t, emptyUnit("hello", "Hello", "Wire"))
		for rel, data := range files {
			if !bytes.Equal(again[rel], data) {
				t.Errorf("%s differs between two generations of the same unit", rel)
			}
		}
		if got := ModuleName("vold_cfloat_res_refine"); got != "VoldCfloatResRefine" {
			t.Errorf("ModuleName(vold_cfloat_res_refine) = %s, want VoldCfloatResRefine", got)
		}
	})

	t.Run("require-relative-negative-control", func(t *testing.T) {
		t.Parallel()
		files := generate(t, emptyUnit("hello", "Hello"))
		files["hello/Hello.rb"] = append(files["hello/Hello.rb"], []byte("require \"json\"\n")...)
		err := requireRelativeOnly(files)
		if err == nil || !strings.Contains(err.Error(), "hello/Hello.rb:") || !strings.Contains(err.Error(), "with require, not require_relative") {
			t.Fatalf("a planted require went %v; want red naming hello/Hello.rb", err)
		}
	})

	t.Run("refuses-declarations", func(t *testing.T) {
		t.Parallel()
		for _, decl := range []ir.Decl{
			&ir.Const{Name: "MaxPlayers"},
			&ir.Enum{Name: "Weapon", Variants: []string{"Laser"}, Max: 1},
			&ir.Flags{Name: "Caps", Variants: []string{"Jump"}, WireBits: 1},
			&ir.Struct{Name: "Player"},
			&ir.Union{Name: "Event"},
		} {
			if _, err := Generate(withDecl(decl)); err != nil {
				t.Errorf("%T: packet declaration refused in R4: %v", decl, err)
			}
		}
		ptrStruct := &ir.Struct{Name: "Node", Fields: []*ir.Field{{Name: "child", Type: ir.FieldType{Kind: ir.TNamed, Name: "Node", Pointer: true}}}}
		if _, err := Generate(withDecl(ptrStruct)); err == nil || !strings.Contains(err.Error(), "Node's field child uses pointer") {
			t.Errorf("pointer field in packet struct: got %v", err)
		}
		u := emptyUnit("game", "Game")
		u.Files[0].Tables = []*ir.Struct{{Name: "Fleet", IsTable: true}}
		if _, err := Generate(u); err == nil || !strings.Contains(err.Error(), "declares table Fleet, and the Ruby target does not carry tables yet (docs/plans/ruby-leg.md, R5 fixed tables, R6 variable tables)") {
			t.Errorf("a table: got %v", err)
		}
		u = emptyUnit("game", "Game")
		u.Files[0].TableUnions = []*ir.Union{{Name: "Pick"}}
		if _, err := Generate(u); err == nil || !strings.Contains(err.Error(), "declares union Pick with a table arm, and the Ruby target does not carry tables yet (docs/plans/ruby-leg.md, R6 variable tables)") {
			t.Errorf("a table union: got %v", err)
		}
	})

	refused := []struct {
		name, rule string
		unit       *ir.Unit
		want       string
	}{
		{"package segment starts with a digit", rulePackageSpelling, emptyUnit("game_2", "Game"), `the package "game_2" does not map to a Ruby module constant`},
		{"package leading underscore", rulePackageSpelling, emptyUnit("_game", "Game"), `the package "_game" does not map to a Ruby module constant`},
		{"package double underscore", rulePackageSpelling, emptyUnit("my__game", "Game"), `the package "my__game" does not map to a Ruby module constant`},
		{"package not an identifier", rulePackageSpelling, emptyUnit("my-game", "Game"), `the package "my-game" does not map to a Ruby module constant`},
		{"package is a core constant", rulePackageConstant, emptyUnit("string", "Game"), "the package string becomes the module String, a constant the Ruby interpreter already defines"},
		{"package is a RubyGems constant", rulePackageConstant, emptyUnit("gem", "Game"), "the package gem becomes the module Gem"},
		{"package is a 4.0-only constant", rulePackageConstant, emptyUnit("pathname", "Game"), "the package pathname becomes the module Pathname"},
		{"file not an identifier", ruleFileIdentifier, emptyUnit("game", "my-file"), "my-file is not a Ruby identifier"},
		{"file named for the runtime", ruleFileReserved, emptyUnit("game", "_schema"), "which folds to the runtime's _schema.rb"},
		{"file named for serialize.rb, folded", ruleFileReserved, emptyUnit("game", "Serialize"), "which folds to the runtime's serialize.rb"},
		{"files differ only in case", ruleFileCaseFold, emptyUnit("game", "Wire", "wire"), "differ only in case"},
		{"type lowercase", ruleConstantSpelling, withDecl(&ir.Struct{Name: "player"}), "type player is not a Ruby constant"},
		{"const lowercase", ruleConstantSpelling, withDecl(&ir.Const{Name: "max_players"}), "const max_players is not a Ruby constant"},
		{"enum leading underscore", ruleConstantSpelling, withDecl(&ir.Enum{Name: "_Weapon"}), "enum _Weapon is not a Ruby constant"},
		{"variant leading underscore", ruleConstantSpelling, withDecl(&ir.Enum{Name: "Weapon", Variants: []string{"_laser"}}), "Weapon's variant _laser is not a Ruby constant"},
		{"variant constant collision", ruleConstantSpelling, withDecl(&ir.Enum{Name: "Weapon", Variants: []string{"laser", "Laser"}}), "Weapon's variant Laser spells constant Laser, colliding with laser"},
		{"flag leading underscore", ruleConstantSpelling, withDecl(&ir.Flags{Name: "Caps", Variants: []string{"_fly"}}), "Caps's flag _fly is not a Ruby constant"},
		{"type named like a core constant", ruleCoreConstant, withDecl(&ir.Struct{Name: "Integer"}), "type Integer is a constant the Ruby interpreter or the generated runtime defines"},
		{"union named like a runtime name", ruleCoreConstant, withDecl(&ir.Union{Name: "Refused"}), "union Refused is a constant the Ruby interpreter or the generated runtime defines"},
		{"const named PROTOCOL_ID", ruleCoreConstant, withDecl(&ir.Const{Name: "PROTOCOL_ID"}), "const PROTOCOL_ID is a constant the Ruby interpreter or the generated runtime defines"},
		{"field double underscore", ruleDoubleUnderscore, withDecl(&ir.Struct{Name: "Player", Fields: fields("__secret")}), "Player's field __secret starts with __"},
		{"field helper prefix", ruleHelperPrefix, withDecl(&ir.Struct{Name: "Player", Fields: fields("_s_hp")}), "Player's field _s_hp starts with _s_"},
		{"field generated member", ruleGeneratedMember, withDecl(&ir.Struct{Name: "Player", Fields: fields("save")}), "Player's field save is the name of a member the generated class defines"},
		{"field generated member read_bits", ruleGeneratedMember, withDecl(&ir.Struct{Name: "Player", Fields: fields("read_bits")}), "Player's field read_bits is the name of a member"},
		{"arm generated member", ruleGeneratedMember, withDecl(&ir.Union{Name: "Event", Variants: []ir.UnionVariant{{Name: "read"}}}), "union Event's arm read is the name of a member the generated union class defines"},
		{"arm named type", ruleGeneratedMember, withDecl(&ir.Union{Name: "Event", Variants: []ir.UnionVariant{{Name: "type"}}}), "union Event's arm type is the name of a member the generated union class defines"},
		// RUBY-1: a field named to_h was replaced by the generated to_h, and
		// public_send is how the descriptor codec and the table runtime reach
		// a field by name.
		{"field named to_h", ruleGeneratedMember, withDecl(&ir.Struct{Name: "Player", Fields: fields("to_h")}), "Player's field to_h is the name of a member the generated class defines"},
		{"field named public_send", ruleGeneratedMember, withDecl(&ir.Struct{Name: "Player", Fields: fields("public_send")}), "Player's field public_send is the name of a member the generated class defines"},
		{"arm named public_send", ruleGeneratedMember, withDecl(&ir.Union{Name: "Event", Variants: []ir.UnionVariant{{Name: "public_send"}}}), "union Event's arm public_send is the name of a member the generated union class defines"},
		{"table field named to_json", ruleGeneratedMember, withTable(&ir.Struct{Name: "Fleet", IsTable: true, Fields: fields("to_json")}), "Fleet's field to_json is the name of a member the generated table class defines"},
		{"table field named cook", ruleGeneratedMember, withTable(&ir.Struct{Name: "Fleet", IsTable: true, Fields: fields("cook")}), "Fleet's field cook is the name of a member the generated table class defines"},
		{"table field named measure", ruleGeneratedMember, withTable(&ir.Struct{Name: "Fleet", IsTable: true, Fields: fields("measure")}), "Fleet's field measure is the name of a member the generated table class defines"},
		// an arm whose tag constant spells the tag module's NAMES would
		// redefine the name table
		{"arm constant NAMES", ruleConstantSpelling, withDecl(&ir.Union{Name: "Event", Variants: []ir.UnionVariant{{Name: "NAMES"}}}), "union Event's arm NAMES spells the constant EventType::NAMES, colliding with built-in"},
	}

	t.Run("identifier-rules", func(t *testing.T) {
		t.Parallel()
		for _, c := range refused {
			err := CheckNames(c.unit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: got %v, want a refusal containing %q", c.name, err, c.want)
			}
			if _, gerr := Generate(c.unit); gerr == nil {
				t.Errorf("%s: Generate emitted a unit CheckNames refuses", c.name)
			}
		}
		for _, c := range []struct {
			name string
			unit *ir.Unit
		}{
			{"package in segments", emptyUnit("my_game", "Game")},
			{"package with a digit inside a segment", emptyUnit("tblw2", "Game")},
			{"file named like a core constant", emptyUnit("game", "String", "json")},
			{"field named like a builtin method", withDecl(&ir.Struct{Name: "Player", Fields: fields("list", "range", "string", "type", "to_s")})},
			{"field with one leading underscore", withDecl(&ir.Struct{Name: "Player", Fields: fields("_hp")})},
			{"type with a digit and an underscore", withDecl(&ir.Struct{Name: "Player_2"})},
			{"enum variant in capitals", withDecl(&ir.Enum{Name: "Weapon", Variants: []string{"LASER", "Rail"}})},
			{"enum variant lowercase exported", withDecl(&ir.Enum{Name: "Tier", Variants: []string{"flash", "pro"}})},
			{"type fields named for table members", withDecl(&ir.Struct{Name: "Player", Fields: fields("measure", "cook", "to_json", "save_text")})},
			{"union arm named save", withDecl(&ir.Union{Name: "ToolBody", Variants: []ir.UnionVariant{{Name: "save"}}})},
			{"arm named like a field", withDecl(&ir.Union{Name: "Event", Variants: []ir.UnionVariant{{Name: "Spawn"}, {Name: "hit"}}})},
			// the field access rule carries every keyword (fields.go)
			{"fields named next and alias", withDecl(&ir.Struct{Name: "Node", Fields: fields("next", "alias")})},
			{"fields named for other keywords", withDecl(&ir.Struct{Name: "Span", Fields: fields("begin", "end", "if", "then")})},
			{"arm named for a keyword", withDecl(&ir.Union{Name: "Event", Variants: []ir.UnionVariant{{Name: "begin"}, {Name: "next"}}})},
		} {
			if err := CheckNames(c.unit); err != nil {
				t.Errorf("%s: refused: %v", c.name, err)
			}
		}
	})

	// THE RULES' NEGATIVE CONTROL: each refused case, with its own rule
	// turned off, passes. A case another rule also caught would stay red, so
	// this proves each rule is the one, and the only one, that holds its case.
	t.Run("identifier-rules-negative-control", func(t *testing.T) {
		t.Parallel()
		ruled := map[string]bool{}
		for _, c := range refused {
			ruled[c.rule] = true
			if err := checkNames(c.unit, c.rule); err != nil {
				t.Errorf("%s: with %s off, still refused: %v", c.name, c.rule, err)
			}
		}
		for _, rule := range []string{rulePackageSpelling, rulePackageConstant, ruleFileIdentifier, ruleFileReserved, ruleFileCaseFold, ruleConstantSpelling, ruleCoreConstant, ruleDoubleUnderscore, ruleHelperPrefix, ruleGeneratedMember} {
			if !ruled[rule] {
				t.Errorf("rule %s has no refused case, so nothing shows it holds", rule)
			}
		}
	})

	// The checker's Ruby row (internal/check) is the leg's keyword list less
	// next and alias, which the shared corpus spells as fields and the field
	// access rule carries (docs/PORT-RUBY.md, IDENTIFIERS).
	t.Run("reserved-row", func(t *testing.T) {
		t.Parallel()
		row := check.ReservedIn("Ruby")
		row["next"], row["alias"] = true, true
		if !maps.Equal(row, rubyKeywords) {
			t.Errorf("the checker's Ruby row plus next and alias is not rubyKeywords:\nrow  %v\nlist %v", slices.Sorted(maps.Keys(row)), slices.Sorted(maps.Keys(rubyKeywords)))
		}
		if lang, ok := check.ReservedLanguageOf("ruby"); !ok || lang != "Ruby" {
			t.Errorf("check.ReservedLanguageOf(ruby) = %q, %v; want Ruby", lang, ok)
		}
	})

	// THE FIELD ACCESS RULE, pure: a record whose fields are every keyword
	// reaches each through an explicit receiver, a symbol, a label or an
	// instance variable, and no line of it starts with a bare field name.
	t.Run("keyword-fields", func(t *testing.T) {
		t.Parallel()
		src := keywordRecord()
		allKeywordFields := keywordFields()
		if len(allKeywordFields) == 0 {
			t.Fatal("keywordFields() is empty: the loop below would check nothing")
		}
		for _, w := range allKeywordFields {
			for _, want := range []string{
				":" + w,
				"@" + w + " = 0",
				"self." + w + " = other." + w,
				w + ": @" + w,
			} {
				if !strings.Contains(src, want) {
					t.Errorf("the keyword record reaches %s without %q", w, want)
				}
			}
		}
		if err := noBareField(src, keywordFields()); err != nil {
			t.Error(err)
		}
	})

	// Its control: a copy that writes next as a bare local.
	t.Run("keyword-fields-shape-negative-control", func(t *testing.T) {
		t.Parallel()
		if err := noBareField(plantBareNext(t, keywordRecord()), keywordFields()); err == nil || !strings.Contains(err.Error(), "next = other.next") {
			t.Fatalf("a planted bare next went %v; want red naming the line", err)
		}
	})

	t.Run("driver", func(t *testing.T) {
		t.Parallel()
		root, err := rubyrun.RepoRoot()
		if err != nil {
			t.Fatal(err)
		}
		conformanceDir := filepath.Join(root, "test", "conformance", "ruby")
		if err := driverSurfaceContract(t, filepath.Join(conformanceDir, "driver"), filepath.Join(conformanceDir, "surfaces"), t.TempDir()); err != nil {
			t.Fatal(err)
		}
	})

	// The driver gate's control: a copy that lists a surface it cannot answer.
	t.Run("driver-negative-control", func(t *testing.T) {
		t.Parallel()
		root, err := rubyrun.RepoRoot()
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		src, err := os.ReadFile(filepath.Join(root, "test", "conformance", "ruby", "main.rb"))
		if err != nil {
			t.Fatal(err)
		}
		anchor := "surface_names.each { |name| puts name }"
		plantedMain := strings.Replace(string(src), anchor, anchor+"\n    puts 'planted-unimplemented-surface'", 1)
		if plantedMain == string(src) {
			t.Fatal("main.rb has no surface_names anchor to plant a surface in")
		}
		if err := os.WriteFile(filepath.Join(dir, "main.rb"), []byte(plantedMain), 0o644); err != nil {
			t.Fatal(err)
		}
		surfacesSrc := filepath.Join(root, "test", "conformance", "ruby", "surfaces")
		surfacesDst := filepath.Join(dir, "surfaces")
		if err := os.MkdirAll(surfacesDst, 0o755); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(surfacesSrc)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				b, rerr := os.ReadFile(filepath.Join(surfacesSrc, e.Name()))
				if rerr != nil {
					t.Fatal(rerr)
				}
				if werr := os.WriteFile(filepath.Join(surfacesDst, e.Name()), b, 0o644); werr != nil {
					t.Fatal(werr)
				}
			}
		}
		wrapper := "#!/bin/sh\nset -eu\nexec \"$RUBY\" --disable-gems -w \"" + filepath.Join(dir, "main.rb") + "\" \"$@\"\n"
		copyPath := filepath.Join(dir, "driver")
		if err := os.WriteFile(copyPath, []byte(wrapper), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := driverSurfaceContract(t, copyPath, surfacesDst, dir); err == nil || !strings.Contains(err.Error(), `lists the unimplemented surface "planted-unimplemented-surface"`) {
			t.Fatalf("a driver listing planted-unimplemented-surface went %v; want red naming the surface", err)
		}
	})

	pins := []struct {
		name string
		run  func(testing.TB, string, string, map[string][]byte) []byte
		pin  func(testing.TB) string
	}{{"current", rubyrun.Run, rubyrun.Interpreter}, {"floor", rubyrun.RunFloor, rubyrun.Floor}}

	for _, p := range pins {
		t.Run("loads/"+p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, generate(t, emptyUnit("hello", "Hello", "Wire", "Set")))
			lines := rubyrun.HexLines(t, p.run(t, dir, "main.rb", map[string][]byte{"main.rb": []byte(loadProbe)}))
			var got []string
			for _, l := range lines {
				got = append(got, string(l))
			}
			if !slices.Equal(got, loadWant) {
				t.Fatalf("probe answered\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(loadWant, "\n"))
			}
		})

		t.Run("units-keep-their-own-errors/"+p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, generate(t, emptyUnit("hello", "Hello")))
			other := emptyUnit("my_game", "Game")
			other.ProtocolId = 0x11
			writeTree(t, dir, generate(t, other))
			lines := rubyrun.HexLines(t, p.run(t, dir, "main.rb", map[string][]byte{"main.rb": []byte(twoUnitsProbe)}))
			want := []string{"false", "0123456789abcdef 0000000000000011", "MyGame::Refused"}
			var got []string
			for _, l := range lines {
				got = append(got, string(l))
			}
			if !slices.Equal(got, want) {
				t.Fatalf("probe answered %q, want %q", got, want)
			}
		})

		// The warning gate's control: a copy of a generated file with a line
		// ruby -w warns about must make Run's check red, naming the file.
		t.Run("warning-negative-control/"+p.name, func(t *testing.T) {
			t.Parallel()
			files := generate(t, emptyUnit("hello", "Hello"))
			files["hello/Hello.rb"] = append(files["hello/Hello.rb"], []byte("def self._s_planted\n  unused = 1\nend\n")...)
			dir := t.TempDir()
			writeTree(t, dir, files)
			_, stderr, err := rubyrun.Try(t, p.pin(t), dir, "main.rb", map[string][]byte{"main.rb": []byte(loadProbe)})
			if err != nil {
				t.Fatalf("the planted warning stopped the load: %v\n%s", err, stderr)
			}
			if !strings.Contains(stderr, "Hello.rb") || !strings.Contains(stderr, "assigned but unused variable - unused") {
				t.Fatalf("ruby -w did not warn on the planted line; stderr:\n%s", stderr)
			}
		})

		// The load gate's control: the unit without its runtime must not
		// load, and the failure names the runtime file.
		t.Run("runtime-negative-control/"+p.name, func(t *testing.T) {
			t.Parallel()
			files := generate(t, emptyUnit("hello", "Hello"))
			delete(files, "hello/_schema.rb")
			dir := t.TempDir()
			writeTree(t, dir, files)
			_, stderr, err := rubyrun.Try(t, p.pin(t), dir, "main.rb", map[string][]byte{"main.rb": []byte(loadProbe)})
			if err == nil || !strings.Contains(stderr, "hello/_schema") {
				t.Fatalf("a unit with no runtime loaded (%v); stderr:\n%s", err, stderr)
			}
		})

		// THE FIELD ACCESS RULE under the pin: the record whose fields are
		// every keyword loads under ruby -w with no warning, each field is
		// written and read through an explicit receiver, copied, hashed by
		// label and compared.
		t.Run("keyword-fields/"+p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			words := keywordFields()
			lines := rubyrun.HexLines(t, p.run(t, dir, "main.rb", map[string][]byte{
				"keyword_fields.rb": []byte(keywordRecord()),
				"main.rb":           []byte(keywordProbe(words)),
			}))
			want := []string{fmt.Sprint(len(words)), "true", strings.Join(words, " ")}
			var got []string
			for _, l := range lines {
				got = append(got, string(l))
			}
			if !slices.Equal(got, want) {
				t.Fatalf("keyword probe answered %q, want %q", got, want)
			}
		})

		// Its control: the same record with next written as a bare local
		// must not load, and the failure names the file.
		t.Run("keyword-fields-negative-control/"+p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			_, stderr, err := rubyrun.Try(t, p.pin(t), dir, "main.rb", map[string][]byte{
				"keyword_fields.rb": []byte(plantBareNext(t, keywordRecord())),
				"main.rb":           []byte(keywordProbe(keywordFields())),
			})
			if err == nil || !strings.Contains(stderr, "keyword_fields.rb") {
				t.Fatalf("a record writing next as a bare local loaded (%v); stderr:\n%s", err, stderr)
			}
		})

		// The name lists are the pins': each keyword lexes as a keyword, and
		// the core constants are the union of both pins' Object.constants
		// with RubyGems and the default gems loaded.
		t.Run("pins/"+p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			words := strings.Join(slices.Sorted(maps.Keys(rubyKeywords)), " ")
			lines := rubyrun.HexLines(t, p.run(t, dir, "main.rb", map[string][]byte{"main.rb": []byte(pinsProbe), "keywords.txt": []byte(words)}))
			if len(lines) != 3 {
				t.Fatalf("pins probe answered %d lines, want 3", len(lines))
			}
			version := map[string]string{"current": rubyrun.CurrentVersion, "floor": rubyrun.FloorVersion}[p.name]
			if string(lines[0]) != version {
				t.Fatalf("the %s pin is Ruby %s, want %s", p.name, lines[0], version)
			}
			constants := 0
			for c := range strings.FieldsSeq(string(lines[1])) {
				constants++
				if !rubyCoreConstants[c] {
					t.Errorf("rubyCoreConstants lacks %s, which Ruby %s defines", c, version)
				}
			}
			// Object.constants always holds Object itself: an empty line is a
			// probe that read nothing, not an interpreter with no constants
			if constants == 0 {
				t.Fatalf("Ruby %s's pins probe listed no Object.constants: the core-constants check checked nothing", version)
			}
			// The probe heads the line, so an empty answer is still a line.
			if s := string(lines[2]); s != "unlexed:" {
				t.Errorf("Ruby %s does not lex these rubyKeywords as keywords: %s", version, strings.TrimPrefix(s, "unlexed: "))
			}
		})
	}

	// Every constant of the list is some pin's, so the list holds nothing
	// neither interpreter defines.
	t.Run("pins/union", func(t *testing.T) {
		t.Parallel()
		seen := map[string]bool{}
		for _, p := range pins {
			dir := t.TempDir()
			lines := rubyrun.HexLines(t, p.run(t, dir, "main.rb", map[string][]byte{"main.rb": []byte(pinsProbe), "keywords.txt": nil}))
			for c := range strings.FieldsSeq(string(lines[1])) {
				seen[c] = true
			}
		}
		if len(rubyCoreConstants) == 0 {
			t.Fatal("rubyCoreConstants is empty: the loop below would check nothing")
		}
		for c := range rubyCoreConstants {
			if !seen[c] {
				t.Errorf("rubyCoreConstants has %s, which neither pin defines", c)
			}
		}
	})
}
