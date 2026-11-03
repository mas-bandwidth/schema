// Command wirefuzzdispatch is the Goose leg's ONE wire-fuzz driver over the
// whole roster (test/conformance/README.md, "The wire-fuzz driver";
// test/conformance/nightly-readiness). Every generated Goose unit carries its
// own copy of the runtime, so two units cannot be one Goose program and the
// leg is one executable per unit (make/goose-wirefuzz-build.sh,
// test/conformance/goose/wirefuzz.goose). The harness runs one --driver over
// the full roster; this dispatcher stands in front of the per-unit
// executables and speaks the harness's protocol on both sides.
//
//	wirefuzzdispatch <legs dir>
//
// <legs dir>/<unit>/wirefuzz is the unit's built leg. For every unit the
// roster names and the directory holds, the dispatcher starts the leg twice,
// once per arm it drives (`builder`, the variable-class file roots; `fixed`,
// the value-only roots and their message form), and hands each the roster
// unchanged, so a roster index means the same entry on both sides and no
// mutant is renumbered. An entry is answered 1 when exactly one child
// answers 1 for it, and every mutant for that entry is routed to that child.
// Two children claiming one entry is a refusal. A unit with no built leg
// answers 0 for its entries, which the harness counts as seeds absent: an
// absence is said, never hidden.
//
// The harness pipelines mutants ahead of their replies, so the replies are
// read back in the order the mutants were routed: one goroutine routes, the
// other relays each reply from the child that owes it.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// replyHead is a reply's fixed part: loaded, the five counters, malformed,
// refused, the measure, retained, retain_lost and the saved length.
const replyHead = 1 + 5*4 + 1 + 1 + 8 + 4 + 4 + 8

// arms are the two modes of wirefuzz.goose, which split the roster between
// them by the root's class.
var arms = []string{"builder", "fixed"}

// child is one running leg: its input, its output and how to reap it.
type child struct {
	name string
	in   io.WriteCloser
	out  *bufio.Reader
	wait func() error
	kill func()
}

// starter starts the leg for one unit and arm, or answers nil, nil when the
// unit has no built leg.
type starter func(unit, arm string) (*child, error)

// entry is one roster entry, by the unit it names.
type entry struct {
	unit string
}

// readRoster reads the roster the harness writes first and answers its
// entries and its raw bytes, which each child is handed unchanged.
func readRoster(in io.Reader) ([]entry, []byte, error) {
	var raw []byte
	take := func(n int) ([]byte, error) {
		b := make([]byte, n)
		if _, err := io.ReadFull(in, b); err != nil {
			return nil, fmt.Errorf("the roster ended early: %w", err)
		}
		raw = append(raw, b...)
		return b, nil
	}
	head, err := take(4)
	if err != nil {
		return nil, nil, err
	}
	count := binary.LittleEndian.Uint32(head)
	entries := make([]entry, 0, count)
	for range count {
		kn, err := take(2)
		if err != nil {
			return nil, nil, err
		}
		unit, err := take(int(binary.LittleEndian.Uint16(kn)))
		if err != nil {
			return nil, nil, err
		}
		rn, err := take(2)
		if err != nil {
			return nil, nil, err
		}
		if _, err := take(int(binary.LittleEndian.Uint16(rn))); err != nil {
			return nil, nil, err
		}
		// the form byte and the retain byte
		if _, err := take(2); err != nil {
			return nil, nil, err
		}
		entries = append(entries, entry{unit: string(unit)})
	}
	return entries, raw, nil
}

// dispatch runs the whole conversation: the roster, then every mutant until
// the harness closes its input.
func dispatch(in io.Reader, out io.Writer, start starter) (err error) {
	entries, raw, err := readRoster(in)
	if err != nil {
		return err
	}
	var children []*child
	defer func() {
		for _, c := range children {
			if c.in != nil {
				_ = c.in.Close()
			}
			if err != nil && c.kill != nil {
				// a failed conversation leaves children mid-reply: end them
				c.kill()
			}
		}
		for _, c := range children {
			if werr := c.wait(); werr != nil && err == nil {
				err = fmt.Errorf("%s: %w", c.name, werr)
			}
		}
	}()
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.unit] {
			continue
		}
		seen[e.unit] = true
		for _, arm := range arms {
			c, serr := start(e.unit, arm)
			if serr != nil {
				return serr
			}
			if c != nil {
				children = append(children, c)
			}
		}
	}

	// every child reads the whole roster before it answers any of it
	var wg sync.WaitGroup
	sendErr := make([]error, len(children))
	for i, c := range children {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, sendErr[i] = c.in.Write(raw)
		}()
	}
	answers := make([][]byte, len(children))
	readErr := make([]error, len(children))
	for i, c := range children {
		answers[i] = make([]byte, len(entries))
		if _, rerr := io.ReadFull(c.out, answers[i]); rerr != nil {
			readErr[i] = rerr
		}
	}
	wg.Wait()
	for i, c := range children {
		if sendErr[i] != nil || readErr[i] != nil {
			return fmt.Errorf("%s did not answer the roster: %v", c.name, errors.Join(sendErr[i], readErr[i]))
		}
	}
	route := make([]*child, len(entries))
	reply := make([]byte, len(entries))
	for i := range entries {
		for j, c := range children {
			if answers[j][i] == 0 {
				continue
			}
			if route[i] != nil {
				return fmt.Errorf("roster entry %d is claimed by both %s and %s", i, route[i].name, c.name)
			}
			route[i] = c
			reply[i] = 1
		}
	}
	// a child that drives no entry is released at once
	live := children[:0]
	for _, c := range children {
		used := false
		for _, r := range route {
			used = used || r == c
		}
		if used {
			live = append(live, c)
			continue
		}
		_ = c.in.Close()
		c.in = nil
		if werr := c.wait(); werr != nil {
			return fmt.Errorf("%s: %w", c.name, werr)
		}
	}
	children = live

	w := bufio.NewWriterSize(out, 1<<20)
	if _, err := w.Write(reply); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}

	// THE MUTANTS: route each to its child; relay the replies in order
	pending := make(chan *child, 1024)
	relayed := make(chan error, 1)
	go func() {
		relayed <- relay(pending, w)
	}()
	routed := make(chan error, 1)
	go func() {
		routed <- routeMutants(in, route, pending)
	}()
	if rerr := <-relayed; rerr != nil {
		return rerr
	}
	return <-routed
}

// routeMutants reads each mutant from the harness and hands it to the child its
// roster entry names, queueing that child for the relay.
func routeMutants(in io.Reader, route []*child, pending chan<- *child) error {
	defer close(pending)
	var head [8]byte
	for {
		if _, rerr := io.ReadFull(in, head[:]); rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return fmt.Errorf("the stream ended inside a frame header: %w", rerr)
		}
		index := binary.LittleEndian.Uint32(head[0:])
		size := binary.LittleEndian.Uint32(head[4:])
		if uint64(index) >= uint64(len(route)) || route[index] == nil {
			return fmt.Errorf("a mutant for roster entry %d, which this leg answered 0", index)
		}
		data := make([]byte, size)
		if _, rerr := io.ReadFull(in, data); rerr != nil {
			return fmt.Errorf("the stream ended inside a mutant: %w", rerr)
		}
		c := route[index]
		if _, werr := c.in.Write(append(head[:], data...)); werr != nil {
			return fmt.Errorf("%s closed its input: %w", c.name, werr)
		}
		pending <- c
	}
}

// relay copies each routed mutant's reply from the child that owes it, in
// routing order, flushing whenever no further reply is already owed.
func relay(pending <-chan *child, w *bufio.Writer) error {
	head := make([]byte, replyHead)
	for c := range pending {
		if _, err := io.ReadFull(c.out, head); err != nil {
			return fmt.Errorf("%s did not reply: %w", c.name, err)
		}
		if _, err := w.Write(head); err != nil {
			return err
		}
		n := int64(binary.LittleEndian.Uint64(head[replyHead-8:]))
		if n > 0 {
			if _, err := io.CopyN(w, c.out, n); err != nil {
				return fmt.Errorf("%s replied short: %w", c.name, err)
			}
		}
		if len(pending) == 0 {
			if err := w.Flush(); err != nil {
				return err
			}
		}
	}
	return w.Flush()
}

// stderr is the dispatcher's own stderr, shared by every child's copier.
type stderr struct{ mu sync.Mutex }

func (s *stderr) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Stderr.Write(b)
}

// execStarter starts <legs>/<unit>/wirefuzz <unit> <arm>. A child's stderr
// is copied into the dispatcher's own, so the harness keeps every child's
// diagnostics, yet no child holds the harness's stderr pipe: when the harness
// kills this dispatcher on a stalled reply, its wait returns even while a
// child is still spinning (and on Linux the child dies with the dispatcher,
// procattr_linux.go).
func execStarter(legs string) starter {
	var errs stderr
	return func(unit, arm string) (*child, error) {
		path := filepath.Join(legs, unit, "wirefuzz")
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return nil, nil
		}
		cmd := exec.Command(path, unit, arm)
		cmd.Stderr = &errs
		cmd.SysProcAttr = childProcAttr()
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("starting %s: %w", path, err)
		}
		return &child{name: unit + " " + arm, in: in, out: bufio.NewReaderSize(out, 1<<20), wait: cmd.Wait,
			kill: func() { _ = cmd.Process.Kill() }}, nil
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: wirefuzzdispatch <legs dir>")
		os.Exit(2)
	}
	if err := dispatch(os.Stdin, os.Stdout, execStarter(os.Args[1])); err != nil {
		fmt.Fprintln(os.Stderr, "wirefuzzdispatch:", err)
		os.Exit(1)
	}
}
