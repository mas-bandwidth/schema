package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

type rosterEntry struct {
	unit, root   string
	form, retain byte
}

func encodeRoster(entries []rosterEntry) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(entries)))
	for _, e := range entries {
		_ = binary.Write(&b, binary.LittleEndian, uint16(len(e.unit)))
		b.WriteString(e.unit)
		_ = binary.Write(&b, binary.LittleEndian, uint16(len(e.root)))
		b.WriteString(e.root)
		b.WriteByte(e.form)
		b.WriteByte(e.retain)
	}
	return b.Bytes()
}

func decodeRoster(t *testing.T, in io.Reader) []rosterEntry {
	var count uint32
	if err := binary.Read(in, binary.LittleEndian, &count); err != nil {
		t.Error(err)
		return nil
	}
	text := func() string {
		var n uint16
		_ = binary.Read(in, binary.LittleEndian, &n)
		b := make([]byte, n)
		_, _ = io.ReadFull(in, b)
		return string(b)
	}
	entries := make([]rosterEntry, count)
	for i := range entries {
		entries[i].unit = text()
		entries[i].root = text()
		var fr [2]byte
		_, _ = io.ReadFull(in, fr[:])
		entries[i].form, entries[i].retain = fr[0], fr[1]
	}
	return entries
}

func mutant(index int, data string) []byte {
	var head [8]byte
	binary.LittleEndian.PutUint32(head[0:], uint32(index))
	binary.LittleEndian.PutUint32(head[4:], uint32(len(data)))
	return append(head[:], data...)
}

// fakeLeg is wirefuzz.goose's contract in miniature: it reads the whole
// roster, answers 1 for the entries of its unit whose root its arm claims,
// and replies to each mutant with a saved value naming itself and the bytes.
// Its pipes are unbuffered, so a dispatcher that read replies out of order or
// waited on the wrong child would deadlock rather than pass.
func fakeLeg(t *testing.T, unit, arm string, claims func(rosterEntry) bool) *child {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		defer outW.Close()
		in := bufio.NewReader(inR)
		entries := decodeRoster(t, in)
		answer := make([]byte, len(entries))
		for i, e := range entries {
			if e.unit == unit && claims(e) {
				answer[i] = 1
			}
		}
		if _, err := outW.Write(answer); err != nil {
			done <- err
			return
		}
		for {
			var head [8]byte
			if _, err := io.ReadFull(in, head[:]); err != nil {
				done <- nil
				return
			}
			data := make([]byte, binary.LittleEndian.Uint32(head[4:]))
			_, _ = io.ReadFull(in, data)
			index := binary.LittleEndian.Uint32(head[0:])
			if answer[index] == 0 {
				t.Errorf("%s %s: a mutant for entry %d it answered 0", unit, arm, index)
			}
			saved := []byte(unit + "/" + arm + ":" + string(data))
			reply := make([]byte, replyHead)
			reply[0] = 1
			binary.LittleEndian.PutUint64(reply[replyHead-8:], uint64(len(saved)))
			_, _ = outW.Write(append(reply, saved...))
		}
	}()
	return &child{name: unit + " " + arm, in: inW, out: bufio.NewReader(outR),
		wait: func() error { return <-done }, kill: func() { _ = inR.Close(); _ = outW.Close() }}
}

// replies reads n replies from the dispatcher's output, after the roster
// answer of count bytes, and answers the answer and each saved value.
func replies(t *testing.T, out []byte, count, n int) ([]byte, []string) {
	if len(out) < count {
		t.Fatalf("output %d bytes, roster answer is %d", len(out), count)
	}
	answer, rest := out[:count], out[count:]
	var saved []string
	for range n {
		if len(rest) < replyHead {
			t.Fatalf("a reply is short: %d bytes left", len(rest))
		}
		size := int(binary.LittleEndian.Uint64(rest[replyHead-8 : replyHead]))
		saved = append(saved, string(rest[replyHead:replyHead+size]))
		rest = rest[replyHead+size:]
	}
	if len(rest) != 0 {
		t.Fatalf("%d bytes after the last reply", len(rest))
	}
	return answer, saved
}

func variable(e rosterEntry) bool { return strings.HasPrefix(e.root, "V") }

func legs(t *testing.T, built map[string]bool) (starter, *[]string) {
	var started []string
	return func(unit, arm string) (*child, error) {
		if !built[unit] {
			return nil, nil
		}
		started = append(started, unit+" "+arm)
		if arm == "builder" {
			return fakeLeg(t, unit, arm, variable), nil
		}
		return fakeLeg(t, unit, arm, func(e rosterEntry) bool { return !variable(e) }), nil
	}, &started
}

func TestDispatchRoutesEveryEntryToItsUnitAndArmInOrder(t *testing.T) {
	entries := []rosterEntry{
		{"a", "VList", 1, 0}, {"b", "Fixed", 1, 0}, {"a", "Fixed", 2, 0},
		{"b", "VMap", 1, 1}, {"c", "VList", 1, 0},
	}
	var in bytes.Buffer
	in.Write(encodeRoster(entries))
	// interleaved across units and arms, more than one per child in a row
	order := []int{0, 1, 2, 3, 3, 0, 2, 1, 0}
	for i, idx := range order {
		in.Write(mutant(idx, string(rune('A'+i))))
	}
	start, started := legs(t, map[string]bool{"a": true, "b": true})
	var out bytes.Buffer
	if err := dispatch(&in, &out, start); err != nil {
		t.Fatal(err)
	}
	answer, saved := replies(t, out.Bytes(), len(entries), len(order))
	if want := []byte{1, 1, 1, 1, 0}; !bytes.Equal(answer, want) {
		t.Errorf("roster answer %v, want %v (unit c has no leg: absent)", answer, want)
	}
	owner := []string{"a/builder", "b/fixed", "a/fixed", "b/builder", ""}
	for i, idx := range order {
		if want := owner[idx] + ":" + string(rune('A'+i)); saved[i] != want {
			t.Errorf("reply %d is %q, want %q", i, saved[i], want)
		}
	}
	if got := strings.Join(*started, ","); got != "a builder,a fixed,b builder,b fixed" {
		t.Errorf("started %s", got)
	}
}

func TestDispatchRefusesAnEntryTwoChildrenClaim(t *testing.T) {
	var in bytes.Buffer
	in.Write(encodeRoster([]rosterEntry{{"a", "VList", 1, 0}}))
	start := func(unit, arm string) (*child, error) {
		return fakeLeg(t, unit, arm, func(rosterEntry) bool { return true }), nil
	}
	err := dispatch(&in, io.Discard, start)
	if err == nil || !strings.Contains(err.Error(), "claimed by both") {
		t.Fatalf("got %v, want a refusal naming both claimants", err)
	}
}

func TestDispatchRefusesAMutantForAnAbsentEntry(t *testing.T) {
	var in bytes.Buffer
	in.Write(encodeRoster([]rosterEntry{{"a", "VList", 1, 0}, {"z", "VList", 1, 0}}))
	in.Write(mutant(1, "x"))
	start, _ := legs(t, map[string]bool{"a": true})
	err := dispatch(&in, io.Discard, start)
	if err == nil || !strings.Contains(err.Error(), "answered 0") {
		t.Fatalf("got %v, want a refusal of the mutant", err)
	}
}

func TestDispatchRefusesATruncatedRoster(t *testing.T) {
	raw := encodeRoster([]rosterEntry{{"a", "VList", 1, 0}})
	start, _ := legs(t, map[string]bool{"a": true})
	if err := dispatch(bytes.NewReader(raw[:len(raw)-1]), io.Discard, start); err == nil {
		t.Fatal("a truncated roster was accepted")
	}
}
