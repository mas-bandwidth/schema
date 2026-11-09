// `harness wire-fuzz` checks the single read contract (docs/SPEC-TABLES.md §4.2).
// Every mutant must produce the same acceptance verdict in the leg and oracle.
// Accepted values must save back to identical canonical bytes. Refusal reasons,
// counters, retention counts and allocation measurements are local diagnostics.
// Driver timeouts and caller resource limits are infrastructure checks, outside
// the read verdict.
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
	// retain compares the retaining read's accepted canonical write-back.
	retain  bool
	builder bool // mutable file reads have no region preflight
	// mixedBuilder preserves the full roster and uses the mutable oracle only
	// for variable-class file roots; fixed roots and messages keep their oracle.
	mixedBuilder bool
	failed       string
	vectors      string
	// Kept for command-line compatibility so a retired exemption fails clearly.
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
}

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
	r := &wireRoot{unit: unitKey, root: rootName, model: m, def: def, variable: m.IsVariable(rootName), message: message, retain: retain}
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
	return r, nil
}

// legReply is what a leg answers for one mutant (test/conformance/README.md,
// "The wire-fuzz driver").
// Counts holds diagnostic counters on the legacy wire-fuzz pipe protocol.
type Counts struct {
	Unknown      int
	KindMismatch int
	Widened      int
	Clamped      int
	Duplicate    int
	Malformed    bool
	Refused      bool
}

func (c Counts) String() string {
	verdict := "read"
	if c.Refused {
		verdict = "refused"
	}
	return fmt.Sprintf("%d,%d,%d,%d,%d,%t,%s",
		c.Unknown, c.KindMismatch, c.Widened, c.Clamped, c.Duplicate, c.Malformed, verdict)
}

// ParseCounts parses the legacy 7-token report line.
func ParseCounts(text string) (Counts, error) {
	var c Counts
	parts := strings.Split(strings.TrimSpace(text), ",")
	if len(parts) != 7 {
		return c, fmt.Errorf("%q is not 7 comma-separated fields", text)
	}
	nums := []*int{&c.Unknown, &c.KindMismatch, &c.Widened, &c.Clamped, &c.Duplicate}
	for i, dst := range nums {
		var n int
		if _, err := fmt.Sscanf(parts[i], "%d", &n); err != nil || fmt.Sprintf("%d", n) != parts[i] {
			return c, fmt.Errorf("field %d (%q) is not an integer", i+1, parts[i])
		}
		*dst = n
	}
	switch parts[5] {
	case "true":
		c.Malformed = true
	case "false":
		c.Malformed = false
	default:
		return c, fmt.Errorf("field 6 (%q) is not true or false", parts[5])
	}
	switch parts[6] {
	case "read":
		c.Refused = false
	case "refused":
		c.Refused = true
	default:
		return c, fmt.Errorf("field 7 (%q) is not read or refused", parts[6])
	}
	return c, nil
}

type legReply struct {
	loaded   bool
	report   Counts
	measure  int64
	saved    []byte
	saveFail bool
	// Legacy saved-length -2, retained only as diagnostic transport.
	blobShared bool
	// Legacy diagnostic transport, never part of the comparison.
	retained   int
	retainLost int
}

// oracleAnswer is what the engine says the same bytes mean.
type oracleAnswer struct {
	accepted bool
	report   Counts // diagnostics only
	encoded  []byte
	encFail  bool
}

func (r *wireRoot) oracle(data []byte) (ans oracleAnswer, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the oracle PANICKED on the mutant: %v\n%s", p, debug.Stack())
		}
	}()
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
		if r.builder {
			// the BUILDER ARM retains too: a builder load has no region, so it
			// is the plain retaining read, with the builder's count refusal
			decode = func() (bool, error) { return tablewire.DecodeRetain(r.model, inst, data, &retain, &rep) }
		}
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
		// wire's M, so both the read and canonical write-back cover every body
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
		encode = func() ([]byte, error) { return tablewire.EncodeMessages(r.model, batch) }
	}
	// Region decode on fixed or variable checks framing first.
	if r.variable && !r.message && !r.builder {
		if _, _, refused := tablewire.FileRegionMeasure(r.model.Unit, r.def, data); refused {
			ans.report = Counts{Refused: true}
			return ans, nil
		}
	}
	ok, derr := decode()
	strictOK := strictReportClean(&rep) && (r.message || strictWireCheckForRoot(r.def, data))
	if !strictOK || derr != nil || !ok {
		rep.Refused = true
	}
	ans.report = Counts{Unknown: rep.Unknown, Widened: rep.Widened, Refused: rep.Refused}
	if derr != nil {
		return ans, nil
	}
	ans.accepted = ok && strictOK
	if !ans.accepted {
		return ans, nil
	}
	ans.encoded, err = encode()
	if err != nil {
		return ans, fmt.Errorf("the oracle could not encode an accepted value: %w", err)
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

// accepted collapses the legacy pipe flags into the single read verdict.
// Repair counters must never turn an accepted result into a refusal.
func (reply legReply) accepted() bool {
	return reply.loaded && !reply.report.Refused && !reply.report.Malformed
}

// wireVerdict compares only acceptance and the complete accepted write-back.
// The root and wire size remain parameters while callers migrate the transport.
func wireVerdict(_ *wireRoot, reply legReply, ans oracleAnswer, _ int) string {
	if accepted := reply.accepted(); accepted != ans.accepted {
		return fmt.Sprintf("read acceptance differs: the leg says %t, the oracle says %t", accepted, ans.accepted)
	}
	if !ans.accepted {
		return ""
	}
	if ans.encFail {
		return "the oracle could not encode its accepted value"
	}
	if reply.saveFail {
		return fmt.Sprintf("the leg refused to save its accepted value; the oracle encoded %d bytes", len(ans.encoded))
	}
	if !bytes.Equal(reply.saved, ans.encoded) {
		at := 0
		for at < len(reply.saved) && at < len(ans.encoded) && reply.saved[at] == ans.encoded[at] {
			at++
		}
		return fmt.Sprintf("the decoded value differs: the leg saves %d bytes, the oracle %d, first difference at byte %d", len(reply.saved), len(ans.encoded), at)
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
		if opts.mixedBuilder && r.variable && !message {
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
			before := len(seeds)
			if err := addFormSeed(v.name, v.unit, v.root, v.file, v.message); err != nil {
				return nil, nil, nil, 0, err
			}
			if len(seeds) > before {
				seeds[len(seeds)-1].vector = true
			}
		}
	}
	if len(seeds) == 0 {
		return nil, nil, nil, 0, fmt.Errorf("wire-fuzz: 0 seeds over %d roots: the roster is empty", len(roots))
	}
	return seeds, roots, seedRoot, skipped, nil
}

func wireFuzz(m *Manifest, opts wireFuzzOptions) error {
	if opts.finiteOwed != "" {
		return fmt.Errorf("--finite-owed is retired: every mutant must be compared")
	}
	if opts.builder && opts.mixedBuilder {
		return fmt.Errorf("--builder and --mixed-builder are mutually exclusive profiles")
	}
	if opts.builder && opts.message {
		return fmt.Errorf("--builder is a file arm: the message form has no builder load")
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
		if verdict := wireVerdict(root, reply, ans, len(mut.data)); verdict != "" {
			leg.kill()
			// THE TWO ANSWERS BESIDE THE MUTANT: a divergence in the decoded
			// VALUE is a diff, and a diff needs both sides on disk
			if dir := filepath.Dir(opts.failed); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
				_ = os.WriteFile(filepath.Join(dir, "leg.bin"), reply.saved, 0o644)
				_ = os.WriteFile(filepath.Join(dir, "oracle.bin"), ans.encoded, 0o644)
			}
			detail := fmt.Sprintf("  leg:    accepted=%t report=%s measure=%d saved=%s\n  oracle: accepted=%t report=%s encoded=%s",
				reply.accepted(), reply.report, reply.measure, describeBytes(reply.saved, reply.saveFail),
				ans.accepted, ans.report, describeBytes(ans.encoded, ans.encFail))
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
	arm := "wire-fuzz"
	if opts.builder {
		arm = "wire-fuzz-builder"
	}
	if opts.mixedBuilder {
		arm = "wire-fuzz-mixed-builder"
	}
	if opts.retain {
		arm += " retain"
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
	if opts.mixedBuilder {
		form = " --mixed-builder"
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
