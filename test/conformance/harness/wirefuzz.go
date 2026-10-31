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
	"crypto/sha256"
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
	// message replays the file as a MESSAGE (docs/SPEC-TABLES.md §3.3),
	// against the unit's own announced table rather than a trailer of its
	// own. A failure on a message seed prints it.
	message bool
	// retain is THE RETENTION ARM (docs/SPEC-TABLES.md §6.6, §4.2): the same
	// mutants through both engines' RETAINING paths, comparing the two
	// retention counters beside the six and the saved bytes beside them.
	// RETENTION IS THE VARIABLE CLASS'S, so the arm's roster is the
	// variable-class FILE roots and nothing else: a fixed-class root's
	// LoadRetain is refused by name, and a form-2 SaveRetain refuses by name
	// (§3.3).
	retain  bool
	builder bool // mutable file reads have no region preflight
	failed  string
	vectors string
	// finiteOwed names the docs/roadmap.sexp card that owes this leg the
	// finite float rule (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27): until
	// that card lands, a mutant the oracle refuses as non_finite_float is
	// counted under the card's name and not compared, because the leg still
	// carries the pattern. Empty is a leg that keeps the rule, and there the
	// leg must refuse the same mutant.
	finiteOwed string
}

// wireRoot is one (unit, root) the seeds name, with what the oracle needs to
// read it and to state the region a variable-class reader may ask for.
type wireRoot struct {
	unit     string
	root     string
	model    *tabletext.Model
	def      *ir.Struct
	variable bool
	// message is the MESSAGE FORM (docs/SPEC-TABLES.md §3.3): the mutants for
	// this root are form 2 wires and their references resolve against the
	// CONNECTION's table rather than a trailer of their own. `vocabulary` is
	// that table — the unit's whole vocabulary, which is a pure function of
	// the build version, so both sides derive it and neither carries it.
	message    bool
	vocabulary *tablewire.Vocabulary
	// retain says this roster entry is driven through both engines' RETAINING
	// paths (docs/SPEC-TABLES.md §6.6)
	retain  bool
	builder bool
	// the C ABI storage each node type commands (docs/SPEC-TABLES.md §6.5,
	// §20.3), by wire type id, rounded to the unit alignment as a reader's LoadMeasure rounds it
	storage               map[uint64]int64
	rootStorage           int64
	align                 int64
	maxStorage            int64
	maxElement            int64
	blobBytes, blobString bool
}

// nodeDirEntryBytes is one attribution entry (§6.3): a u64 offset and a u64
// type id.
const nodeDirEntryBytes = int64(16)

// nodeRecordHeaderBytes is the least a record costs on the wire (§3.1): its
// type id and its length. It bounds how many records a wire can carry.
const nodeRecordHeaderBytes = int64(2)

// amplificationCeiling is the pinned ceiling on the region a table read may be
// made to materialize from a wire of the given size (schema#466,
// docs/SPEC-TABLES.md §6.5, docs/SECURITY.md). A hostile record costs its type
// id reference and its length — nodeRecordHeaderBytes, two wire bytes — and
// commands at most the largest record this root can place plus one directory
// entry, so a wire can command no more than the root, one record per two
// bytes, and the largest element a list or map under the root can add. The
// row this constant pins is the amplification ratio (region bytes over wire
// bytes): a mutant above the ceiling is a divergence even before the leg and
// the oracle are compared, and it is the same number the oracle's exact
// answer takes as its upper form.
func (r *wireRoot) amplificationCeiling(wireBytes int) int64 {
	records := int64(wireBytes)/nodeRecordHeaderBytes + 1
	ceiling := r.rootStorage + records*(r.maxStorage+nodeDirEntryBytes)
	ceiling += int64(wireBytes) * (r.maxElement + 16)
	if r.blobBytes || r.blobString {
		ceiling += int64(wireBytes) + records*16
	}
	return ceiling
}

func (r *wireRoot) roundStorage(n int64) int64 { return (n + r.align - 1) & -r.align }

// THE RETENTION ARM'S TWO CAPACITIES (docs/SPEC-TABLES.md §6.6), and they are
// declared LARGE ON PURPOSE.
//
// A record's BYTE cost is THE PORT'S OWN: the page fixes what a record must
// CARRY and leaves the layout inside the buffer to the port, and nothing
// compares two ports' buffers. Two engines run at one tight byte capacity
// would therefore drop DIFFERENT records, and the arm would be measuring the
// two layouts rather than the feature. The ID LIST'S capacity is a COUNT and
// is comparable, so it is set to the page's own `C / 8` bound, which is what
// no file can beat.
//
// The two capacity-overflow rules are held where one engine answers alone: the
// reference's own retain gate (test/tables/retain_main.cpp) and the oracle's
// (internal/tablewire/retain_test.go), each pinning the buffer one byte short
// of the last record and the id list one entry short.
const retainFuzzCapacity = 1 << 20
const retainFuzzIdCapacity = retainFuzzCapacity / 8

func newWireRoot(u *units, unitKey, rootName string, message, retain bool) (*wireRoot, error) {
	unit, err := u.get(unitKey)
	if err != nil {
		return nil, err
	}
	m := tabletext.NewModel(unit)
	def := m.Lookup(rootName)
	if def == nil {
		return nil, fmt.Errorf("unit %s declares no table %s", unitKey, rootName)
	}
	r := &wireRoot{unit: unitKey, root: rootName, model: m, def: def, variable: m.IsVariable(rootName), message: message, retain: retain, storage: map[uint64]int64{}}
	if retain && (!r.variable || message) {
		// RETENTION IS THE VARIABLE CLASS'S AND A FIXED-CLASS ROOT GETS NONE
		// (docs/SPEC-TABLES.md §6.6), and a form-2 SaveRetain refuses by name
		// (§3.3): neither is a roster entry this arm can hold.
		return nil, fmt.Errorf("unit %s root %s: retention is the variable class's variable form and nothing else", unitKey, rootName)
	}
	if message && r.variable {
		// THE MESSAGE FORM IS FIXED TABLES ONLY (docs/SPEC-TABLES.md §3.3)
		return nil, fmt.Errorf("unit %s root %s: %s", unitKey, rootName, VariableMessageRefusal)
	}
	if message {
		r.vocabulary = &tablewire.Vocabulary{}
		if err := r.vocabulary.AnnounceRead(ir.TableAnnouncement(unit), &tabletext.Report{}); err != nil {
			return nil, fmt.Errorf("unit %s: its own announcement was refused: %w", unitKey, err)
		}
	}
	for name := range ir.TableClosure(unit) {
		st := m.Lookup(name)
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.IsList() {
				size, _ := ir.ListElementLayout(unit, f)
				r.maxElement = max(r.maxElement, size)
			} else if f.IsMap() {
				r.maxElement = max(r.maxElement, ir.RecordLayout(unit, f.MapEntry).Size)
			}
		}
	}
	r.align = ir.TableRegionAlign(unit)
	r.rootStorage = r.roundStorage(ir.RecordLayout(unit, def).Size)
	r.blobBytes, r.blobString = ir.PointerReachableBlobs(def)
	// THE STORAGE A RECORD COMMANDS is its type's, and only for a type THIS
	// ROOT can place: a table no pointer below the root targets is a node the
	// reader cannot name, so it commands none (docs/SPEC-TABLES.md §3.1,
	// §6.5), exactly as the reference's <Root>NodeStorage answers -1 for one.
	for _, reached := range ir.PointerReachable(def) {
		name := reached.Name
		st := m.Lookup(name)
		if st == nil {
			continue
		}
		size := r.roundStorage(ir.RecordLayout(unit, st).Size)
		r.storage[ir.TableTypeId(st.WireName())] = size
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
	// blobShared marks a save refused BY NAME as blob_shared
	// (docs/SPEC-TABLES.md §2.5): a leg answers the saved length -2 for it. Every writer
	// of the standard refuses a blob two slots name; the C++ reference still
	// writes one, so a differential against it reads this refusal as the
	// standard's answer and not as a divergence.
	blobShared bool
	// RETENTION'S TWO COUNTERS (docs/SPEC-TABLES.md §6.6). They ride beside
	// Counts rather than inside it, because Counts is the MANIFEST's own
	// report spelling, which every port's `report` surface prints and which
	// this feature does not move.
	retained   int
	retainLost int
}

// oracleAnswer is what the engine says the same bytes mean.
type oracleAnswer struct {
	report  Counts
	encoded []byte
	encFail bool
	// the region bytes a variable-class reader owes: exact when the node
	// table read whole, else the most the framing could have commanded
	exact          bool
	measureRefused bool
	bytes          int64
	builderStopped bool
	// the same two counters from the engine
	retained   int
	retainLost int
	// nonFinite is the oracle's REFUSAL of a float whose bits are a NaN or an
	// infinity (docs/SPEC-TABLES.md §3.3): every read refuses it, tolerant
	// and strict, so the whole of what a leg owes is the refusal itself.
	nonFinite bool
}

func (r *wireRoot) oracle(data []byte) (ans oracleAnswer, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the oracle PANICKED on the mutant: %v\n%s", p, debug.Stack())
		}
	}()
	// A region caller measures before decoding. A storage refusal ends that
	// path; a header that merely crosses its L is still decoded below using
	// the region's recovery rules rather than the builder's count refusal.
	if r.variable && !r.message && !r.builder {
		if _, _, refused := tablewire.FileRegionMeasure(r.model.Unit, r.def, data); refused {
			ans.measureRefused, ans.encFail, ans.bytes = true, true, -1
			return ans, nil
		}
	}
	inst := r.model.New(r.def)
	var rep tabletext.Report
	// THE MESSAGE FORM's mutants are read against the CONNECTION's table and
	// written back the same way (docs/SPEC-TABLES.md §3.3). Every other rule
	// of the read is §3's and §4's, unchanged, so the branch is here and
	// nowhere else in this file.
	decode := func() (bool, error) { return tablewire.DecodeRegion(r.model, inst, data, &rep) }
	encode := func() ([]byte, error) { return tablewire.Encode(r.model, inst) }
	if r.builder {
		decode = func() (bool, error) { return tablewire.Decode(r.model, inst, data, &rep) }
	}
	if r.retain {
		// THE RETENTION ARM (docs/SPEC-TABLES.md §6.6): the same mutant read
		// and written back through the retaining pair, with the two stores the
		// arm declares.
		retain := tablewire.Retain{Capacity: retainFuzzCapacity, IdCapacity: retainFuzzIdCapacity}
		decode = func() (bool, error) { return tablewire.DecodeRegionRetain(r.model, inst, data, &retain, &rep) }
		encode = func() ([]byte, error) {
			var saved tabletext.Report
			out, err := tablewire.EncodeRetain(r.model, inst, &retain, &saved)
			// THE REPORT ACCUMULATES ACROSS THE PAIR (§6.6): the caller zeroes
			// one report before the load and reads it after the save, and
			// `retain_lost` at the end is the sum of what the load could not
			// keep and what the save could not place.
			rep.RetainLost += saved.RetainLost
			return out, err
		}
	}
	if r.message {
		// THE WIRE MAY CARRY UP TO 256 BODIES, and the leg holds room for the
		// wire's M, so the oracle reads the batch whole and answers body one
		bodies, _ := tablewire.MessageCount(data, r.vocabulary)
		if bodies < 1 {
			bodies = 1
		}
		batch := make([]*tabletext.Instance, bodies)
		batch[0] = inst
		for i := 1; i < len(batch); i++ {
			batch[i] = r.model.New(r.def)
		}
		decode = func() (bool, error) {
			_, ok, err := tablewire.DecodeMessages(r.model, batch, data, r.vocabulary, &rep)
			return ok, err
		}
		encode = func() ([]byte, error) { return tablewire.EncodeMessage(r.model, inst) }
	}
	ok, derr := decode()
	if _, refused := errors.AsType[*tablewire.NonFiniteRefusal](derr); refused {
		ans.nonFinite = true
		return ans, nil
	}
	if r.builder {
		ans.builderStopped = !ok
		if _, ok := errors.AsType[*tablewire.CountRefusal](derr); ok {
			derr = nil
		}
	}
	if tablewire.Refused(derr) {
		// A FORM BYTE THIS READER DOES NOT CARRY IS A REFUSAL, and the refusal
		// is the answer: nothing was decoded, no counter moved, and no damage
		// is reported (docs/SPEC-TABLES.md §3). A leg must say the same.
		ans.report = Counts{Refused: true}
		if r.builder {
			ans.encFail = true
			return ans, nil
		}
		// nothing was decoded, so what a leg saves is what it read into: the
		// value at its declared defaults, which is what the fresh instance
		// encodes here. Both must answer the same bytes, as they must on
		// every other mutant (§4.2).
		if encoded, eerr := encode(); eerr != nil {
			ans.encFail = true
		} else {
			ans.encoded = encoded
		}
		return ans, nil
	}
	if derr != nil {
		return ans, fmt.Errorf("the oracle refused the root itself: %w", derr)
	}
	ans.report = Counts{Unknown: rep.Unknown, KindMismatch: rep.KindMismatch, Widened: rep.Widened, Clamped: rep.Clamped, Duplicate: rep.Duplicate, Malformed: rep.Malformed || (!ok && !r.builder)}
	if r.builder && !ok {
		ans.encFail = true
		return ans, nil
	}
	encoded, eerr := encode()
	// the encode is what runs the SAVE half of the retaining pair, so the two
	// counters are read after it and not before
	ans.retained, ans.retainLost = rep.Retained, rep.RetainLost
	if eerr != nil {
		ans.encFail = true
	} else {
		ans.encoded = encoded
	}
	if r.variable && !r.builder {
		measured, whole, refused := tablewire.FileRegionMeasure(r.model.Unit, r.def, data)
		if refused {
			ans.bytes = -1
			ans.measureRefused = true
			return ans, nil
		}
		if whole {
			ans.exact = true
			ans.bytes = measured
		} else {
			records := int64(len(data))/nodeRecordHeaderBytes + 1
			ans.bytes = r.rootStorage + records*(r.maxStorage+nodeDirEntryBytes)
			ans.bytes += int64(len(data)) * (r.maxElement + 16)
			if r.blobBytes || r.blobString {
				ans.bytes += int64(len(data)) + records*16
			}
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

// wireClock is the timer the driver-reply deadline waits on: real time in the
// harness, an injected one in a test. The deadline only ever waits, so the
// interface is After alone; nothing here reads the time of day.
type wireClock interface {
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// replyDeadline turns a stalled driver into a named failure instead of a hang
// (test/conformance/README.md, "The wire-fuzz driver"): the harness never
// waits forever for a reply a driver will not send. A deadline alone is not
// the fix — the stream is still a pure request/reply loop — but it backstops
// the loop so a driver that stops answering is reported by name.
const replyDeadline = 30 * time.Second

// receiveWithDeadline reads one reply, bounded by replyDeadline against clk. A
// driver that stops answering is reported as a stall rather than hanging the
// run. The read runs on a goroutine so the deadline can interrupt it; when
// the deadline fires that goroutine is left blocked on the pipe until the leg
// is killed, which the caller always does on this path.
func (l *wireLeg) receiveWithDeadline(clk wireClock) (legReply, error) {
	type result struct {
		rep legReply
		err error
	}
	ch := make(chan result, 1)
	go func() {
		rep, err := l.receive()
		ch <- result{rep, err}
	}()
	select {
	case r := <-ch:
		return r.rep, r.err
	case <-clk.After(replyDeadline):
		return legReply{}, fmt.Errorf("the driver stalled: no reply within %s", replyDeadline)
	}
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

	// the roster: which roots the stream will name, in which FORM, and which
	// the leg has. The form is the wire's own byte (docs/SPEC-TABLES.md §3,
	// §3.3) rather than a flag of this protocol's minting, so a leg reads the
	// value it already knows: `1` is the variable form and `2` is the message
	// form, whose mutants resolve against the connection's announced table.
	// A leg derives that table from its OWN unit's announcement, because the
	// vocabulary is a pure function of the build version and both sides
	// derive the same one, so the roster names the form and never carries an
	// announcement.
	var roster bytes.Buffer
	_ = binary.Write(&roster, binary.LittleEndian, uint32(len(roots)))
	for _, r := range roots {
		_ = binary.Write(&roster, binary.LittleEndian, uint16(len(r.unit)))
		roster.WriteString(r.unit)
		_ = binary.Write(&roster, binary.LittleEndian, uint16(len(r.root)))
		roster.WriteString(r.root)
		form := byte(ir.TableWireForm)
		if r.message {
			form = ir.TableWireMessageForm
		}
		roster.WriteByte(form)
		// AND WHETHER THIS ENTRY RETAINS (docs/SPEC-TABLES.md §6.6): a leg with
		// no retention for this root answers `0` for the entry, exactly as it
		// does for a root it cannot name.
		retain := byte(0)
		if r.retain {
			retain = 1
		}
		roster.WriteByte(retain)
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
	// loaded, the five counters, malformed, the REFUSAL VERDICT (§3), the
	// measure and the saved length
	var head [1 + 5*4 + 1 + 1 + 8 + 8 + 4 + 4]byte
	if _, err := io.ReadFull(l.stdout, head[:]); err != nil {
		return rep, err
	}
	rep.loaded = head[0] != 0
	rep.report.Unknown = int(int32(binary.LittleEndian.Uint32(head[1:])))
	rep.report.KindMismatch = int(int32(binary.LittleEndian.Uint32(head[5:])))
	rep.report.Widened = int(int32(binary.LittleEndian.Uint32(head[9:])))
	rep.report.Clamped = int(int32(binary.LittleEndian.Uint32(head[13:])))
	rep.report.Duplicate = int(int32(binary.LittleEndian.Uint32(head[17:])))
	rep.report.Malformed = head[21] != 0
	rep.report.Refused = head[22] != 0
	rep.measure = int64(binary.LittleEndian.Uint64(head[23:]))
	// RETENTION'S TWO COUNTERS ride the reply always, and are zero on every
	// entry that does not retain (docs/SPEC-TABLES.md §6.6)
	rep.retained = int(int32(binary.LittleEndian.Uint32(head[31:])))
	rep.retainLost = int(int32(binary.LittleEndian.Uint32(head[35:])))
	n := int64(binary.LittleEndian.Uint64(head[39:]))
	if n < 0 {
		rep.saveFail = true
		rep.blobShared = n == -2
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
func wireVerdict(root *wireRoot, reply legReply, ans oracleAnswer, wireBytes int) string {
	if ans.nonFinite {
		// A NaN OR AN INFINITY IS REFUSED BY EVERY READ (docs/SPEC-TABLES.md
		// §3.3), and the refusal is the answer: a leg says so with the
		// refusal verdict, and nothing it decoded or counted on the way is
		// compared, because nothing of it lands.
		if !reply.report.Refused {
			return "the oracle refuses a non-finite float (non_finite_float); the leg did not refuse the wire"
		}
		return ""
	}
	if root.builder {
		// a damaged load is refused whole (docs/SPEC-TABLES.md §4), so on a
		// damaged mutant the builder's completion is not compared: the
		// malformed verdict below is the whole of what the leg owes
		if !ans.report.Malformed && reply.loaded == ans.builderStopped {
			return "builder completion differs from the oracle"
		}
	} else if !reply.loaded {
		if root.variable && reply.measure < 0 {
			// LOADMEASURE REFUSED THE FRAMING, so there was no region to load
			// into and no value to compare: a wire that cannot be OPENED at
			// all, fewer than nine bytes, a table that cannot be read whole,
			// a form this reader does not carry, has no body and no numbering
			// (docs/SPEC-TABLES.md §3). The oracle has to agree there was
			// nothing to read, AND THAT IS THE WHOLE OF WHAT IT OWES HERE.
			//
			// THE COUNTER TUPLES ARE NOT COMPARED ON THIS BRANCH, because the
			// two sides ran two different operations and the page rules both
			// of them. A LoadMeasure refusal moves no counter (§6.5), so the
			// leg's report is empty by rule; the oracle DECODED, and the
			// fields decoded before the damage stand with the counters they
			// earned (§3.3), so its report is not. A batch claiming 129
			// bodies in 66 bytes is exactly that shape: the leg refuses to
			// measure and the oracle reads body one and counts one malformed.
			// Requiring the tuples to match would require one of those two
			// sentences to be false.
			if !ans.measureRefused && !ans.report.Malformed && !ans.report.Refused {
				return "the leg's LoadMeasure refused the framing; the oracle read the same bytes cleanly"
			}
			return ""
		}
		if ans.report.Malformed && reply.report.Malformed {
			// DAMAGE DISCARDS THE WHOLE LOAD (Glenn, 2026-10-31): a leg that
			// meets damage and hands back no root has given the answer the
			// rule asks for.
			return ""
		}
		if ans.report.Refused && reply.report == ans.report {
			// A FORM THE READER DOES NOT CARRY IS REFUSED (docs/SPEC-TABLES.md
			// §3): nothing was decoded, so a leg that refuses it by the same
			// verdict and hands back no root has answered it.
			return ""
		}
		return "the leg returned no root — its LoadMeasure sized a region its Load then refused"
	}
	if ans.measureRefused {
		return "the leg loaded a wire whose container framing requires refusal"
	}
	// DAMAGE DISCARDS THE WHOLE LOAD (Glenn, 2026-10-31: "failing to read
	// something means the whole read is undefined"): on a mutant the oracle
	// reads as damaged, the refusal is the whole of what a leg owes. The
	// counters, the retention report and the value the leg would save are
	// all undefined there, so none is compared; only the region bounds
	// below, which are the framing's and not the value's, still hold. A form
	// refusal is the same: nothing was decoded, so the verdict is compared
	// and the saved defaults are not.
	discarded := ans.report.Malformed || ans.report.Refused
	if ans.report.Malformed {
		if !reply.report.Malformed {
			return fmt.Sprintf("the oracle discards a damaged load (malformed); the leg says %s", reply.report)
		}
	} else if reply.report != ans.report {
		return fmt.Sprintf("the report differs: the leg says %s, the oracle says %s", reply.report, ans.report)
	}
	if !discarded && root.retain && (reply.retained != ans.retained || reply.retainLost != ans.retainLost) {
		// THE RETENTION REPORT (docs/SPEC-TABLES.md §6.6): `retained` counts the
		// fields whose bytes were kept and `retain_lost` every unknown the load
		// or the save could not keep. Two engines that keep different fields, or
		// that count a class differently, differ here before they differ in the
		// bytes.
		return fmt.Sprintf("the retention report differs: the leg says retained=%d retain_lost=%d, the oracle says retained=%d retain_lost=%d",
			reply.retained, reply.retainLost, ans.retained, ans.retainLost)
	}
	if !discarded && reply.saveFail != ans.encFail {
		if reply.saveFail {
			return fmt.Sprintf("the leg refused to save the value it loaded; the oracle encoded %d bytes", len(ans.encoded))
		}
		return fmt.Sprintf("the oracle refused to encode the value it decoded; the leg saved %d bytes", len(reply.saved))
	}
	if !discarded && !reply.saveFail && !bytes.Equal(reply.saved, ans.encoded) {
		at := 0
		for at < len(reply.saved) && at < len(ans.encoded) && reply.saved[at] == ans.encoded[at] {
			at++
		}
		return fmt.Sprintf("the decoded value differs: the leg saves %d bytes, the oracle %d, first difference at byte %d", len(reply.saved), len(ans.encoded), at)
	}
	if root.variable && !root.builder {
		if ans.exact && reply.measure != ans.bytes {
			return fmt.Sprintf("LoadMeasure asks for %d region bytes; the framing commands exactly %d", reply.measure, ans.bytes)
		}
		if reply.measure > ans.bytes {
			return fmt.Sprintf("LoadMeasure asks for %d region bytes; the framing can command at most %d", reply.measure, ans.bytes)
		}
		// THE AMPLIFICATION ROW (schema#466, docs/SPEC-TABLES.md §6.5): the
		// pinned maximum region-over-wire ratio, independent of the oracle's
		// per-mutant answer. A hostile record is two wire bytes and commands
		// at most the largest record the root can place, so a mutant above
		// this ceiling is a divergence on the bound the page states.
		if wireBytes > 0 && reply.measure > root.amplificationCeiling(wireBytes) {
			return fmt.Sprintf("LoadMeasure asks for %d region bytes from %d wire bytes; the amplification ceiling is %d",
				reply.measure, wireBytes, root.amplificationCeiling(wireBytes))
		}
	}
	return ""
}

// randomPass is the random pass (test/conformance/README.md, "The wire-fuzz
// driver"; docs/SPEC-TABLES.md §4.2). It draws from the manifest seeds alone,
// because a pinned vector is damaged bytes by construction and no basis for a
// mutation; keeping it out also keeps the sequence a function of the corpus
// and the seed alone, so pinning a vector never moves a red already there.
//
// An empty pool is a NAMED SKIP and not a panic: a leg (or a skip list) can
// leave only pinned-vector roots live, and there is then nothing to mutate.
// It answers the one line the run prints to stderr; no mutant is drawn.
func randomPass(pool []*wireSeed, pinned int, seed uint64, n int, draw func(*wireMutant)) string {
	if n <= 0 {
		return ""
	}
	if len(pool) == 0 {
		return fmt.Sprintf("wire-fuzz: random pass skipped: no manifest seed is live (%d pinned vectors only)", pinned)
	}
	for i := range n {
		// randomMutant refuses an empty list; this caller never sends nil.
		if mut := randomMutant(pool, seed, i); mut != nil {
			draw(mut)
		}
	}
	return ""
}

// buildWireFuzzRoster builds the wire-fuzz roster: every seed and every root
// the manifest names (docs/SPEC-TABLES.md §4.2, the wire fuzzer). It refuses
// by name when the roster is empty — a manifest, or a --manifest/--unit
// selection, that leaves no root with a seed — so the harness never fuzzes
// nothing and never PANICS on an empty seed set.
func buildWireFuzzRoster(m *Manifest, opts wireFuzzOptions) ([]*wireSeed, []*wireRoot, map[*wireSeed]int, int, error) {
	u := newUnits(m)

	var seeds []*wireSeed
	var roots []*wireRoot
	rootIndex := map[string]int{}
	addRoot := func(unit, root string, message bool) (int, error) {
		key := unit + "." + root
		if message {
			key += ".message"
		}
		if i, ok := rootIndex[key]; ok {
			return i, nil
		}
		r, err := newWireRoot(u, unit, root, message, false)
		if err != nil {
			return 0, err
		}
		if opts.builder {
			if !r.variable || message {
				rootIndex[key] = -1
				return -1, nil
			}
			r.builder = true
		}
		if opts.retain {
			// THE RETENTION ARM'S ROSTER IS THE VARIABLE-CLASS FILE ROOTS AND
			// NOTHING ELSE (docs/SPEC-TABLES.md §6.6, §3.3).
			if !r.variable || message {
				rootIndex[key] = -1
				return -1, nil
			}
			r.retain = true
		}
		rootIndex[key] = len(roots)
		roots = append(roots, r)
		return len(roots) - 1, nil
	}
	seedRoot := map[*wireSeed]int{}
	skipped := 0
	addFormSeed := func(name, unit, root, wirePath string, message bool) error {
		wire, err := os.ReadFile(wirePath)
		if err != nil {
			return err
		}
		ri, err := addRoot(unit, root, message)
		if err != nil {
			return err
		}
		if ri < 0 {
			skipped++
			return nil
		}
		s := &wireSeed{name: name, unit: unit, root: root, wire: wire, message: message}
		if message {
			// A MESSAGE SEED IS FRAMED IN BITS (docs/SPEC-TABLES.md §3.3)
			s.spots = tablewire.MessageSpots(roots[ri].model, roots[ri].def, wire, roots[ri].vocabulary)
			s.entries = len(roots[ri].vocabulary.Entries())
		} else {
			s.frame = frameWire(wire)
		}
		seeds = append(seeds, s)
		seedRoot[s] = ri
		return nil
	}
	addSeed := func(name, unit, root, wirePath string) error {
		return addFormSeed(name, unit, root, wirePath, false)
	}
	if opts.replay != "" {
		if opts.unit == "" || opts.root == "" {
			return nil, nil, nil, 0, fmt.Errorf("--replay needs --unit and --root")
		}
		if err := addFormSeed(filepath.Base(opts.replay), opts.unit, opts.root, opts.replay, opts.message); err != nil {
			return nil, nil, nil, 0, err
		}
	} else {
		for _, inst := range m.Instances {
			if err := addSeed(inst.Name, inst.Unit, inst.Root, inst.Wire); err != nil {
				return nil, nil, nil, 0, err
			}
		}
		for _, rc := range m.Reports {
			if err := addSeed(rc.Name, rc.Unit, rc.Root, rc.Wire); err != nil {
				return nil, nil, nil, 0, err
			}
		}
		// AND THE REFERENCE BOUND UNDER A CONNECTION TABLE
		// (docs/SPEC-TABLES.md §3.3).
		for _, msg := range m.Messages {
			if msg.Refused() {
				continue // a variable table: no message form to fuzz (§3.3)
			}
			c, cerr := m.LookupConnection(msg.Connection)
			if cerr != nil {
				return nil, nil, nil, 0, cerr
			}
			if err := addFormSeed(msg.Name+"_message", c.Unit, msg.Root, msg.MessageWire, true); err != nil {
				return nil, nil, nil, 0, err
			}
		}
		// THE PINNED VECTORS ride last and unmutated.
		vecs, err := readWireVectors(opts.vectors)
		if err != nil {
			return nil, nil, nil, 0, err
		}
		for _, v := range vecs {
			if err := addFormSeed(v.name, v.unit, v.root, v.file, v.message); err != nil {
				return nil, nil, nil, 0, err
			}
			seeds[len(seeds)-1].vector = true
		}
	}
	if len(seeds) == 0 {
		return nil, nil, nil, 0, fmt.Errorf("wire-fuzz: 0 seeds over %d roots: the roster is empty", len(roots))
	}
	return seeds, roots, seedRoot, skipped, nil
}

func wireFuzz(m *Manifest, opts wireFuzzOptions) error {
	if opts.builder && (opts.retain || opts.message) {
		return fmt.Errorf("--builder is a non-retaining file arm")
	}
	if opts.driver == "" {
		return fmt.Errorf("wire-fuzz needs --driver")
	}
	seeds, roots, seedRoot, skipped, err := buildWireFuzzRoster(m, opts)
	if err != nil {
		return err
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
	var pool []*wireSeed
	for _, s := range live {
		if !s.vector {
			pool = append(pool, s)
		}
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
				if s.vector {
					// a pinned vector IS the mutant. Its bytes are hostile
					// already, and mutating them would lose the exact input
					// the vector exists to replay
					produced <- item{m: &wireMutant{seed: s, pass: "vector", data: s.wire}}
					continue
				}
				pass := map[string]int{}
				enumerated(s, func(name string, data []byte) {
					produced <- item{m: &wireMutant{seed: s, pass: name, index: pass[name], data: data}}
					pass[name]++
				})
			}
			if line := randomPass(pool, len(live)-len(pool), opts.seed, opts.n, func(mut *wireMutant) {
				produced <- item{m: mut}
			}); line != "" {
				fmt.Fprintln(os.Stderr, line)
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
	owed := 0
	for mut := range sent {
		root := roots[seedRoot[mut.seed]]
		reply, err := leg.receiveWithDeadline(realClock{})
		if err != nil {
			leg.kill()
			return wireFailure(opts, mut, total, fmt.Sprintf("the leg died on the mutant (%v)", err), leg.stderr.String())
		}
		ans, err := root.oracle(mut.data)
		if err != nil {
			leg.kill()
			return wireFailure(opts, mut, total, err.Error(), "")
		}
		if ans.nonFinite && opts.finiteOwed != "" {
			// the leg still carries the pattern: owed by its finite-tables
			// card, counted, and not compared (docs/SPEC-TABLES.md §3.3)
			owed++
			total++
			if mut.pass != "random" {
				enumeratedCount++
			}
			continue
		}
		if verdict := wireVerdict(root, reply, ans, len(mut.data)); verdict != "" {
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
			if root.variable && !root.builder {
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
		form := "file"
		if opts.message {
			form = "message"
		}
		fmt.Printf("wire-fuzz: replay of %s over %s.%s as a %s, %d mutant, 0 divergences, %.1f s\n",
			opts.replay, opts.unit, opts.root, form, total, elapsed.Seconds())
		return nil
	}
	rate := float64(total) / elapsed.Seconds()
	absence := ""
	if absent > 0 {
		absence = fmt.Sprintf(", %d seeds absent (roots the leg has no codec for)", absent)
	}
	if skipped > 0 {
		// the seeds this ARM has nothing to say about: a fixed-class root gets
		// no retention and a form-2 SaveRetain refuses by name (§6.6, §3.3)
		absence += fmt.Sprintf(", %d seeds outside the arm", skipped)
	}
	if opts.finiteOwed != "" {
		absence += fmt.Sprintf(", %d non-finite mutants not compared (the finite float rule is owed by %s)", owed, opts.finiteOwed)
	}
	arm := "wire-fuzz"
	if opts.builder {
		arm = "wire-fuzz-builder"
	}
	if opts.retain {
		arm = "wire-fuzz retain"
	}
	fmt.Printf("%s: seed %d, %d seeds over %d roots%s, %d enumerated + %d random = %d mutants, 0 divergences, %.1f s, %.0f mutants/s\n",
		arm, opts.seed, len(live), len(roots), absence, enumeratedCount, total-enumeratedCount, total, elapsed.Seconds(), rate)
	return nil
}

func describeBytes(b []byte, refused bool) string {
	if refused {
		return "refused"
	}
	return fmt.Sprintf("%d bytes", len(b))
}

// mutantHexLimit is how much of a mutant rides in the failure message itself.
// A CI log is the only copy of a red a person outside the run ever sees, so the
// bytes go in it. A corpus wire reaches 200 KB and a log line is not a file, so
// past the limit the digest and the run seed carry the reproduction instead.
// The whole mutant is on disk at --failed either way.
const mutantHexLimit = 4096

// wireFailure writes the mutant where a person can pick it up, prints what
// reproduces it, and prints the one command that replays it alone.
func wireFailure(opts wireFuzzOptions, mut *wireMutant, passed int, verdict, detail string) error {
	if err := os.MkdirAll(filepath.Dir(opts.failed), 0o755); err == nil {
		_ = os.WriteFile(opts.failed, mut.data, 0o644)
	}
	// THE BYTES AND THE RUN SEED RIDE IN THE MESSAGE. Certification runs on
	// hardware nobody here owns, so a red whose only record is a file under
	// build/ is a red nobody can reproduce from the log, and a run whose seed
	// is not stated is a search rather than a seek. Both go here, on every
	// failure.
	sum := sha256.Sum256(mut.data)
	var bytesLine string
	if len(mut.data) <= mutantHexLimit {
		bytesLine = fmt.Sprintf("\n  mutant bytes (sha256 %x), restore with `xxd -r -p`:\n  %x", sum, mut.data)
	} else {
		bytesLine = fmt.Sprintf("\n  mutant sha256 %x, too large to print: reproduce with --seed %d and read %s",
			sum, opts.seed, opts.failed)
	}
	// a MESSAGE seed's mutant is a form-2 wire, and a replay that read it as a
	// file would read another wire entirely (docs/SPEC-TABLES.md §3.3)
	form := ""
	if opts.builder {
		form = " --builder"
	}
	if opts.retain {
		form += " --retain"
	}
	if mut.seed.message {
		form = " --message"
	}
	msg := fmt.Sprintf("FAILED after %d mutants: %s\n  corpus seed %s (%s.%s), pass %s #%d, run seed %d, mutant of %d bytes written to %s%s\n%s\n  replay: %s wire-fuzz --driver %q --replay %s --unit %s --root %s%s",
		passed, verdict, mut.seed.name, mut.seed.unit, mut.seed.root, mut.pass, mut.index, opts.seed, len(mut.data), opts.failed,
		bytesLine, strings.TrimRight(detail, "\n"), os.Args[0], opts.driver, opts.failed, mut.seed.unit, mut.seed.root, form)
	return fmt.Errorf("%s", msg)
}

// wireVector is one pinned red, as the index names it.
type wireVector struct {
	// message marks a vector that is a MESSAGE rather than a file
	// (docs/SPEC-TABLES.md §3.3): its references resolve against the unit's
	// announced table, so the form is part of what the index has to say.
	message bool
	name    string
	unit    string
	root    string
	file    string
}

// readWireVectors reads the pinned-vector index. An ABSENT index is an empty
// corpus and not an error, because the vectors are a gate that grows and a tree
// that has not needed one yet still fuzzes.
func readWireVectors(path string) ([]wireVector, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []wireVector
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		message := false
		switch {
		case len(f) == 4:
		case len(f) == 5 && f[4] == "message":
			message = true
		default:
			return nil, fmt.Errorf("%s:%d: a vector is <name> <unit> <root> <file> [message]", path, i+1)
		}
		out = append(out, wireVector{name: f[0], unit: f[1], root: f[2], file: f[3], message: message})
	}
	return out, nil
}
