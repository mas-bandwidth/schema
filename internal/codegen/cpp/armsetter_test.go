// The arm-setter test (issue #449, SPEC §4.8 "the arm setter"): a generated
// C++ union selects an arm in ONE call that sets the tag and constructs the
// arm together, so a forgotten tag can no longer leave None under a populated
// payload. The setter is additive: the wire half of every example header is
// byte-identical to its pinned golden.
package cpp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// examplesUnit loads the whole examples/ corpus: ArmDefaults.schema names
// ProbeConfig from Wire.schema, so the unit is the directory, not the file.
func examplesUnit(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "examples", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("examples corpus: %v (%d files)", err, len(paths))
	}
	sources := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		sources[filepath.Base(p)] = string(b)
	}
	files, err := Generate(unitFromSources(t, sources))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestUnionArmSetterSetsTagAndArm(t *testing.T) {
	t.Parallel()
	files := examplesUnit(t)
	h := string(files["ArmDefaults.h"])

	// A payload arm: the tag and the in-place construction in one member,
	// returning the fresh arm. Each union, each arm.
	for _, c := range []struct{ union, arm, member, typ string }{
		{"DefaultChoice", "First", "first", "DefaultArm"},
		{"DefaultChoice", "Second", "second", "DefaultArm"},
		{"DefaultBulkChoice", "First", "first", "DefaultBulkArm"},
		{"DefaultBulkChoice", "Second", "second", "DefaultBulkArm"},
	} {
		want := "    " + c.typ + " & Select" + c.arm + "() // sets the tag and constructs the arm with its defaults (SPEC §4.8)\n" +
			"    {\n" +
			"        ::new ( static_cast<void *>( &" + c.member + " ) ) " + c.typ + "{};\n" +
			"        type = " + c.union + "Type::" + c.arm + ";\n" +
			"        return " + c.member + ";\n" +
			"    }\n"
		if !strings.Contains(h, want) {
			t.Errorf("ArmDefaults.h: %s lacks the payload-arm setter that sets the tag and the arm together; want\n%s\ngot\n%s", c.union, want, h)
		}
	}
	// the setters live inside the struct, after its constructor
	ctor := strings.Index(h, "DefaultChoice() : type( DefaultChoiceType::None )")
	setter := strings.Index(h, "DefaultArm & SelectFirst()")
	end := strings.Index(h, "inline constexpr int64_t DefaultChoiceMaxBits")
	if ctor == -1 || setter < ctor || setter > end {
		t.Errorf("ArmDefaults.h: SelectFirst is not a member of DefaultChoice (ctor at %d, setter at %d, struct end before %d)", ctor, setter, end)
	}
	// the data header constructs in place now, so it carries <new> itself
	if !strings.Contains(h, "\n#include <new>\n") {
		t.Error("ArmDefaults.h: the setter's placement construction needs a direct <new> include")
	}
	if !strings.Contains(h, "DefaultArm & arm = value.SelectFirst();") {
		t.Error("ArmDefaults.h: the union's doc comment must show selection through the setter")
	}
	if strings.Contains(h, "value.type = DefaultChoiceType::First;") {
		t.Error("ArmDefaults.h: the doc comment still teaches the two-assignment form the setter replaces")
	}

	// A payload-free arm: ArmDefaults.schema has none (adding one would move
	// its wire in all nine targets), so the shape comes from a unit beside it.
	free, err := Generate(unitFromSources(t, map[string]string{
		"Payload.schema": "package t\ntype Payload {\n    value int32 = -1\n}\n",
		"Choice.schema":  "package t\nunion Choice {\n    ping\n    payload Payload\n    pong\n}\n",
		"Signals.schema": "package t\nunion Signals {\n    ping\n    pong\n}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ file, union, arm string }{
		{"Choice.h", "Choice", "Ping"},
		{"Choice.h", "Choice", "Pong"},
		{"Signals.h", "Signals", "Ping"},
		{"Signals.h", "Signals", "Pong"},
	} {
		want := "    void Select" + c.arm + "() // payload-free: the tag is the whole selection (SPEC §4.8)\n" +
			"    {\n" +
			"        type = " + c.union + "Type::" + c.arm + ";\n" +
			"    }\n"
		if !strings.Contains(string(free[c.file]), want) {
			t.Errorf("%s: %s lacks the payload-free setter that sets the tag alone; want\n%s\ngot\n%s", c.file, c.union, want, free[c.file])
		}
	}
	if !strings.Contains(string(free["Choice.h"]), "Payload & SelectPayload()") {
		t.Error("Choice.h: the payload arm beside payload-free arms lost its setter")
	}
	if strings.Contains(string(free["Signals.h"]), "#include <new>") {
		t.Error("Signals.h: a union with no payload arm constructs nothing and must not include <new>")
	}
}

// TestUnionArmSetterLeavesTheWireUnmoved pins the additive rule: the setter is
// a data-header member, so every example's wire header (its Write and Read
// bodies, the union tag wire included) is byte-identical to the pinned golden
// the BASE compiler produced, and every data header still carries its Max
// constants unchanged.
func TestUnionArmSetterLeavesTheWireUnmoved(t *testing.T) {
	t.Parallel()
	files := examplesUnit(t)
	golden := filepath.Join("..", "..", "..", "testdata", "golden", "cpp")
	for name, got := range files {
		want, err := os.ReadFile(filepath.Join(golden, name))
		if err != nil {
			t.Fatalf("%s: no pinned golden: %v", name, err)
		}
		// <Base>Wire.h is a wire header exactly when <Base>.h is beside it
		// (Wire.schema's own data header is Wire.h)
		if _, data := files[strings.TrimSuffix(name, "Wire.h")+".h"]; strings.HasSuffix(name, "Wire.h") && data {
			if string(got) != string(want) {
				t.Errorf("%s moved: the arm setter must not touch a Write or Read body", name)
			}
			continue
		}
		for line := range strings.SplitSeq(string(want), "\n") {
			if strings.Contains(line, "MaxBits =") || strings.Contains(line, "MaxBytes =") {
				if !strings.Contains(string(got), line+"\n") {
					t.Errorf("%s: a Max constant moved: %q", name, line)
				}
			}
		}
	}
}

// TestUnionArmSetterRefusesACollidingName pins the refusal: a setter spelled
// like the union itself (a constructor), its tag enum, or an arm's payload
// type or member cannot compile, so generation names it instead of emitting it.
func TestUnionArmSetterRefusesACollidingName(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"its own name":     "package t\ntype P {\n    v uint8\n}\nunion SelectA {\n    a P\n}\n",
		"its tag enum":     "package t\ntype P {\n    v uint8\n}\nunion SelectX {\n    x_type P\n}\n",
		"a payload type":   "package t\ntype SelectB {\n    v uint8\n}\nunion U {\n    b SelectB\n}\n",
		"a payload member": "package t\ntype P {\n    v uint8\n}\ntype Q {\n    v uint8\n}\nunion U {\n    first P\n    SelectFirst Q\n}\n",
	} {
		_, err := Generate(unitFromSources(t, map[string]string{"S.schema": src}))
		if err == nil || !strings.Contains(err.Error(), "the arm setter") {
			t.Errorf("a setter colliding with %s: want a refusal naming the arm setter, got %v", name, err)
		}
	}
}
