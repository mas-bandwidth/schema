package rusttable

// THE FINITE RULE ON A DEQUANTIZED MESSAGE FLOAT (docs/SPEC-TABLES.md §3.3,
// §4.3). A message read dequantizes the peer's announced triple, and the value
// that comes out is a float read like any other: a NaN or an infinity is
// refused on its bits, never delivered, and never repaired by the range clamp
// that follows. The peer's announcement is not this reader's declaration, so a
// triple whose announced facts are each finite can still name a top step past
// the largest float32, and §3.3 refuses a triple whose own facts or whose
// derived delta is not finite before any body is read.
//
// The probe holds both emitted sites — a float32 field and a float64 field
// widened from the peer's quantized float32 — and a RANGED float field, whose
// clamp is false both ways for a NaN and so cannot stand in for the finite
// test. A control triple with finite steps still loads its value.
//
// A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by name
// when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by name
// when SCHEMA_REQUIRE_CORPUS=1. It reads only the generator's output: no
// clock, no socket, no network.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type quantFiniteCase struct {
	id           string
	announcement []byte
	batch        []byte
	announceOK   bool
	loadOK       bool
	value        string // the Rust literal the field must hold when loadOK
}

func quantTripleBits(min, max, res float32) []byte {
	out := binary.LittleEndian.AppendUint32(nil, math.Float32bits(min))
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(max))
	return binary.LittleEndian.AppendUint32(out, math.Float32bits(res))
}

// quantPeer is one declared peer: a fixed table carrying the field a reader
// names `reaction`, over a triple whose steps are all finite. The wire it
// writes is what the reader consumes after the announcement's triple is
// replaced.
func quantPeer(t *testing.T, max, value float64) (*ir.Unit, []byte) {
	t.Helper()
	src := fmt.Sprintf("package peer\n\nfixed table Peer\n{\n    reaction float32 | min = 0, max = %g, resolution = 1\n}\n", max)
	u := unitFrom(t, src)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Peer"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(fmt.Sprintf(`{"reaction": %g}`, value)), &rep) || rep != (tabletext.Report{}) {
		t.Fatalf("the peer text did not read cleanly: %+v", rep)
	}
	batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatalf("the peer batch did not encode: %v", err)
	}
	return u, batch
}

// quantHostile announces the peer's shape with a different triple. The caller
// names the declared triple exactly once in the announcement.
func quantHostile(t *testing.T, u *ir.Unit, declared, hostile []byte) []byte {
	t.Helper()
	own := tablewire.Announce(u)
	if bytes.Count(own, declared) != 1 {
		t.Fatalf("the announcement does not carry the declared triple once")
	}
	return bytes.Replace(own, declared, hostile, 1)
}

func TestRustMessageQuantizedFloatIsFinite(t *testing.T) {
	t.Parallel()
	quantRustc(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	// The triple the packet wire's own rule derives 341 steps from (9 bits),
	// every announced fact finite, whose top step dequantizes past the
	// largest float32. The checker refuses it in a declaration; only a
	// hostile announcement carries it.
	edge := quantTripleBits(3*(1<<103), math.MaxFloat32, 1e36)
	// delta = max - min overflows to +inf.
	deltaOverflow := quantTripleBits(-3e38, 3e38, 1e36)
	// min and max are themselves infinite.
	infiniteEnds := quantTripleBits(float32(math.Inf(-1)), float32(math.Inf(1)), 1)

	// The batch names the top step, 341 in the 9 bits the step count derives.
	topPeer, topBatch := quantPeer(t, 341, 341)
	topDeclared := quantTripleBits(0, 341, 1)
	// The control's steps are finite and its value is on the grid.
	controlPeer, controlBatch := quantPeer(t, 1000, 500)

	gunner := generateUnit(t, root, []string{"tables/examples"})
	gunnerCases := []quantFiniteCase{
		{id: "top_step", announcement: quantHostile(t, topPeer, topDeclared, edge), batch: topBatch, announceOK: true},
		{id: "delta_overflow", announcement: quantHostile(t, topPeer, topDeclared, deltaOverflow), batch: topBatch},
		{id: "infinite_ends", announcement: quantHostile(t, topPeer, topDeclared, infiniteEnds), batch: topBatch},
		{id: "finite_control", announcement: tablewire.Announce(controlPeer), batch: controlBatch, announceOK: true, loadOK: true, value: "500.0f32"},
	}
	t.Run("gunner_float32", func(t *testing.T) {
		t.Parallel()
		out := runQuantFiniteProbe(t, gunner, "GunnerConfigRow", "gunner_config_load_messages", gunnerCases, nil)
		for _, c := range gunnerCases {
			if line := messageBatchLine(out, c.id); line != "BATCH "+c.id+" ok" {
				t.Errorf("%s: %s", c.id, line)
			}
		}
	})

	// :561, a float64 field widened from the peer's quantized float32.
	wide := unitFrom(t, "package probe\n\nfixed table Probe\n{\n    reaction float64\n}\n")
	wideCases := []quantFiniteCase{
		{id: "top_step_float64", announcement: quantHostile(t, topPeer, topDeclared, edge), batch: topBatch, announceOK: true},
	}
	t.Run("float64_from_quantized_float32", func(t *testing.T) {
		t.Parallel()
		out := runQuantFiniteProbe(t, wide, "ProbeRow", "probe_load_messages", wideCases, nil)
		for _, c := range wideCases {
			if line := messageBatchLine(out, c.id); line != "BATCH "+c.id+" ok" {
				t.Errorf("%s: %s", c.id, line)
			}
		}
	})

	// :572, a ranged float: the clamp after the read is false both ways for
	// a NaN, so the finite test is what must refuse the value.
	ranged := unitFrom(t, "package probe\n\nfixed table Probe\n{\n    reaction float32 | min = 0, max = 1000, resolution = 1\n}\n")
	rangedCases := []quantFiniteCase{
		{id: "top_step_ranged", announcement: quantHostile(t, topPeer, topDeclared, edge), batch: topBatch, announceOK: true},
		{id: "nan_ranged", announcement: quantHostile(t, topPeer, topDeclared, infiniteEnds), batch: topBatch},
	}
	t.Run("ranged_float", func(t *testing.T) {
		t.Parallel()
		out := runQuantFiniteProbe(t, ranged, "ProbeRow", "probe_load_messages", rangedCases, nil)
		for _, c := range rangedCases {
			if line := messageBatchLine(out, c.id); line != "BATCH "+c.id+" ok" {
				t.Errorf("%s: %s", c.id, line)
			}
		}
	})
}

// TestRustMessageQuantizedFloatIsFiniteNegativeControl plants the packing
// exemption back into the generated loader and requires the same probe to go
// red: the finite test, not the range clamp, is what refuses a dequantized
// non-finite value.
func TestRustMessageQuantizedFloatIsFiniteNegativeControl(t *testing.T) {
	t.Parallel()
	quantRustc(t)

	const planted = "entry.shape.packing == 2 || table_finite_f32(lo as u32)"
	edge := quantTripleBits(3*(1<<103), math.MaxFloat32, 1e36)
	topPeer, topBatch := quantPeer(t, 341, 341)
	topDeclared := quantTripleBits(0, 341, 1)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	gunner := generateUnit(t, root, []string{"tables/examples"})
	cases := []quantFiniteCase{
		{id: "top_step", announcement: quantHostile(t, topPeer, topDeclared, edge), batch: topBatch, announceOK: true},
	}
	plantedAt := 0
	out := runQuantFiniteProbe(t, gunner, "GunnerConfigRow", "gunner_config_load_messages", cases, func(files map[string][]byte) {
		for name, data := range files {
			if strings.HasSuffix(name, "_message.rs") {
				src := string(data)
				if strings.Contains(src, "if table_finite_f32(lo as u32) {") {
					files[name] = []byte(strings.Replace(src, "if table_finite_f32(lo as u32) {", "if "+planted+" {", 1))
					plantedAt++
				}
			}
		}
	})
	if plantedAt == 0 {
		t.Fatal("the negative control planted the exemption nowhere in the generated loader")
	}
	if line := messageBatchLine(out, "top_step"); line == "BATCH top_step ok" {
		t.Fatalf("the negative control stayed green with the packing exemption planted: %s", line)
	}
}

func quantRustc(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "an external toolchain")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH, so the emitted message reader cannot be built or run: %v", err)
		}
		t.Skipf("rustc is not on PATH and SCHEMA_REQUIRE_CORPUS is unset: this is a functional test that compiles and runs Rust: %v", err)
	}
}

func runQuantFiniteProbe(t *testing.T, u *ir.Unit, row, load string, cases []quantFiniteCase, edit func(map[string][]byte)) string {
	t.Helper()
	slowtest.Gate(t, "an external toolchain")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" || name == MessageRuntimeModule+".rs" ||
			name == VariableRuntimeModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs") || strings.HasSuffix(name, "_message.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports, unused_variables, unused_mut)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString(`
fn say(id: &str, ok: bool, why: String) {
    if ok {
        println!("BATCH {id} ok");
    } else {
        println!("BATCH {id} BAD {why}");
    }
}

fn main() {
`)
	for _, c := range cases {
		verdict := "!announced && !ok && count == 0"
		if c.announceOK {
			if c.loadOK {
				verdict = fmt.Sprintf("announced && ok && count == 1 && values[0].reaction == %s", c.value)
			} else {
				verdict = "announced && !ok && count == 0"
			}
		}
		fmt.Fprintf(&main, `
    {
        const ANN: [u8; %d] = [%s];
        const BATCH: [u8; %d] = [%s];
        let mut storage = vec![TableMessageEntry::default(); 4096];
        let mut vocab = TableVocabulary::new(&mut storage);
        let mut arep = TableReport::default();
        let announced = announce_read(&mut vocab, &ANN, &mut arep);
        let mut values = vec![%s::default(); 8];
        let mut lrep = TableReport::default();
        let (count, ok) = %s(&mut values, &vocab, &BATCH, &mut lrep);
        let verdict = %s;
        say(%q, verdict, format!("announced={announced} ok={ok} count={count} value={:?} report={lrep:?}", values[0].reaction));
    }
`, len(c.announcement), rustBytes(c.announcement), len(c.batch), rustBytes(c.batch), row, load, verdict, c.id)
	}
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "quantprobe")
	args := []string{"--edition", "2024"}
	if cc, err := exec.LookPath("cc"); err == nil {
		// rustc's own search for `cc` runs execvp, whose first relative
		// attempt some sandboxes deny; the absolute path is what it means.
		args = append(args, "-C", "linker="+cc)
	}
	args = append(args, "-o", bin, "main.rs")
	cmd := exec.Command("rustc", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	return string(out)
}
