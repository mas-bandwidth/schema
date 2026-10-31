package squirreltable_test

// THE WIRE-FUZZ DIFFERENTIAL (docs/SPEC-TABLES.md §4.2; docs/PORT-SQUIRREL.md,
// THE CARD MAP, schema-squirrel-fuzz-differential). A table read is untrusted
// input, so the read IS the verifier: a splitmix64 generator mutates every
// instance wire of the tables conformance corpus whose unit this target
// generates, the Squirrel leg's tolerant load reads each mutant and saves what
// it decoded, and the compiler's own engine (internal/tablewire, written from
// the spec and from neither backend) reads the same bytes. The two must agree
// on the builder's completion, the six counters (unknown, kind_mismatch,
// widened, clamped, duplicate, malformed), the refusal verdict and the re-saved
// bytes, on every mutant.
//
// The leg is the SAME driver `make tables-squirrel-wire-fuzz` hands the
// conformance harness (test/conformance/squirrel/wirefuzz.nut), on the same
// pipe protocol (test/conformance/README.md, "The wire-fuzz driver"), pointed
// at units generated here into a t.TempDir() through
// $SCHEMA_SQUIRREL_CONFORMANCE. One driver, two judges: the harness's
// enumerated passes there, this generator's random edits here.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fuzzMutantsPerInstance is how many mutants each corpus instance wire yields.
// The nightly target makes it an argument (make/schema-squirrel-nightly-fuzz.mk,
// SCHEMA_SQUIRREL_FUZZ_MUTANTS); the default is the pull request's count.
var fuzzMutantsPerInstance = envInt("SCHEMA_SQUIRREL_FUZZ_MUTANTS", 200)

// fuzzSeed is the run's seed; mutant j of instance i is a pure function of
// (fuzzSeed, i, j), so a red names everything that reproduces it. The nightly
// target makes it an argument too, so a night's search is replayable by one
// command.
var fuzzSeed = envSeed("SCHEMA_SQUIRREL_FUZZ_SEED", 0x5351554952524c46) // "SQUIRRLF"

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func envSeed(name string, def uint64) uint64 {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.ParseUint(strings.TrimSpace(v), 0, 64); err == nil {
			return n
		}
	}
	return def
}

// splitmix64 is the differential's random source (as internal/codegen/gotable/
// textdifferential_test.go).
type splitmix64 struct{ state uint64 }

func (r *splitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// fuzzMutant applies one to four edits to a copy of base: a random byte, a
// flipped bit, a boundary byte, a truncation, an inserted byte, a deleted
// byte, or a LEB128 continuation bit forced on. Every rule of §3 the load
// checks is a byte one of these can break: the form byte, the trailer's count
// and ids, a reference, a kind, a frame length, a count, a payload.
func fuzzMutant(base []byte, r *splitmix64) []byte {
	w := append([]byte(nil), base...)
	edits := 1 + int(r.next()%4)
	for range edits {
		op := r.next() % 7
		if len(w) == 0 {
			op = 4
		}
		pos := 0
		if len(w) > 0 {
			pos = int(r.next() % uint64(len(w)))
		}
		switch op {
		case 0:
			w[pos] = byte(r.next())
		case 1:
			w[pos] ^= byte(1) << (r.next() % 8)
		case 2:
			w[pos] = [...]byte{0x00, 0x01, 0x7f, 0x80, 0xff}[r.next()%5]
		case 3:
			w = w[:r.next()%uint64(len(w)+1)]
		case 4:
			at := int(r.next() % uint64(len(w)+1))
			w = append(w[:at], append([]byte{byte(r.next())}, w[at:]...)...)
		case 5:
			w = append(w[:pos], w[pos+1:]...)
		case 6:
			w[pos] |= 0x80
		}
	}
	return w
}

// fuzzAnswer is one side's answer for one mutant: the leg's reply or the
// engine's, in the same shape.
type fuzzAnswer struct {
	loaded                                             bool
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed, refused                                 bool
	saved                                              []byte
	saveFail                                           bool
	// nonFinite is the engine's refusal of a NaN or an infinity
	// (docs/SPEC-TABLES.md §3.3): every read refuses it, so the whole of
	// what the leg owes is its own refusal verdict
	nonFinite bool
}

func (a fuzzAnswer) String() string {
	save := "refused"
	if !a.saveFail {
		save = fmt.Sprintf("%d bytes", len(a.saved))
	}
	return fmt.Sprintf("loaded=%t report=%d,%d,%d,%d,%d,%t refused=%t saved=%s",
		a.loaded, a.unknown, a.kindMismatch, a.widened, a.clamped, a.duplicate, a.malformed, a.refused, save)
}

// fuzzVerdict names the first disagreement between the leg and the engine, or
// "" when they agree (test/conformance/harness/wirefuzz.go, wireVerdict, for
// the builder arm and the fixed-class roots).
func fuzzVerdict(leg, eng fuzzAnswer) string {
	if eng.nonFinite {
		if !leg.refused {
			return "the engine refuses a non-finite float (non_finite_float); the leg did not refuse the wire"
		}
		return ""
	}
	switch {
	case leg.loaded != eng.loaded:
		return "the builder's completion differs"
	case leg.unknown != eng.unknown || leg.kindMismatch != eng.kindMismatch ||
		leg.widened != eng.widened || leg.clamped != eng.clamped ||
		leg.duplicate != eng.duplicate || leg.malformed != eng.malformed:
		return "the report differs"
	case leg.refused != eng.refused:
		return "the refusal verdict differs"
	case leg.saveFail != eng.saveFail:
		return "one side saved and the other refused to"
	case !bytes.Equal(leg.saved, eng.saved):
		at := 0
		for at < len(leg.saved) && at < len(eng.saved) && leg.saved[at] == eng.saved[at] {
			at++
		}
		return fmt.Sprintf("the decoded value differs, first at byte %d", at)
	}
	return ""
}

// fuzzEngine is the Go engine's answer (docs/SPEC-TABLES.md §4.2), the
// harness's oracle for the arm the root's class puts it in:
//
//   - a VARIABLE-class root is read as a BUILDER (tablewire.Decode): `loaded`
//     is whether the read finished, a stopped builder saves nothing, and a
//     count past the storage cap stops it without a malformed event;
//   - a FIXED-class root always loads (tablewire.DecodeRegion: the value is
//     filled in place and reports), and what it saves is whatever it holds,
//     the declared defaults where nothing was decoded.
func fuzzEngine(m *tabletext.Model, def *ir.Struct, variable bool, data []byte) (ans fuzzAnswer, err error) {
	inst := m.New(def)
	var rep tabletext.Report
	var ok bool
	var derr error
	if variable {
		ok, derr = tablewire.Decode(m, inst, data, &rep)
		if _, capped := errors.AsType[*tablewire.CountRefusal](derr); capped {
			derr = nil
		}
	} else {
		ok, derr = tablewire.DecodeRegion(m, inst, data, &rep)
	}
	if _, refused := errors.AsType[*tablewire.NonFiniteRefusal](derr); refused {
		ans.nonFinite = true
		return ans, nil
	}
	switch {
	case tablewire.Refused(derr):
		ans.refused = true
		if variable {
			ans.saveFail = true
			return ans, nil
		}
		ans.loaded = true
	case derr != nil:
		return ans, fmt.Errorf("the engine refused the root itself: %w", derr)
	default:
		ans.loaded = ok || !variable
		ans.unknown, ans.kindMismatch, ans.widened = rep.Unknown, rep.KindMismatch, rep.Widened
		ans.clamped, ans.duplicate = rep.Clamped, rep.Duplicate
		ans.malformed = rep.Malformed || (!ok && !variable)
		if !ans.loaded {
			ans.saveFail = true
			return ans, nil
		}
	}
	enc, eerr := tablewire.Encode(m, inst)
	if eerr != nil {
		ans.saveFail = true
	} else {
		ans.saved = enc
	}
	return ans, nil
}

// fuzzLeg is one wirefuzz.nut process on its pipe.
type fuzzLeg struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr bytes.Buffer
}

type fuzzRoot struct {
	unit, root string
}

// startFuzzLeg starts the driver over the corpus in dir, in the builder arm or
// the plain one, and sends it the roster; it answers which roots it carries.
func startFuzzLeg(t *testing.T, dir string, builder bool, roster []fuzzRoot) (*fuzzLeg, []bool) {
	t.Helper()
	sq := sqrun.Require(t)
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{filepath.Join(repo, "test", "conformance", "squirrel", "wirefuzz.nut")}
	if builder {
		args = append(args, "builder")
	}
	ctx, cancel := slowtest.ProbeContext(t)
	t.Cleanup(cancel)
	leg := &fuzzLeg{cmd: exec.CommandContext(ctx, sq, args...)}
	leg.cmd.Dir = dir
	leg.cmd.Env = append(os.Environ(), "SCHEMA_SQUIRREL_CONFORMANCE="+dir)
	leg.cmd.Stderr = &leg.stderr
	if leg.stdin, err = leg.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	out, err := leg.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	leg.stdout = bufio.NewReader(out)
	if err := leg.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = leg.stdin.Close()
		_ = leg.cmd.Wait()
	})
	var head bytes.Buffer
	_ = binary.Write(&head, binary.LittleEndian, uint32(len(roster)))
	for _, r := range roster {
		_ = binary.Write(&head, binary.LittleEndian, uint16(len(r.unit)))
		head.WriteString(r.unit)
		_ = binary.Write(&head, binary.LittleEndian, uint16(len(r.root)))
		head.WriteString(r.root)
		head.WriteByte(byte(ir.TableWireForm))
		head.WriteByte(0)
	}
	if _, err := leg.stdin.Write(head.Bytes()); err != nil {
		t.Fatalf("the driver closed its input during the roster: %v\n%s", err, leg.stderr.String())
	}
	known := make([]bool, len(roster))
	for i := range roster {
		b, err := leg.stdout.ReadByte()
		if err != nil {
			t.Fatalf("the driver did not answer the roster: %v\n%s", err, leg.stderr.String())
		}
		known[i] = b != 0
		if known[i] && roster[i].unit == "scalars" {
			t.Logf("roster: scalars.%s answered 1 (builder arm %t)", roster[i].root, builder)
		}
	}
	return leg, known
}

// ask sends one mutant and reads the driver's reply.
func (l *fuzzLeg) ask(index int, data []byte) (fuzzAnswer, error) {
	var a fuzzAnswer
	var head [8]byte
	binary.LittleEndian.PutUint32(head[0:], uint32(index))
	binary.LittleEndian.PutUint32(head[4:], uint32(len(data)))
	if _, err := l.stdin.Write(append(head[:], data...)); err != nil {
		return a, err
	}
	var r [1 + 5*4 + 1 + 1 + 8 + 4 + 4 + 8]byte
	if _, err := io.ReadFull(l.stdout, r[:]); err != nil {
		return a, err
	}
	a.loaded = r[0] != 0
	a.unknown = int(int32(binary.LittleEndian.Uint32(r[1:])))
	a.kindMismatch = int(int32(binary.LittleEndian.Uint32(r[5:])))
	a.widened = int(int32(binary.LittleEndian.Uint32(r[9:])))
	a.clamped = int(int32(binary.LittleEndian.Uint32(r[13:])))
	a.duplicate = int(int32(binary.LittleEndian.Uint32(r[17:])))
	a.malformed = r[21] != 0
	a.refused = r[22] != 0
	if measure := int64(binary.LittleEndian.Uint64(r[23:])); measure != -1 {
		return a, fmt.Errorf("the driver answered a region measure of %d; this leg has no region", measure)
	}
	n := int64(binary.LittleEndian.Uint64(r[39:]))
	if n < 0 {
		a.saveFail = true
		return a, nil
	}
	a.saved = make([]byte, n)
	_, err := io.ReadFull(l.stdout, a.saved)
	return a, err
}

// fuzzSeedWire is one corpus instance, the basis of its mutants.
type fuzzSeedWire struct {
	name string
	root int
	wire []byte
}

// fuzzTarget builds the compiler (make/squirrel.mk's build/schema-squirrel)
// into dir and returns its path. The target's refusal chain is the compiler's, and the
// differential fuzzes exactly the units that chain lets through, as `make
// tables-squirrel-wire-fuzz` does over build/conformance-squirrel.
func fuzzTarget(t *testing.T, repo, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "schema-squirrel")
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/schema")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the tagged compiler: %v\n%s", err, out)
	}
	return bin
}

// runWireFuzzDifferential generates every manifest unit through the target,
// one directory per unit key exactly as build/conformance-squirrel lays them
// out (edit patches each emitted file first: the negative control), runs both
// arms of the driver over every instance wire's mutants, and compares each
// reply with the engine. It returns the mutant count and the first divergence,
// named by seed, unit, root and the mutant's hex.
func runWireFuzzDifferential(t *testing.T, edit func(name string, src []byte) []byte) (int, error) {
	t.Helper()
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	dir := t.TempDir()
	schema := fuzzTarget(t, repo, t.TempDir())
	models := map[string]*tabletext.Model{}
	var generated, refused []string
	for _, mu := range manifestUnits(t, manifest) {
		out := filepath.Join(dir, mu.key)
		ctx, cancel := slowtest.ProbeContext(t)
		gen := exec.CommandContext(ctx, schema, append([]string{"generate", "--lang", "squirrel", "--out", out}, mu.paths...)...)
		gen.Dir = repo
		msg, err := gen.CombinedOutput()
		cancel()
		if err != nil {
			// a unit the target REFUSES BY NAME is no seed (build/conformance-
			// squirrel/REFUSED.txt): every refusal is a `schema:` line naming
			// the target. Anything else — a crash, a deadline — is a red.
			if !strings.Contains(string(msg), "schema: ") || !strings.Contains(string(msg), "squirrel") || strings.Contains(string(msg), "panic") {
				t.Fatalf("generating unit %s failed without a refusal by name: %v\n%s", mu.key, err, msg)
			}
			_ = os.RemoveAll(out)
			refused = append(refused, mu.key)
			continue
		}
		if edit != nil {
			entries, err := os.ReadDir(out)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				path := filepath.Join(out, e.Name())
				src, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, path, edit(e.Name(), src))
			}
		}
		models[mu.key] = tabletext.NewModel(loadUnit(t, repo, mu.paths))
		generated = append(generated, mu.key)
	}
	t.Logf("%d units generated (%s), %d refused by the target", len(generated), strings.Join(generated, " "), len(refused))
	var roster []fuzzRoot
	rootIndex := map[fuzzRoot]int{}
	var seeds []fuzzSeedWire
	for _, in := range readInstanceRows(t, manifest) {
		if models[in.unit] == nil {
			continue
		}
		key := fuzzRoot{in.unit, in.root}
		i, ok := rootIndex[key]
		if !ok {
			i = len(roster)
			rootIndex[key] = i
			roster = append(roster, key)
		}
		data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		seeds = append(seeds, fuzzSeedWire{name: in.name, root: i, wire: data})
	}
	if len(seeds) == 0 {
		t.Fatal("no corpus instance names a unit the Squirrel target generates")
	}
	t.Logf("%d instance seeds over %d roots", len(seeds), len(roster))

	mutants := 0
	carried := make([]int, len(roster)) // how many arms carry each root
	for _, builder := range []bool{false, true} {
		leg, known := startFuzzLeg(t, dir, builder, roster)
		for i, k := range known {
			if k {
				carried[i]++
			}
		}
		for si, s := range seeds {
			if !known[s.root] {
				continue
			}
			r := roster[s.root]
			m := models[r.unit]
			def := m.Lookup(r.root)
			if def == nil {
				t.Fatalf("unit %s declares no table %s", r.unit, r.root)
			}
			variable := m.IsVariable(r.root)
			if variable != builder {
				return mutants, fmt.Errorf("the driver carried %s.%s (variable=%t) in the builder=%t arm", r.unit, r.root, variable, builder)
			}
			for j := range fuzzMutantsPerInstance {
				rng := &splitmix64{state: fuzzSeed ^ uint64(si)<<32 ^ uint64(j)}
				data := fuzzMutant(s.wire, rng)
				where := fmt.Sprintf("seed %#x, instance %d (%s), mutant %d, unit %s, root %s, builder arm %t\n  mutant (%d bytes): %x",
					fuzzSeed, si, s.name, j, r.unit, r.root, builder, len(data), data)
				got, err := leg.ask(s.root, data)
				if err != nil {
					return mutants, fmt.Errorf("%s\n  the driver died on the mutant: %w\n%s", where, err, leg.stderr.String())
				}
				want, err := fuzzEngine(m, def, variable, data)
				if err != nil {
					return mutants, fmt.Errorf("%s\n  %w", where, err)
				}
				if v := fuzzVerdict(got, want); v != "" {
					if dump := os.Getenv("SCHEMA_SQUIRREL_FUZZ_FAILED"); dump != "" {
						// THE THREE FILES OF A RED, for replay and diff: the
						// mutant, and what each side saved from it
						_ = os.MkdirAll(dump, 0o755)
						_ = os.WriteFile(filepath.Join(dump, "mutant.bin"), data, 0o644)
						_ = os.WriteFile(filepath.Join(dump, "squirrel.bin"), got.saved, 0o644)
						_ = os.WriteFile(filepath.Join(dump, "engine.bin"), want.saved, 0o644)
						where += "\n  written to " + dump + ": mutant.bin, squirrel.bin, engine.bin"
					}
					return mutants, fmt.Errorf("%s\n  %s\n  squirrel: %s\n  engine:   %s", where, v, got, want)
				}
				mutants++
			}
		}
		_ = leg.stdin.Close()
		if err := leg.cmd.Wait(); err != nil || leg.stderr.Len() > 0 {
			return mutants, fmt.Errorf("the driver (builder arm %t) did not leave cleanly at EOF: %w\n%s", builder, err, leg.stderr.String())
		}
	}
	// NOTHING PASSES BY BEING SKIPPED: every root of a generated unit is
	// carried by exactly one arm, and every seed yielded all its mutants
	for i, n := range carried {
		if n != 1 {
			return mutants, fmt.Errorf("the driver carried %s.%s in %d arms, want exactly one", roster[i].unit, roster[i].root, n)
		}
	}
	if want := len(seeds) * fuzzMutantsPerInstance; mutants != want {
		return mutants, fmt.Errorf("compared %d mutants, want %d (%d seeds x %d)", mutants, want, len(seeds), fuzzMutantsPerInstance)
	}
	return mutants, nil
}

// TestSquirrelWireFuzzAgreesWithTheGoEngine is the in-package differential:
// every corpus instance wire of a unit this target generates, mutated
// fuzzMutantsPerInstance times, read by the Squirrel load and the Go engine,
// and the completion, the six counters, the verdict and the resave must agree
// on every mutant (docs/SPEC-TABLES.md §4.2). A red prints the seed, the
// instance, the mutant's index, the unit, the root and the mutant's hex; with
// $SCHEMA_SQUIRREL_FUZZ_FAILED set it also writes the mutant and both saves
// to that directory.
func TestSquirrelWireFuzzAgreesWithTheGoEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	n, err := runWireFuzzDifferential(t, nil)
	if err != nil {
		t.Fatalf("after %d agreeing mutants: %v", n, err)
	}
	t.Logf("wire fuzz: %d mutants, 0 divergences (seed %#x, %d per instance)", n, fuzzSeed, fuzzMutantsPerInstance)
}

// TestSquirrelWireFuzzNegativeControl patches the emitted load in memory — one
// anchor, matched exactly once — so an array field's frame length past its
// body is clamped to the body instead of counted malformed (docs/SPEC-TABLES.md
// §3, §4: a frame that runs past its parent is framing damage), and requires
// the differential to go red.
func TestSquirrelWireFuzzNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	const anchor = "readFieldArray = function(fd, r, T, table, v, report, st) {\n" +
		"  local L = leb(r);\n" +
		"  if (r.bad || r.at + L > r.end) { report.malformed = true; return false; }\n"
	const sabotaged = "readFieldArray = function(fd, r, T, table, v, report, st) {\n" +
		"  local L = leb(r);\n" +
		"  if (r.bad) { report.malformed = true; return false; }\n" +
		"  if (r.at + L > r.end) L = r.end - r.at;\n"
	patched := 0
	_, err := runWireFuzzDifferential(t, func(name string, src []byte) []byte {
		if name != "schema_table_load.nut" {
			return src
		}
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the anchor appears %d times in the emitted %s, want exactly once", n, name)
		}
		patched++
		return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
	})
	if patched == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: no emitted unit carried schema_table_load.nut")
	}
	if err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: a load that clamps an array frame past its body instead of counting it malformed left the differential GREEN")
	}
	if !strings.Contains(err.Error(), "\n  squirrel: ") || !strings.Contains(err.Error(), "\n  engine:   ") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the differential went red, but not on a disagreement between the two sides: %v", err)
	}
	t.Logf("negative control: red as required:\n%v", err)
}
