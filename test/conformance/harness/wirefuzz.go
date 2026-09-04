// `harness wire-fuzz` — the tolerant wire's fuzzer (docs/SPEC-TABLES.md §4.2,
// docs/SECURITY.md).
//
// A table read is untrusted input, so the read IS the verifier, and this is
// the gate on that claim: every pinned wire in the corpus, mutated, read by a
// language's leg and by the compiler's own engine, and the two must agree —
// the same report, the same decoded value (as the bytes it saves back), or
// both refusing to save — while the leg returns from every mutant and never
// asks for more region than the framing can justify.
//
// The ORACLE is internal/tablewire, a third implementation of the wire that
// no backend was written from. The leg is a COMMAND on a pipe, so a port is
// the thin reader test/conformance/README.md states and nothing else; the
// mutators, the oracle and the comparison live here once, for every language.
//
//	harness wire-fuzz --driver <cmd> [--seed S] [--n N]
//	harness wire-fuzz --driver <cmd> --replay <file> --unit <key> --root <table>
//
// Every mutant is a pure function of (seed, pass, index), and a failure writes
// the mutant to --failed and prints the replay command.
package main

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
	"runtime/debug"
	"strings"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wireFuzzOptions is what the command line settles.
type wireFuzzOptions struct {
	driver string
	seed   uint64
	n      int
	replay string
	unit   string
	root   string
	failed string
}

// wireRoot is one (unit, root) the seeds name, with what the oracle needs to
// read it and to state the region a variable-class reader may ask for.
type wireRoot struct {
	unit     string
	root     string
	model    *tabletext.Model
	def      *ir.Struct
	variable bool
	// the C ABI storage each node type commands (docs/SPEC-TABLES.md §6.5,
	// §20.3), by wire type id, eight-aligned as a reader's LoadMeasure rounds it
	storage     map[uint64]int64
	rootStorage int64
	maxStorage  int64
}

// nodeDirEntryBytes is one attribution entry (§6.3): a u64 offset and a u64
// type id.
const nodeDirEntryBytes = int64(16)

// nodeRecordHeaderBytes is the least a record costs on the wire (§3.1): its
// type id and its length. It bounds how many records a wire can carry.
const nodeRecordHeaderBytes = int64(12)

func alignUp8(n int64) int64 { return (n + 7) &^ 7 }

func newWireRoot(u *units, unitKey, rootName string) (*wireRoot, error) {
	unit, err := u.get(unitKey)
	if err != nil {
		return nil, err
	}
	m := tabletext.NewModel(unit)
	def := m.Lookup(rootName)
	if def == nil {
		return nil, fmt.Errorf("unit %s declares no table %s", unitKey, rootName)
	}
	r := &wireRoot{unit: unitKey, root: rootName, model: m, def: def, variable: m.IsVariable(rootName), storage: map[uint64]int64{}}
	r.rootStorage = alignUp8(ir.RecordLayout(unit, def).Size)
	for name := range ir.TableClosure(unit) {
		st := m.Lookup(name)
		if st == nil {
			continue
		}
		size := alignUp8(ir.RecordLayout(unit, st).Size)
		r.storage[ir.TableTypeId(name)] = size
		if size > r.maxStorage {
			r.maxStorage = size
		}
	}
	return r, nil
}

// legReply is what a leg answers for one mutant (test/conformance/README.md,
// "The wire-fuzz driver").
type legReply struct {
	loaded   bool
	report   Counts
	measure  int64
	saved    []byte
	saveFail bool
}

// oracleAnswer is what the engine says the same bytes mean.
type oracleAnswer struct {
	report  Counts
	encoded []byte
	encFail bool
	// the region bytes a variable-class reader owes: exact when the node
	// table read whole, else the most the framing could have commanded
	exact bool
	bytes int64
}

func (r *wireRoot) oracle(data []byte) (ans oracleAnswer, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the oracle PANICKED on the mutant: %v\n%s", p, debug.Stack())
		}
	}()
	inst := r.model.New(r.def)
	var rep tabletext.Report
	ok, derr := tablewire.Decode(r.model, inst, data, &rep)
	var refusal *tablewire.FormRefusal
	if errors.As(derr, &refusal) {
		// A FORM BYTE THIS READER DOES NOT CARRY IS A REFUSAL, and the refusal
		// is the answer: nothing was decoded, no counter moved, and no damage
		// is reported (docs/SPEC-TABLES.md §3). A leg must say the same.
		ans.report = Counts{Refused: true}
		// nothing was decoded, so what a leg saves is what it read into: the
		// value at its declared defaults, which is what the fresh instance
		// encodes here. Both must answer the same bytes, as they must on
		// every other mutant (§4.2).
		if encoded, eerr := tablewire.Encode(r.model, inst); eerr != nil {
			ans.encFail = true
		} else {
			ans.encoded = encoded
		}
		return ans, nil
	}
	if derr != nil {
		return ans, fmt.Errorf("the oracle refused the root itself: %w", derr)
	}
	ans.report = Counts{Unknown: rep.Unknown, KindMismatch: rep.KindMismatch, Clamped: rep.Clamped, Duplicate: rep.Duplicate, Malformed: rep.Malformed || !ok}
	encoded, eerr := tablewire.Encode(r.model, inst)
	if eerr != nil {
		ans.encFail = true
	} else {
		ans.encoded = encoded
	}
	if r.variable {
		types, whole := tablewire.NodeRecordTypes(data)
		if whole {
			ans.exact = true
			ans.bytes = r.rootStorage + (int64(len(types))+1)*nodeDirEntryBytes
			for _, t := range types {
				ans.bytes += r.storage[t] // a type id this build cannot name commands none
			}
		} else {
			records := int64(len(data))/nodeRecordHeaderBytes + 1
			ans.bytes = r.rootStorage + records*(r.maxStorage+nodeDirEntryBytes)
		}
	}
	return ans, nil
}

// ---------------------------------------------------------------------------
// the leg on its pipe
// ---------------------------------------------------------------------------

type wireLeg struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr bytes.Buffer
	known  []bool
}

func startWireLeg(command string, roots []*wireRoot) (*wireLeg, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("--driver names no command")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	leg := &wireLeg{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20)}
	cmd.Stderr = &leg.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %q: %w", command, err)
	}

	// the roster: which roots the stream will name, and which the leg has
	var roster bytes.Buffer
	_ = binary.Write(&roster, binary.LittleEndian, uint32(len(roots)))
	for _, r := range roots {
		_ = binary.Write(&roster, binary.LittleEndian, uint16(len(r.unit)))
		roster.WriteString(r.unit)
		_ = binary.Write(&roster, binary.LittleEndian, uint16(len(r.root)))
		roster.WriteString(r.root)
	}
	if _, err := stdin.Write(roster.Bytes()); err != nil {
		return nil, fmt.Errorf("the leg closed its input during the roster: %w", err)
	}
	leg.known = make([]bool, len(roots))
	for i := range roots {
		b, err := leg.stdout.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("the leg did not answer the roster: %w\n%s", err, leg.stderr.String())
		}
		leg.known[i] = b != 0
	}
	return leg, nil
}

func (l *wireLeg) send(rootIndex int, data []byte) error {
	var head [8]byte
	binary.LittleEndian.PutUint32(head[0:], uint32(rootIndex))
	binary.LittleEndian.PutUint32(head[4:], uint32(len(data)))
	if _, err := l.stdin.Write(head[:]); err != nil {
		return err
	}
	_, err := l.stdin.Write(data)
	return err
}

func (l *wireLeg) receive() (legReply, error) {
	var rep legReply
	// loaded, the four counters, malformed, the REFUSAL VERDICT (§3), the
	// measure and the saved length
	var head [1 + 4*4 + 1 + 1 + 8 + 8]byte
	if _, err := io.ReadFull(l.stdout, head[:]); err != nil {
		return rep, err
	}
	rep.loaded = head[0] != 0
	rep.report.Unknown = int(int32(binary.LittleEndian.Uint32(head[1:])))
	rep.report.KindMismatch = int(int32(binary.LittleEndian.Uint32(head[5:])))
	rep.report.Clamped = int(int32(binary.LittleEndian.Uint32(head[9:])))
	rep.report.Duplicate = int(int32(binary.LittleEndian.Uint32(head[13:])))
	rep.report.Malformed = head[17] != 0
	rep.report.Refused = head[18] != 0
	rep.measure = int64(binary.LittleEndian.Uint64(head[19:]))
	n := int64(binary.LittleEndian.Uint64(head[27:]))
	if n < 0 {
		rep.saveFail = true
		return rep, nil
	}
	rep.saved = make([]byte, n)
	if _, err := io.ReadFull(l.stdout, rep.saved); err != nil {
		return rep, err
	}
	return rep, nil
}

// close ends the stream and waits for the leg to leave at EOF.
func (l *wireLeg) close() error {
	_ = l.stdin.Close()
	return l.cmd.Wait()
}

// kill ends a leg mid-stream, on a failure: it may be blocked writing a reply
// nobody will read now, so a wait alone would never return.
func (l *wireLeg) kill() {
	_ = l.stdin.Close()
	_ = l.cmd.Process.Kill()
	_ = l.cmd.Wait()
}

// ---------------------------------------------------------------------------
// the run
// ---------------------------------------------------------------------------

// wireVerdict compares one leg reply with the oracle's answer and names the
// first disagreement, or "" when there is none.
func wireVerdict(root *wireRoot, reply legReply, ans oracleAnswer) string {
	if !reply.loaded {
		if root.variable && reply.measure < 0 {
			// LOADMEASURE REFUSED THE FRAMING, so there was no region to load
			// into and no value to compare: a wire that cannot be OPENED at
			// all — fewer than nine bytes, a table that cannot be read whole,
			// a form this reader does not carry — has no body and no numbering
			// (docs/SPEC-TABLES.md §3). The oracle has to agree there was
			// nothing to read.
			if !ans.report.Malformed && !ans.report.Refused {
				return "the leg's LoadMeasure refused the framing; the oracle read the same bytes cleanly"
			}
			if reply.report != ans.report {
				return fmt.Sprintf("the report differs: the leg says %s, the oracle says %s", reply.report, ans.report)
			}
			return ""
		}
		return "the leg returned no root — its LoadMeasure sized a region its Load then refused"
	}
	if reply.report != ans.report {
		return fmt.Sprintf("the report differs: the leg says %s, the oracle says %s", reply.report, ans.report)
	}
	if reply.saveFail != ans.encFail {
		if reply.saveFail {
			return fmt.Sprintf("the leg refused to save the value it loaded; the oracle encoded %d bytes", len(ans.encoded))
		}
		return fmt.Sprintf("the oracle refused to encode the value it decoded; the leg saved %d bytes", len(reply.saved))
	}
	if !reply.saveFail && !bytes.Equal(reply.saved, ans.encoded) {
		at := 0
		for at < len(reply.saved) && at < len(ans.encoded) && reply.saved[at] == ans.encoded[at] {
			at++
		}
		return fmt.Sprintf("the decoded value differs: the leg saves %d bytes, the oracle %d, first difference at byte %d", len(reply.saved), len(ans.encoded), at)
	}
	if root.variable {
		if ans.exact && reply.measure != ans.bytes {
			return fmt.Sprintf("LoadMeasure asks for %d region bytes; the framing commands exactly %d", reply.measure, ans.bytes)
		}
		if reply.measure > ans.bytes {
			return fmt.Sprintf("LoadMeasure asks for %d region bytes; the framing can command at most %d", reply.measure, ans.bytes)
		}
	}
	return ""
}

func wireFuzz(m *Manifest, opts wireFuzzOptions) error {
	if opts.driver == "" {
		return fmt.Errorf("wire-fuzz needs --driver")
	}
	u := newUnits(m)

	// the seeds: every instance and every report case, each a (unit, root,
	// wire) the corpus already pins
	var seeds []*wireSeed
	var roots []*wireRoot
	rootIndex := map[string]int{}
	addRoot := func(unit, root string) (int, error) {
		key := unit + "." + root
		if i, ok := rootIndex[key]; ok {
			return i, nil
		}
		r, err := newWireRoot(u, unit, root)
		if err != nil {
			return 0, err
		}
		rootIndex[key] = len(roots)
		roots = append(roots, r)
		return len(roots) - 1, nil
	}
	seedRoot := map[*wireSeed]int{}
	addSeed := func(name, unit, root, wirePath string) error {
		wire, err := os.ReadFile(wirePath)
		if err != nil {
			return err
		}
		ri, err := addRoot(unit, root)
		if err != nil {
			return err
		}
		s := &wireSeed{name: name, unit: unit, root: root, wire: wire, frame: frameWire(wire)}
		seeds = append(seeds, s)
		seedRoot[s] = ri
		return nil
	}
	if opts.replay != "" {
		if opts.unit == "" || opts.root == "" {
			return fmt.Errorf("--replay needs --unit and --root")
		}
		if err := addSeed(filepath.Base(opts.replay), opts.unit, opts.root, opts.replay); err != nil {
			return err
		}
	} else {
		for _, inst := range m.Instances {
			if err := addSeed(inst.Name, inst.Unit, inst.Root, inst.Wire); err != nil {
				return err
			}
		}
		for _, rc := range m.Reports {
			if err := addSeed(rc.Name, rc.Unit, rc.Root, rc.Wire); err != nil {
				return err
			}
		}
	}

	leg, err := startWireLeg(opts.driver, roots)
	if err != nil {
		return err
	}
	absent := 0
	var live []*wireSeed
	for _, s := range seeds {
		if leg.known[seedRoot[s]] {
			live = append(live, s)
		} else {
			absent++
		}
	}
	if len(live) == 0 {
		leg.kill()
		return fmt.Errorf("the leg has a codec for none of the %d roots the corpus names", len(roots))
	}

	// the stream: a producer that both the writer and the comparator follow,
	// so a mutant is generated once and never held past its comparison
	type item struct {
		m *wireMutant
	}
	produced := make(chan item, 256)
	go func() {
		if opts.replay != "" {
			produced <- item{m: &wireMutant{seed: live[0], pass: "replay", data: live[0].wire}}
		} else {
			for _, s := range live {
				pass := map[string]int{}
				enumerated(s, func(name string, data []byte) {
					produced <- item{m: &wireMutant{seed: s, pass: name, index: pass[name], data: data}}
					pass[name]++
				})
			}
			for i := 0; i < opts.n; i++ {
				mut := randomMutant(live, opts.seed, i)
				produced <- item{m: mut}
			}
		}
		close(produced)
	}()
	sent := make(chan *wireMutant, 256)
	writeErr := make(chan error, 1)
	go func() {
		for it := range produced {
			if err := leg.send(seedRoot[it.m.seed], it.m.data); err != nil {
				writeErr <- err
				// drain, so the producer can finish
				for range produced {
				}
				close(sent)
				return
			}
			sent <- it.m
		}
		writeErr <- nil
		close(sent)
	}()

	start := time.Now()
	total := 0
	enumeratedCount := 0
	for mut := range sent {
		root := roots[seedRoot[mut.seed]]
		reply, err := leg.receive()
		if err != nil {
			leg.kill()
			return wireFailure(opts, mut, total, fmt.Sprintf("the leg died on the mutant (%v)", err), leg.stderr.String())
		}
		ans, err := root.oracle(mut.data)
		if err != nil {
			leg.kill()
			return wireFailure(opts, mut, total, err.Error(), "")
		}
		if verdict := wireVerdict(root, reply, ans); verdict != "" {
			leg.kill()
			// THE TWO ANSWERS BESIDE THE MUTANT: a divergence in the decoded
			// VALUE is a diff, and a diff needs both sides on disk
			if dir := filepath.Dir(opts.failed); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
				_ = os.WriteFile(filepath.Join(dir, "leg.bin"), reply.saved, 0o644)
				_ = os.WriteFile(filepath.Join(dir, "oracle.bin"), ans.encoded, 0o644)
			}
			detail := fmt.Sprintf("  leg:    loaded=%t report=%s measure=%d saved=%s\n  oracle: report=%s encoded=%s",
				reply.loaded, reply.report, reply.measure, describeBytes(reply.saved, reply.saveFail),
				ans.report, describeBytes(ans.encoded, ans.encFail))
			if root.variable {
				bound := "at most"
				if ans.exact {
					bound = "exactly"
				}
				detail += fmt.Sprintf("\n  region: the framing commands %s %d bytes", bound, ans.bytes)
			}
			return wireFailure(opts, mut, total, verdict, detail+"\n"+leg.stderr.String())
		}
		total++
		if mut.pass != "random" {
			enumeratedCount++
		}
	}
	if err := <-writeErr; err != nil {
		stderr := leg.stderr.String()
		leg.kill()
		return fmt.Errorf("FAILED: the leg closed its input mid-stream: %w\n%s", err, stderr)
	}
	if err := leg.close(); err != nil {
		return fmt.Errorf("FAILED: the leg exited with an error after the last mutant: %w\n%s", err, leg.stderr.String())
	}
	elapsed := time.Since(start)
	if opts.replay != "" {
		// replay sends the one mutant the file holds and nothing else, so the
		// enumerated/random split has nothing to say about it
		fmt.Printf("wire-fuzz: replay of %s over %s.%s, %d mutant, 0 divergences, %.1f s\n",
			opts.replay, opts.unit, opts.root, total, elapsed.Seconds())
		return nil
	}
	rate := float64(total) / elapsed.Seconds()
	absence := ""
	if absent > 0 {
		absence = fmt.Sprintf(", %d seeds absent (roots the leg has no codec for)", absent)
	}
	fmt.Printf("wire-fuzz: seed %d, %d seeds over %d roots%s, %d enumerated + %d random = %d mutants, 0 divergences, %.1f s, %.0f mutants/s\n",
		opts.seed, len(live), len(roots), absence, enumeratedCount, total-enumeratedCount, total, elapsed.Seconds(), rate)
	return nil
}

func describeBytes(b []byte, refused bool) string {
	if refused {
		return "refused"
	}
	return fmt.Sprintf("%d bytes", len(b))
}

// wireFailure writes the mutant where a person can pick it up and prints the
// one command that replays it alone.
func wireFailure(opts wireFuzzOptions, mut *wireMutant, passed int, verdict, detail string) error {
	if err := os.MkdirAll(filepath.Dir(opts.failed), 0o755); err == nil {
		_ = os.WriteFile(opts.failed, mut.data, 0o644)
	}
	msg := fmt.Sprintf("FAILED after %d mutants: %s\n  seed %s (%s.%s), pass %s #%d, mutant of %d bytes written to %s\n%s\n  replay: %s wire-fuzz --driver %q --replay %s --unit %s --root %s",
		passed, verdict, mut.seed.name, mut.seed.unit, mut.seed.root, mut.pass, mut.index, len(mut.data), opts.failed,
		strings.TrimRight(detail, "\n"), os.Args[0], opts.driver, opts.failed, mut.seed.unit, mut.seed.root)
	return fmt.Errorf("%s", msg)
}
