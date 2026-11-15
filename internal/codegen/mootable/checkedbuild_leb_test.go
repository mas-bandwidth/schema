package mootable_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// sceneLengthCases are form-1 wires whose lengths sit at the edge of moo's
// number range: a string length in the root body and the same claim inside a
// nested table body. No file can hold any of them, so every reader refuses.
func sceneLengthCases() []struct {
	name string
	wire []byte
} {
	claims := []struct {
		name  string
		value uint64
	}{
		{"below-marker", (1 << 47) - 2},
		{"max", (1 << 47) - 1},
		{"past", 1 << 47},
		{"seventh-33", 33 << 42},
		{"seventh-127", 127 << 42},
	}
	var cases []struct {
		name string
		wire []byte
	}
	for _, claim := range claims {
		cases = append(cases,
			struct {
				name string
				wire []byte
			}{"root-" + claim.name, sceneRootLengthWire(claim.value)},
			struct {
				name string
				wire []byte
			}{"nested-" + claim.name, sceneNestedLengthWire(claim.value)},
		)
	}
	return cases
}

// sceneWire frames one Scene body with the given id table.
func sceneWire(ids []string, body []byte) []byte {
	wire := append([]byte{1}, body...)
	for _, id := range ids {
		wire = append(wire, u64le(ir.TableWireId(id))...)
	}
	return append(wire, u64le(uint64(len(ids)))...)
}

// sceneRootLengthWire is a root body holding one string field whose length is
// the claim.
func sceneRootLengthWire(claim uint64) []byte {
	body := []byte{1, 12}
	body = append(body, leb(claim)...)
	body = append(body, 0)
	return sceneWire([]string{"name"}, body)
}

// sceneNestedLengthWire is a root body holding a nested table whose string
// field carries the claim.
func sceneNestedLengthWire(claim uint64) []byte {
	inner := []byte{2, 12}
	inner = append(inner, leb(claim)...)
	inner = append(inner, 0)
	body := []byte{1, 13}
	body = append(body, leb(uint64(len(inner)))...)
	body = append(body, inner...)
	body = append(body, 0)
	return sceneWire([]string{"ground", "name"}, body)
}

// sceneUnit generates the pointers corpus under its graphdemo manifest unit.
func sceneUnit(t *testing.T) *generatedUnit {
	t.Helper()
	units, _ := readManifest(t)
	if _, ok := units["graphdemo"]; !ok {
		t.Fatal("the tables manifest carries no graphdemo unit")
	}
	g := generateUnit(t, units, "graphdemo")
	if g.err != nil {
		t.Fatalf("graphdemo does not generate: %v", g.err)
	}
	return g
}

// runSceneWireAt runs the one read under the named interpreter and returns
// its verdict line; a raise is returned as the error.
func runSceneWireAt(t *testing.T, g *generatedUnit, interpreter string, wire []byte) (string, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.moo"), []byte(loadProbe(g, "Scene")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "inputs.moo"), moorun.InputsModule(map[string][]byte{"wire": wire}), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(interpreter, "main.moo")
	cmd.Dir = g.dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		return strings.TrimSpace(stdout.String()), fmt.Errorf("%s: %w\nstderr:\n%s", filepath.Base(interpreter), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// checkedMoo resolves the checked interpreter named by MOO_CHECKED.
func checkedMoo(t *testing.T) string {
	t.Helper()
	p := os.Getenv("MOO_CHECKED")
	if p == "" {
		t.Fatal("MOO_CHECKED is not set: this gate needs a checked moo build")
	}
	resolved, err := exec.LookPath(p)
	if err != nil {
		t.Fatalf("MOO_CHECKED=%s does not resolve: %v", p, err)
	}
	return resolved
}

// TestMooLengthsStayInRangeUnderACheckedBuild holds every length claim at the
// seventh-byte edge to the same refusal in both interpreters: the release
// build and the checked build, with no raise.
func TestMooLengthsStayInRangeUnderACheckedBuild(t *testing.T) {
	slowtest.Gate(t, "moo and a checked moo (it runs the emitted load and values modules)")
	t.Parallel()
	g := sceneUnit(t)
	release, why := moorun.Interpreter()
	if release == "" {
		t.Fatalf("the release moo interpreter does not resolve: %s", why)
	}
	checked := checkedMoo(t)
	cases := sceneLengthCases()
	for _, c := range cases {
		want, err := runSceneWireAt(t, g, release, c.wire)
		if err != nil {
			t.Fatalf("%s: the release interpreter raised: %v", c.name, err)
		}
		if want != "refused" {
			t.Fatalf("%s: the release interpreter answered %q, want refused", c.name, want)
		}
		got, err := runSceneWireAt(t, g, checked, c.wire)
		if err != nil {
			t.Fatalf("%s: the checked interpreter raised: %v", c.name, err)
		}
		if got != want {
			t.Fatalf("%s: the checked interpreter answered %q, the release %q", c.name, got, want)
		}
	}
	if len(cases) == 0 {
		t.Fatal("no length cases: the gate proves nothing")
	}
}

// TestMooLengthsStayInRangeNegativeControl plants the multiply-before-test
// back into an emitted table_values.moo and requires the checked interpreter
// to raise.
func TestMooLengthsStayInRangeNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo and a checked moo (it runs the emitted load and values modules)")
	t.Parallel()
	g := sceneUnit(t)
	checked := checkedMoo(t)
	path := filepath.Join(g.dir, "table_values.moo")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const fixed = `			elseif low > (V.HUGE - 1 - v) // mul then
				huge = true
			else
				v += low * mul
			end`
	const planted = `			else
				local add = low * mul
				if v + add > V.HUGE - 1 then
					huge = true
				else
					v += add
				end
			end`
	if strings.Count(string(src), fixed) != 1 {
		t.Fatalf("the room check is not in the emitted table_values.moo exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), fixed, planted, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	red := false
	for _, c := range sceneLengthCases() {
		if _, err := runSceneWireAt(t, g, checked, c.wire); err != nil {
			red = true
		}
	}
	if !red {
		t.Fatal("the checked interpreter did not raise on a planted multiply-before-test")
	}
}
