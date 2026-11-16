// Tagged unions of type payloads and named payload-free arms (SPEC §4.8),
// held two ways under both pinned interpreters and mypy --strict.
//
// The leg's half of test/packet-void reads and writes its SPEC-calculated
// wire byte for byte: a void arm spends exactly its tag bits.
//
// A small fixture schema of this test's own (unionOracleSchema) uses the union
// kind alone and beside the core's kinds (bool, enum, flags, integers, a type,
// a union inside a payload, a union inside a union's payload). The generated
// Go packet code is the reference: it writes every case, reads every hostile
// input, and prints its bytes and verdicts. Python must write the same bytes,
// read them back to an equal value that writes back byte-identical, and agree
// with every Go read verdict, including the unused tags Go refuses. A read
// builds every unselected arm at its declared defaults (SPEC §5), every
// out-of-contract write raises InvalidValue, and two negative controls plant a
// fault in a copy of the generated code and must go red by name.
package python

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// packetVoidPythonFixture returns the Python half of test/packet-void: a
// schema whose unions use only the kinds the packet core carries, the probe
// that reads and writes it, and the SPEC-calculated wire it must match.
func packetVoidPythonFixture(t *testing.T) (schema, probe, goldens string) {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "test", "packet-void")
	return filepath.Join(dir, "python", "Python.schema"),
		filepath.Join(dir, "python", "main.py"),
		filepath.Join(dir, "python", "goldens.txt")
}

func loadPacketVoidPython(t *testing.T) (files map[string][]byte, pkg string, probe, goldens []byte) {
	t.Helper()
	schema, probePath, goldensPath := packetVoidPythonFixture(t)
	u := loadPythonUnit(t, schema)
	generated, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the packet-void fixture: %v", err)
	}
	probe, err = os.ReadFile(probePath)
	if err != nil {
		t.Fatal(err)
	}
	goldens, err = os.ReadFile(goldensPath)
	if err != nil {
		t.Fatal(err)
	}
	if blocker := rootBlocker(u, "Mixed"); blocker != "" {
		t.Fatalf("the union root is on the golden harness's skip list, blocked by %s", blocker)
	}
	return generated, u.Package, probe, goldens
}

// unionOracleSchema is the union kind alone (Action: payload arms between
// payload-free ones, a 3-bit tag with 6 and 7 unused) and beside the core's
// kinds: an enum, flags, full-width integers and a bool in the payloads, a
// union inside a type (Nest), a union whose payload holds a union (Outer, a
// 2-bit tag with 3 unused), and a type that carries both between other fields
// (Envelope). Every payload field that has a declared default declares one
// other than zero, so a payload built at zero instead of its defaults shows.
const unionOracleSchema = `package pyunion

enum Mode { Idle, Walk, Run }

flags Perm { Read, Write, Exec }

type Move
{
    mode  Mode  = Walk
    speed int32 = -7
}

type Grant
{
    perm  Perm
    owner uint16 = 300
    live  bool   = true
}

union Action
{
    stop
    walk  Move
    grant Grant
    halt
    pause
}

type Nest
{
    inner Action
    tail  int8 = -2
}

union Outer
{
    nest Nest
    idle
}

type Envelope
{
    head  uint8 = 9
    mode  Mode
    act   Action
    flag  bool
    outer Outer
    perm  Perm
}
`

// unionCase is one value the Go reference writes and Python must match: the
// same value built in each language.
type unionCase struct {
	name, root, goValue, pyValue string
}

var unionCases = []unionCase{
	{"action-none", "Action", `Action{}`, `Action()`},
	{"action-stop", "Action", `Action{Type: ActionTypeStop}`, `mk(Action(), type=ActionType.Stop)`},
	{"action-walk", "Action", `Action{Type: ActionTypeWalk, Walk: Move{Mode: ModeRun, Speed: -123456}}`,
		`mk(Action(), type=ActionType.Walk, walk=mk(Move(), mode=Mode.Run, speed=-123456))`},
	{"action-walk-defaults", "Action", `Action{Type: ActionTypeWalk, Walk: NewMove()}`,
		`mk(Action(), type=ActionType.Walk)`},
	{"action-walk-extremes", "Action", `Action{Type: ActionTypeWalk, Walk: Move{Mode: ModeNone, Speed: -2147483648}}`,
		`mk(Action(), type=ActionType.Walk, walk=mk(Move(), mode=Mode.None_, speed=-2147483648))`},
	{"action-grant", "Action", `Action{Type: ActionTypeGrant, Grant: Grant{Perm: PermRead | PermExec, Owner: 65535, Live: false}}`,
		`mk(Action(), type=ActionType.Grant, grant=mk(Grant(), perm=Perm.Read | Perm.Exec, owner=65535, live=False))`},
	{"action-halt", "Action", `Action{Type: ActionTypeHalt}`, `mk(Action(), type=ActionType.Halt)`},
	{"action-pause", "Action", `Action{Type: ActionTypePause}`, `mk(Action(), type=ActionType.Pause)`},
	{"nest-pause", "Nest", `Nest{Inner: Action{Type: ActionTypePause}, Tail: 0}`,
		`mk(Nest(), inner=mk(Action(), type=ActionType.Pause), tail=0)`},
	{"nest-grant", "Nest", `Nest{Inner: Action{Type: ActionTypeGrant, Grant: Grant{Perm: PermWrite, Owner: 1, Live: true}}, Tail: -128}`,
		`mk(Nest(), inner=mk(Action(), type=ActionType.Grant, grant=mk(Grant(), perm=Perm.Write, owner=1, live=True)), tail=-128)`},
	{"outer-none", "Outer", `Outer{}`, `Outer()`},
	{"outer-idle", "Outer", `Outer{Type: OuterTypeIdle}`, `mk(Outer(), type=OuterType.Idle)`},
	{"outer-nest-none", "Outer", `Outer{Type: OuterTypeNest, Nest: Nest{Tail: 127}}`,
		`mk(Outer(), type=OuterType.Nest, nest=mk(Nest(), tail=127))`},
	{"outer-nest-walk", "Outer", `Outer{Type: OuterTypeNest, Nest: Nest{Inner: Action{Type: ActionTypeWalk, Walk: Move{Mode: ModeIdle, Speed: 5}}, Tail: -1}}`,
		`mk(Outer(), type=OuterType.Nest, nest=mk(Nest(), inner=mk(Action(), type=ActionType.Walk, walk=mk(Move(), mode=Mode.Idle, speed=5)), tail=-1))`},
	{"envelope-defaults", "Envelope", `NewEnvelope()`, `Envelope()`},
	{"envelope-full", "Envelope",
		`Envelope{Head: 255, Mode: ModeIdle, Act: Action{Type: ActionTypeWalk, Walk: Move{Mode: ModeIdle, Speed: 2147483647}}, Flag: true, ` +
			`Outer: Outer{Type: OuterTypeNest, Nest: Nest{Inner: Action{Type: ActionTypeHalt}, Tail: -1}}, Perm: PermRead | PermWrite | PermExec}`,
		`mk(Envelope(), head=255, mode=Mode.Idle, act=mk(Action(), type=ActionType.Walk, walk=mk(Move(), mode=Mode.Idle, speed=2147483647)), flag=True, ` +
			`outer=mk(Outer(), type=OuterType.Nest, nest=mk(Nest(), inner=mk(Action(), type=ActionType.Halt), tail=-1)), perm=Perm.Read | Perm.Write | Perm.Exec)`},
	{"envelope-grant-idle", "Envelope",
		`Envelope{Head: 0, Mode: ModeRun, Act: Action{Type: ActionTypeGrant, Grant: NewGrant()}, Flag: false, Outer: Outer{Type: OuterTypeIdle}, Perm: PermWrite}`,
		`mk(Envelope(), head=0, mode=Mode.Run, act=mk(Action(), type=ActionType.Grant), flag=False, outer=mk(Outer(), type=OuterType.Idle), perm=Perm.Write)`},
}

// unionHostile is a read input whose verdict comes from the Go reference
// alone. Each case's bytes one short and one zero byte long are added beside
// these. mustRefuse marks the inputs the Go reader is required to refuse, so
// the comparison is never vacuous on the unused tags.
type unionRead struct {
	name, root, hex string
	mustRefuse      bool
}

var unionHostile = []unionRead{
	{"action-tag-6", "Action", "06", true},
	{"action-tag-7", "Action", "07", true},
	{"action-empty", "Action", "", true},
	{"action-trailing-bit", "Action", "09", true},
	{"outer-tag-3", "Outer", "03", true},
	{"outer-empty", "Outer", "", true},
	{"nest-inner-tag-6", "Nest", "0600", true},
	{"outer-nest-inner-tag-7", "Outer", "1d00", true},
	{"envelope-empty", "Envelope", "", true},
}

// unionGoOracle is the test file the generated Go package runs: it writes
// every case, then reads every input, and prints one line for each.
func unionGoOracle(reads []unionRead) string {
	var b strings.Builder
	b.WriteString(`package pyunion

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func padded(h string) []byte {
	data, err := hex.DecodeString(h)
	if err != nil {
		panic(err)
	}
	out := make([]byte, len(data), len(data)+16)
	copy(out, data)
	return out
}

func verdict(err error) string {
	if err != nil {
		return "refused"
	}
	return "ok"
}

func TestUnionOracle(t *testing.T) {
	fmt.Printf("TAG|ActionType|%d|%d|%d|%d|%d|%d\n", ActionTypeStop, ActionTypeWalk, ActionTypeGrant, ActionTypeHalt, ActionTypePause, ActionTypeMax)
	fmt.Printf("TAG|OuterType|%d|%d|%d\n", OuterTypeNest, OuterTypeIdle, OuterTypeMax)
`)
	for _, c := range unionCases {
		fmt.Fprintf(&b, `	{
		value := %s
		ws := serialize.NewWriteStream(make([]byte, 64))
		if err := Write%s(ws, &value); err != nil {
			t.Fatalf("write %s: %%v", err)
		}
		ws.Flush()
		data := ws.Data()
		fmt.Printf("CASE|%s|%s|%%x\n", data)
		if len(data) > 0 {
			short := hex.EncodeToString(data[:len(data)-1])
			var v %s
			err := Read%s(serialize.NewReadStream(padded(short)), &v)
			fmt.Printf("READ|%s-short|%s|%%s|%%s\n", short, verdict(err))
		}
		long := hex.EncodeToString(append(append([]byte(nil), data...), 0))
		var v %s
		err := Read%s(serialize.NewReadStream(padded(long)), &v)
		fmt.Printf("READ|%s-long|%s|%%s|%%s\n", long, verdict(err))
	}
`, c.goValue, c.root, c.name, c.name, c.root, c.root, c.root, c.name, c.root, c.root, c.root, c.name, c.root)
	}
	b.WriteString("}\n\nfunc TestUnionOracleReads(t *testing.T) {\n\tfor _, in := range []struct{ name, root, hex string }{\n")
	for _, h := range reads {
		fmt.Fprintf(&b, "\t\t{%q, %q, %q},\n", h.name, h.root, h.hex)
	}
	b.WriteString("\t} {\n\t\tdata := padded(in.hex)\n\t\tvar err error\n\t\tswitch in.root {\n")
	for _, root := range []string{"Action", "Nest", "Outer", "Envelope"} {
		fmt.Fprintf(&b, "\t\tcase %q:\n\t\t\tvar v %s\n\t\t\terr = Read%s(serialize.NewReadStream(data), &v)\n", root, root, root)
	}
	b.WriteString("\t\tdefault:\n\t\t\tt.Fatalf(\"unknown root %s\", in.root)\n\t\t}\n")
	b.WriteString("\t\tfmt.Printf(\"READ|%s|%s|%s|%s\\n\", in.name, in.root, in.hex, verdict(err))\n\t}\n}\n")
	return b.String()
}

// unionOracle is the generated Go reference's answer, computed once for the
// whole package run.
var unionOracle struct {
	once  sync.Once
	lines string
	err   error
}

// goUnionOracle generates the fixture's Go packet code, runs the oracle test
// beside it, and returns its CASE, TAG and READ lines with each case's short
// and long reads added (their verdicts read by Go as well).
func goUnionOracle(t *testing.T) string {
	t.Helper()
	slowtest.Gate(t, "the go toolchain over the generated Go reference")
	root := repoRoot(t)
	runtime := slowtest.SerializeGo(t, root)
	unionOracle.once.Do(func() {
		unionOracle.lines, unionOracle.err = runUnionOracle(t, runtime)
	})
	if unionOracle.err != nil {
		t.Fatal(unionOracle.err)
	}
	return unionOracle.lines
}

func runUnionOracle(t *testing.T, runtime string) (string, error) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Union.schema"), []byte(unionOracleSchema), 0o644); err != nil {
		return "", err
	}
	u := loadPythonUnit(t, filepath.Join(src, "Union.schema"))
	files, err := golang.Generate(u)
	if err != nil {
		return "", fmt.Errorf("generate the Go reference: %w", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return "", err
		}
	}
	mod := fmt.Sprintf("module pyunion\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "oracle_test.go"), []byte(unionGoOracle(unionHostile)), 0o644); err != nil {
		return "", err
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "300s", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("the generated Go reference: %w\n%s", err, out)
	}
	var keep []string
	verdicts := map[string]string{}
	cases := 0
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		f := strings.Split(line, "|")
		switch f[0] {
		case "TAG":
		case "CASE":
			if len(f) != 4 {
				return "", fmt.Errorf("the Go reference's line %q is not CASE|name|root|hex", line)
			}
			if _, err := hex.DecodeString(f[3]); err != nil {
				return "", fmt.Errorf("the Go reference's %s line carries no hex: %w", f[1], err)
			}
			cases++
		case "READ":
			if len(f) != 5 {
				return "", fmt.Errorf("the Go reference's line %q is not READ|name|root|hex|verdict", line)
			}
			verdicts[f[1]] = f[4]
		default:
			continue
		}
		keep = append(keep, line)
	}
	if cases != len(unionCases) {
		return "", fmt.Errorf("the Go reference wrote %d cases, want %d:\n%s", cases, len(unionCases), out)
	}
	// the reads that must be refused, so the comparison is never vacuous: the
	// listed hostile inputs, and every case one byte short or one byte long
	for _, h := range unionHostile {
		if got := verdicts[h.name]; h.mustRefuse && got != "refused" {
			return "", fmt.Errorf("the Go reference gave %q on %s (%s); the comparison would prove nothing", got, h.name, h.hex)
		}
	}
	for _, c := range unionCases {
		for _, suffix := range []string{"-short", "-long"} {
			if got, ok := verdicts[c.name+suffix]; ok && got != "refused" {
				return "", fmt.Errorf("the Go reference accepted %s%s", c.name, suffix)
			}
		}
		if _, ok := verdicts[c.name+"-long"]; !ok {
			return "", fmt.Errorf("the Go reference gave no verdict on %s-long", c.name)
		}
	}
	return strings.Join(keep, "\n") + "\n", nil
}

// unionOracleProbe is the Python side: it rebuilds each case, compares its
// write with the Go reference's bytes, reads those bytes back, and gives
// every Go read verdict its own.
func unionOracleProbe() string {
	var builders strings.Builder
	for _, c := range unionCases {
		fmt.Fprintf(&builders, "    %q: lambda: %s,\n", c.name, c.pyValue)
	}
	return pyrun.ImportPrelude + `
from typing import Any, Callable

from pyunion import (
    Action,
    ActionType,
    Envelope,
    Grant,
    InvalidValue,
    Mode,
    Move,
    Nest,
    Outer,
    OuterType,
    Perm,
    Refused,
)


def fail(message: str) -> None:
    print("FAILED: " + message)
    raise SystemExit(1)


def mk(value: Any, **fields: Any) -> Any:
    for key, field in fields.items():
        setattr(value, key, field)
    return value


BUILD: "dict[str, Callable[[], Any]]" = {
` + builders.String() + `}

ROOTS: "dict[str, Any]" = {"Action": Action, "Nest": Nest, "Outer": Outer, "Envelope": Envelope}


def inactive(value: Any, where: str) -> None:
    """Every unselected payload arm a read leaves is at its declared defaults."""
    if isinstance(value, Action):
        if value.type != ActionType.Walk and value.walk != Move():
            fail(where + ": unselected walk is not at its defaults: " + repr(value.walk))
        if value.type != ActionType.Grant and value.grant != Grant():
            fail(where + ": unselected grant is not at its defaults: " + repr(value.grant))
        if value.type == ActionType.Walk:
            inactive(value.walk, where + ".walk")
    elif isinstance(value, Outer):
        if value.type != OuterType.Nest and value.nest != Nest():
            fail(where + ": unselected nest is not at its defaults: " + repr(value.nest))
        inactive(value.nest, where + ".nest")
    elif isinstance(value, Nest):
        inactive(value.inner, where + ".inner")
    elif isinstance(value, Envelope):
        inactive(value.act, where + ".act")
        inactive(value.outer, where + ".outer")


def expect_invalid(label: str, value: Any, path: str) -> None:
    try:
        value.write()
    except InvalidValue as exc:
        if path not in str(exc):
            fail("write " + label + " raised InvalidValue without " + path + ": " + str(exc))
        return
    fail("write " + label + " was accepted")


def main() -> None:
    # declared defaults, born in every arm at construction (SPEC §5)
    fresh = Action()
    if fresh.type != ActionType.None_ or fresh.walk.mode != Mode.Walk or fresh.walk.speed != -7:
        fail("Action() walk arm is not at its declared defaults")
    if fresh.grant.perm != 0 or fresh.grant.owner != 300 or fresh.grant.live is not True:
        fail("Action() grant arm is not at its declared defaults")
    if Outer().nest.tail != -2 or Outer().nest.inner != Action() or Envelope().head != 9:
        fail("nested union defaults")
    if Action.__slots__ != ("type", "walk", "grant") or Outer.__slots__ != ("type", "nest"):
        fail("union slots")
    # equality is by the selected arm only, as the wire is
    a = mk(Action(), type=ActionType.Halt)
    b = mk(Action(), type=ActionType.Halt)
    b.walk.speed = 99
    if a != b:
        fail("an unselected arm took part in equality")
    b.type = ActionType.Walk
    if a == b:
        fail("unions with different tags compare equal")

    seen = 0
    with open("oracle.txt", encoding="utf-8") as stream:
        for line in stream:
            f = line.strip().split("|")
            if f[0] == "TAG":
                if f[1] == "ActionType":
                    want = [ActionType.Stop, ActionType.Walk, ActionType.Grant, ActionType.Halt, ActionType.Pause, ActionType.Max]
                else:
                    want = [OuterType.Nest, OuterType.Idle, OuterType.Max]
                if [int(x) for x in f[2:]] != want:
                    fail("tag numbers of " + f[1] + " differ from the Go reference: " + line.strip())
            elif f[0] == "CASE":
                name, root, wire = f[1], f[2], bytes.fromhex(f[3])
                value = BUILD[name]()
                if type(value).__name__ != root:
                    fail("case " + name + " builds " + type(value).__name__)
                got = value.write()
                if got != wire:
                    fail("write " + name + ": got " + got.hex() + " want " + wire.hex())
                try:
                    back = ROOTS[root].read(wire)
                except Refused as exc:
                    fail("read " + name + " was refused: " + exc.reason)
                if back != value:
                    fail("read " + name + " value differs: " + repr(back))
                if back.write() != wire:
                    fail("read " + name + " write-back differs")
                inactive(back, name)
                seen += 1
            elif f[0] == "READ":
                name, root, data, want = f[1], f[2], bytes.fromhex(f[3]), f[4]
                try:
                    ROOTS[root].read(data)
                    verdict = "ok"
                except Refused:
                    verdict = "refused"
                if verdict != want:
                    fail("read " + name + " (" + f[3] + "): got " + verdict + ", the Go reference says " + want)
    if seen != len(BUILD):
        fail("compared " + str(seen) + " cases, want " + str(len(BUILD)))

    # out-of-contract writes: Python refuses every one in every build
    for label, tag in (("tag-6", 6), ("tag-negative", -1), ("tag-bool", True), ("tag-str", "1"), ("tag-float", 1.0)):
        expect_invalid("action-" + label, mk(Action(), type=tag), "Action.type")
    expect_invalid("outer-tag-3", mk(Outer(), type=3), "Outer.type")
    expect_invalid("action-walk-type", mk(Action(), type=ActionType.Walk, walk=Grant()), "Action.walk")
    expect_invalid("action-walk-none", mk(Action(), type=ActionType.Walk, walk=None), "Action.walk")
    expect_invalid("action-walk-mode", mk(Action(), type=ActionType.Walk, walk=mk(Move(), mode=4)), "Action.walk.mode")
    expect_invalid("action-walk-speed", mk(Action(), type=ActionType.Walk, walk=mk(Move(), speed=2147483648)), "Action.walk.speed")
    expect_invalid("action-grant-perm", mk(Action(), type=ActionType.Grant, grant=mk(Grant(), perm=8)), "Action.grant.perm")
    expect_invalid("action-grant-live", mk(Action(), type=ActionType.Grant, grant=mk(Grant(), live=1)), "Action.grant.live")
    expect_invalid("nest-inner-tag", mk(Nest(), inner=mk(Action(), type=7)), "Nest.inner.type")
    expect_invalid("nest-inner-type", mk(Nest(), inner=Outer()), "Nest.inner")
    expect_invalid("envelope-outer-nest-inner", mk(Envelope(), outer=mk(Outer(), type=OuterType.Nest, nest=mk(Nest(), inner=mk(Action(), type=ActionType.Grant, grant=mk(Grant(), owner=65536))))), "Envelope.outer.nest.inner.grant.owner")
    # an unselected arm is neither validated nor written, as in the Go reference
    idle = mk(Action(), type=ActionType.Stop, walk=None)
    if idle.write() != mk(Action(), type=ActionType.Stop).write():
        fail("an unselected arm changed the wire")
    print("ok")


main()
`
}

// unionOracleTree generates the fixture's Python package beside the probe.
func unionOracleTree(t *testing.T) (files map[string][]byte, pkg string) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "Union.schema"), []byte(unionOracleSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadPythonUnit(t, filepath.Join(src, "Union.schema"))
	for _, root := range []string{"Action", "Nest", "Outer", "Envelope"} {
		if blocker := rootBlocker(u, root); blocker != "" {
			t.Fatalf("%s is blocked by %s; the fixture must use only the union kind and the core's", root, blocker)
		}
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the union fixture: %v", err)
	}
	return files, u.Package
}

// TestPythonPacketUnions holds the union kind to test/packet-void's wire and
// to the generated Go reference, under both pinned interpreters, with the
// analyzer over each generated tree.
func TestPythonPacketUnions(t *testing.T) {
	t.Parallel()
	interpreters := []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}}
	t.Run("packet-void", func(t *testing.T) {
		t.Parallel()
		files, pkg, probe, goldens := loadPacketVoidPython(t)
		for _, run := range interpreters {
			t.Run(run.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, files)
				got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{
					"main.py":     probe,
					"goldens.txt": goldens,
				})))
				if got != "ok" {
					t.Fatalf("packet-void unions under %s:\n%s", run.name, got)
				}
				out, err := pyrun.Analyze(t, dir, pkg)
				if err != nil || !strings.Contains(out, "Success: no issues found") {
					t.Fatalf("packet-void unions under %s: mypy --strict: %v\n%s", run.name, err, out)
				}
			})
		}
	})
	t.Run("go-reference", func(t *testing.T) {
		t.Parallel()
		files, pkg := unionOracleTree(t)
		oracle := goUnionOracle(t)
		for _, run := range interpreters {
			t.Run(run.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				writeTree(t, dir, files)
				got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{
					"main.py":    []byte(unionOracleProbe()),
					"oracle.txt": []byte(oracle),
				})))
				if got != "ok" {
					t.Fatalf("unions against the Go reference under %s:\n%s", run.name, got)
				}
				out, err := pyrun.Analyze(t, dir, pkg)
				if err != nil || !strings.Contains(out, "Success: no issues found") {
					t.Fatalf("unions against the Go reference under %s: mypy --strict: %v\n%s", run.name, err, out)
				}
			})
		}
	})
}

// TestPythonPacketUnionsNegativeControl plants a fault in a copy of the
// generated code and requires each probe to go red by name: one extra set bit
// on the Mixed union's payload-free arms on the write path alone (the
// packet-void probe), and an Action reader whose tag range admits the unused
// tags 6 and 7 (the Go-reference probe).
func TestPythonPacketUnionsNegativeControl(t *testing.T) {
	t.Parallel()
	t.Run("void-arm-bit", func(t *testing.T) {
		t.Parallel()
		files, pkg, probe, goldens := loadPacketVoidPython(t)
		module := pkg + "/Python.py"
		text := string(files[module])
		const anchor = "        if self.type == 2:\n            self.payload._s_write(stream)\n"
		if strings.Count(text, anchor) != 1 {
			t.Fatalf("the Mixed writer anchor matched %d times:\n%s", strings.Count(text, anchor), text)
		}
		const planted = "        if self.type == 1 or self.type == 3:\n" +
			"            _s_ri.value = 1\n" +
			"            _s_serialize_int(_s_ri, 0, 1)\n"
		files[module] = []byte(strings.Replace(text, anchor, planted+anchor, 1))
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
			"main.py":     probe,
			"goldens.txt": goldens,
		})
		if err == nil || !strings.Contains(out, "FAILED: write mixed-ping") {
			t.Fatalf("the planted void-arm bit did not go red on the write marker (err %v):\n%s", err, out)
		}
	})
	t.Run("unused-tag-read", func(t *testing.T) {
		t.Parallel()
		files, pkg := unionOracleTree(t)
		module := pkg + "/Union.py"
		text := string(files[module])
		const anchor = "        if not _s_serialize_int(_s_ri, 0, 5):\n"
		if strings.Count(text, anchor) != 1 {
			t.Fatalf("the Action reader anchor matched %d times:\n%s", strings.Count(text, anchor), text)
		}
		files[module] = []byte(strings.Replace(text, anchor, "        if not _s_serialize_int(_s_ri, 0, 7):\n", 1))
		oracle := goUnionOracle(t)
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
			"main.py":    []byte(unionOracleProbe()),
			"oracle.txt": []byte(oracle),
		})
		if err == nil || !strings.Contains(out, "FAILED: read action-tag-6") {
			t.Fatalf("the widened tag range did not go red on the unused-tag read (err %v):\n%s", err, out)
		}
	})
}
