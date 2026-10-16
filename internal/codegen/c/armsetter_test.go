package c

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The C half of #449: selecting a union arm by assigning the tag and the arm
// separately lets a forgotten tag leave None, and the payload is then lost on
// the wire with no assert. Every arm therefore gets a generated selector that
// sets the tag and initializes the arm together, by the same rule a read
// uses (declared defaults through new_<type>, else the zero form), and
// returns the fresh arm; a payload-free arm's selector sets the tag alone.
//
// This test pins four things:
//   - the exact selector text for a payload arm (examples/ArmDefaults.schema's
//     DefaultChoice.first, whose payload carries defaults), a payload arm with
//     no defaults (the memset form), and a payload-free arm;
//   - the wire headers (every write_*/read_* body) of every example are
//     byte-identical to the committed goldens, so the selectors moved nothing
//     on the wire;
//   - a compiled C round trip of every arm of every example union, and of a
//     mixed and a payload-free-only union, selected through the selector:
//     tag set, arm equal to a declared-default arm, write, read, re-encode;
//   - the same round trip with every selector's tag assignment deleted goes
//     red on the tag check, so the harness sees a forgotten tag.
func TestUnionArmSetterSetsTagAndArmC(t *testing.T) {
	t.Parallel()

	examples := loadExamplesUnit(t)
	exampleFiles, err := Generate(examples)
	if err != nil {
		t.Fatal(err)
	}
	mixed := unitFromSource(t, "SetterArms.schema", setterArmsSource)
	mixedFiles, err := Generate(mixed)
	if err != nil {
		t.Fatal(err)
	}

	armDefaults := string(exampleFiles["ArmDefaults.h"])
	arms := string(mixedFiles["SetterArms.h"])
	for _, c := range []struct{ header, what, want string }{
		{armDefaults, "a payload arm with declared defaults", "static SCHEMA_UNUSED DefaultArm * default_choice_select_first( DefaultChoice * value )\n{\n" +
			"    value->type = DEFAULT_CHOICE_TYPE_FIRST;\n" +
			"    value->as.first = new_default_arm();\n" +
			"    return &value->as.first;\n}\n"},
		{armDefaults, "a second payload arm", "static SCHEMA_UNUSED DefaultArm * default_choice_select_second( DefaultChoice * value )\n{\n" +
			"    value->type = DEFAULT_CHOICE_TYPE_SECOND;\n" +
			"    value->as.second = new_default_arm();\n" +
			"    return &value->as.second;\n}\n"},
		{arms, "a payload arm without declared defaults", "static SCHEMA_UNUSED SetterPlain * setter_choice_select_plain( SetterChoice * value )\n{\n" +
			"    value->type = SETTER_CHOICE_TYPE_PLAIN;\n" +
			"    memset( &value->as.plain, 0, sizeof( value->as.plain ) );\n" +
			"    return &value->as.plain;\n}\n"},
		{arms, "a payload-free arm", "static SCHEMA_UNUSED void setter_choice_select_ping( SetterChoice * value )\n{\n" +
			"    value->type = SETTER_CHOICE_TYPE_PING;\n}\n"},
		{arms, "a payload-free arm of a tag-only union", "static SCHEMA_UNUSED void setter_signals_select_pong( SetterSignals * value )\n{\n" +
			"    value->type = SETTER_SIGNALS_TYPE_PONG;\n}\n"},
	} {
		if !strings.Contains(c.header, c.want) {
			t.Errorf("%s needs a selector that sets the tag and the arm together; want:\n%s", c.what, c.want)
		}
	}
	if strings.Contains(arms, "setter_empty_select_") {
		t.Error("a union with no arms has nothing to select and must get no selector")
	}

	// The wire must not move: every example's write/read pair is the
	// committed golden, byte for byte, and no selector rides in a wire header.
	for name, data := range exampleFiles {
		if !isWireHeader(examples, name) {
			continue
		}
		golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "golden", "c", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(golden) {
			t.Errorf("%s moved: the selectors are data-header only, and every write_*/read_* body stays byte-identical to the golden", name)
		}
		if strings.Contains(string(data), "_select_") {
			t.Errorf("%s carries a selector; selectors belong to the data header", name)
		}
	}

	t.Run("round trip through every selector", func(t *testing.T) {
		t.Parallel()
		for _, p := range []struct {
			name  string
			unit  *ir.Unit
			files map[string][]byte
		}{{"examples", examples, exampleFiles}, {"setter-arms", mixed, mixedFiles}} {
			out, err := runSetterProbe(t, p.unit, p.files, nil)
			if err != nil {
				t.Fatalf("%s: %v\n%s", p.name, err, out)
			}
			if !strings.Contains(string(out), "selector round trips: ") {
				t.Fatalf("%s: the probe did not report its round trips:\n%s", p.name, out)
			}
		}
	})

	// THE SELECTOR'S FLAT NAME IS CLAIMED. snake(union) + "_select_" + arm is
	// many-to-one across legal declarations: Foo's arm select_bar and
	// FooSelect's arm bar both spell foo_select_select_bar, one static C
	// function defined twice in one translation unit. The checker claims the
	// spelling in its one registry, so the pair is refused at the source
	// instead of failing the C compile; each union alone still checks, and
	// emits exactly the name the claim names.
	t.Run("colliding selector names are refused by the checker", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			"package t\ntype Arm { x uint8 }\nunion Foo { select_bar Arm }\n",
			"package t\ntype Arm { x uint8 }\nunion FooSelect { bar Arm }\n",
			"package t\nunion Foo { select_bar }\n",
			"package t\nunion FooSelect { bar }\n",
		} {
			files, err := Generate(unitFromSource(t, "Alone.schema", src))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(files["Alone.h"]), " foo_select_select_bar( ") {
				t.Errorf("the claim must name the emitted selector; %q does not emit foo_select_select_bar", src)
			}
		}
		for _, src := range []string{
			"package t\ntype Arm { x uint8 }\nunion Foo { select_bar Arm }\nunion FooSelect { bar Arm }\n",
			"package t\nunion Foo { select_bar }\nunion FooSelect { bar }\n",
			"package t\ntype Arm { x uint8 }\nunion Foo { bar Arm }\ntype foo_select_bar { y uint8 }\n",
		} {
			f, perrs := parser.Parse("Clash.schema", []byte(src))
			if len(perrs) > 0 {
				t.Fatalf("parse: %v", perrs[0])
			}
			_, cerrs := check.Unit([]check.SourceFile{{Path: "Clash.schema", Name: "Clash.schema", Base: "Clash", Bytes: []byte(src), AST: f}})
			if len(cerrs) == 0 {
				t.Errorf("%q checks clean, but its C header defines one symbol twice", src)
				continue
			}
			if msg := fmt.Sprint(cerrs); !strings.Contains(msg, "both generate the symbol foo_select_") || !strings.Contains(msg, "selector (C form)") {
				t.Errorf("%q is refused, but not as a selector collision: %s", src, msg)
			}
		}
	})

	t.Run("a forgotten tag goes red", func(t *testing.T) {
		t.Parallel()
		forget := regexp.MustCompile(`(?m)^    value->type = [A-Z0-9_]+;\n`)
		sabotaged := 0
		out, err := runSetterProbe(t, examples, exampleFiles, func(name string, data []byte) []byte {
			if isWireHeader(examples, name) {
				return data
			}
			sabotaged += len(forget.FindAll(data, -1))
			return forget.ReplaceAll(data, nil)
		})
		if sabotaged == 0 {
			t.Fatal("the control removed no tag assignment, so it proves nothing")
		}
		if err == nil {
			t.Fatalf("selectors that forget the tag stayed green:\n%s", out)
		}
		if !strings.Contains(string(out), "FAILED: default_choice_select_first sets the tag") {
			t.Fatalf("selectors that forget the tag failed without the tag assertion:\n%s", out)
		}
	})
}

// setterArmsSource carries the shapes examples/ lacks: a payload arm with no
// declared defaults, payload-free arms beside payload arms, a tag-only union
// and an empty union.
const setterArmsSource = `package setterarms
type SetterPlain {
    value uint8
}
type SetterDefaulted {
    value int32 = -1
    count [1..3]uint8
}
union SetterChoice {
    ping
    plain SetterPlain
    defaulted SetterDefaulted
    pong
}
union SetterSignals {
    ping
    pong
}
union SetterEmpty {}
`

// isWireHeader reports whether name is a file's wire header (<Base>Wire.h) —
// by the unit's own bases, since examples/Wire.schema's DATA header is Wire.h.
func isWireHeader(u *ir.Unit, name string) bool {
	for _, f := range u.Files {
		if name == f.Base+"Wire.h" {
			return true
		}
	}
	return false
}

func loadExamplesUnit(t *testing.T) *ir.Unit {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "examples")
	paths, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	var files []check.SourceFile
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(p, data)
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		name := filepath.Base(p)
		files = append(files, check.SourceFile{
			Path: p, Name: name, Base: strings.TrimSuffix(name, ".schema"),
			Bytes: data, AST: f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// runSetterProbe writes the generated files (through edit, when given) and a
// probe that round-trips every arm of every union in the unit through its
// selector, compiles them with the flags the Makefile's C corpus test uses,
// and runs the probe.
func runSetterProbe(t *testing.T, u *ir.Unit, files map[string][]byte, edit func(string, []byte) []byte) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	var includes []string
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".h") {
			continue
		}
		if edit != nil {
			data = edit(name, data)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if isWireHeader(u, name) {
			includes = append(includes, name)
		}
	}
	sort.Strings(includes)

	var b strings.Builder
	b.WriteString("#include <stdio.h>\n#include <string.h>\n#include <stdint.h>\n")
	for _, inc := range includes {
		fmt.Fprintf(&b, "#include %q\n", inc)
	}
	b.WriteString(setterProbePrelude)
	var calls []string
	for _, f := range u.Files {
		for _, d := range f.Decls {
			un, ok := d.(*ir.Union)
			if !ok {
				continue
			}
			for _, v := range un.Variants {
				fn := fmt.Sprintf("probe_%s_%s", snake(un.Name), v.Name)
				calls = append(calls, fn)
				emitSetterProbeArm(&b, fn, un, v)
			}
		}
	}
	if len(calls) == 0 {
		t.Fatal("the unit has no union arm to round-trip")
	}
	b.WriteString("int main( void )\n{\n")
	for _, fn := range calls {
		fmt.Fprintf(&b, "    %s();\n", fn)
	}
	fmt.Fprintf(&b, "    if ( failed )\n    {\n        return 1;\n    }\n    printf( \"selector round trips: %d arms\\n\" );\n    return 0;\n}\n", len(calls))
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	serialize, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "serialize.c"))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wtype-limits", "-O2", "-ffp-contract=off",
		"-I" + dir, "-I" + serialize, filepath.Join(dir, "probe.c"), filepath.Join(serialize, "serialize.c"),
		"-o", filepath.Join(dir, "probe"), "-lm"}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the selector probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

const setterProbePrelude = `
static int failed = 0;

static void check( int condition, const char * message )
{
    if ( !condition )
    {
        printf( "FAILED: %s\n", message );
        failed = 1;
    }
}

`

// emitSetterProbeArm emits one arm's round trip: poison the union (arm bytes
// and a None tag, so a forgotten tag or an uninitialized arm shows), select
// the arm through its selector, check the tag and the returned pointer, check
// the encoding equals a hand-built declared-default selection, then read the
// bytes back and check the tag and the re-encoding.
func emitSetterProbeArm(b *strings.Builder, fn string, un *ir.Union, v ir.UnionVariant) {
	sn := snake(un.Name)
	tag := screaming(un.Name+"Type") + "_" + screaming(v.Name)
	setter := fmt.Sprintf("%s_select_%s", sn, v.Name)
	words := fmt.Sprintf("%s_MAX_BYTES / 8 + 2", screaming(un.Name))
	fmt.Fprintf(b, "static void %s( void )\n{\n", fn)
	fmt.Fprintf(b, "    %s value, reference, back;\n", un.Name)
	fmt.Fprintf(b, "    uint64_t sent_words[%s], again_words[%s];\n", words, words)
	b.WriteString("    serialize_write_stream_t w;\n    serialize_read_stream_t r;\n    int sent_bytes, again_bytes;\n")
	b.WriteString("    memset( &value, 0xA5, sizeof( value ) );\n")
	fmt.Fprintf(b, "    value.type = %s_NONE;\n", screaming(un.Name+"Type"))
	if v.Void() {
		fmt.Fprintf(b, "    %s( &value );\n", setter)
	} else {
		fmt.Fprintf(b, "    check( %s( &value ) == &value.as.%s, \"%s returns the selected arm\" );\n", setter, v.Name, setter)
	}
	fmt.Fprintf(b, "    check( value.type == %s, \"%s sets the tag\" );\n", tag, setter)
	b.WriteString("    memset( &reference, 0, sizeof( reference ) );\n")
	fmt.Fprintf(b, "    reference.type = %s;\n", tag)
	if !v.Void() && v.Ref != nil && structHasDefaults(v.Ref) {
		fmt.Fprintf(b, "    reference.as.%s = new_%s();\n", v.Name, snake(v.Type))
	}
	b.WriteString("    memset( sent_words, 0, sizeof( sent_words ) );\n    memset( again_words, 0, sizeof( again_words ) );\n")
	fmt.Fprintf(b, "    serialize_write_stream_init( &w, (uint8_t *) sent_words, sizeof( sent_words ) );\n")
	fmt.Fprintf(b, "    check( write_%s( &w, &value ), \"%s writes\" );\n", sn, setter)
	b.WriteString("    serialize_write_flush( &w );\n    sent_bytes = serialize_write_bytes_processed( &w );\n")
	fmt.Fprintf(b, "    serialize_write_stream_init( &w, (uint8_t *) again_words, sizeof( again_words ) );\n")
	fmt.Fprintf(b, "    check( write_%s( &w, &reference ), \"the declared-default %s selection writes\" );\n", sn, v.Name)
	b.WriteString("    serialize_write_flush( &w );\n    again_bytes = serialize_write_bytes_processed( &w );\n")
	fmt.Fprintf(b, "    check( sent_bytes == again_bytes && memcmp( sent_words, again_words, sizeof( sent_words ) ) == 0, \"%s initializes the arm with its declared defaults\" );\n", setter)
	b.WriteString("    memset( &back, 0xA5, sizeof( back ) );\n")
	b.WriteString("    serialize_read_stream_init( &r, (const uint8_t *) sent_words, sent_bytes );\n")
	fmt.Fprintf(b, "    check( read_%s( &r, &back ), \"%s reads back\" );\n", sn, setter)
	fmt.Fprintf(b, "    check( back.type == %s, \"%s reads back its tag\" );\n", tag, setter)
	b.WriteString("    memset( again_words, 0, sizeof( again_words ) );\n")
	fmt.Fprintf(b, "    serialize_write_stream_init( &w, (uint8_t *) again_words, sizeof( again_words ) );\n")
	fmt.Fprintf(b, "    check( write_%s( &w, &back ), \"%s re-writes\" );\n", sn, setter)
	b.WriteString("    serialize_write_flush( &w );\n")
	fmt.Fprintf(b, "    check( serialize_write_bytes_processed( &w ) == sent_bytes && memcmp( sent_words, again_words, sizeof( sent_words ) ) == 0, \"%s round-trips byte-identical\" );\n", setter)
	b.WriteString("}\n\n")
}
