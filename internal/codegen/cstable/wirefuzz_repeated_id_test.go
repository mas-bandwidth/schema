package cstable

// A REPEATED FIELD ID IS DAMAGE (docs/SPEC-TABLES.md §6.5): the wire-alone
// walk a form-1 read runs before it decodes anything holds one id per body.
// The C# wire fuzzer stopped on this rule before the walk landed, and the
// region arm in particular reads through LoadMeasure, so the rule has to hold
// in the emitted WireWalk.Body and in its KeyedBody.
//
// These are unit tests over the REAL emitted artifact: Generate is run over a
// genuine pointer table and the runtime it produced is inspected. They take no
// interpreter, socket or network. The companion negative control plants the
// pre-fix fault (the duplicate refusal taken out, so the later occurrence
// wins) in that same emitted artifact and requires this check to go red.

import (
	"fmt"
	"strings"
	"testing"
)

// csWireFuzzRepeatedIdSchema is a table with a pointer edge, so the emitted
// unit carries the variable-class runtime the wire fuzzer reads.
const csWireFuzzRepeatedIdSchema = `package repeatedid

table Node
{
    value int32
    name  string(12)
    next  *Node
}
`

// The two emitted duplicate refusals. Body holds field references; KeyedBody
// holds the key references of an enum-keyed array. Both are damage.
const (
	csWireFuzzBodyRepeat    = "if ((seen[r >> 6] & (1ul << (r & 63))) != 0) { return Damage(); }"
	csWireFuzzBodySeen      = "seen[r >> 6] |= 1ul << (r & 63);"
	csWireFuzzKeyedRepeat   = "if ((seen[k >> 6] & (1ul << (k & 63))) != 0) { return Damage(); }"
	csWireFuzzKeyedSeen     = "seen[k >> 6] |= 1ul << (k & 63);"
	csWireFuzzSeenAlloc     = "Span<ulong> seen = stackalloc ulong[Words];"
	csWireFuzzBodySignature = "bool Body(ReadOnlySpan<byte> buf, bool root)"
	csWireFuzzBodyEnd       = "bool Payload(ReadOnlySpan<byte> buf, ref int off, byte kind)"
	csWireFuzzKeyedSig      = "bool KeyedBody(ReadOnlySpan<byte> b)"
	csWireFuzzKeyedEnd      = "bool Union(ReadOnlySpan<byte> buf, ref int off)"
)

// csWireFuzzSlice is the emitted text between two signatures. The signatures
// bound a method because the runtime emits one method after another.
func csWireFuzzSlice(src, start, end string) (string, error) {
	i := strings.Index(src, start)
	if i < 0 {
		return "", fmt.Errorf("the emitted runtime has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		return "", fmt.Errorf("the emitted runtime has no %q after %q", end, start)
	}
	return rest[:j], nil
}

// csWireFuzzRepeatedIdRule holds the emitted wire-alone walk to the rule: a
// reference already in the body's seen bitset is damage, and the bitset is
// recorded for the next occurrence, in the root/nested body and in a keyed
// body's keys.
func csWireFuzzRepeatedIdRule(files map[string][]byte) error {
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	src := b.String()
	if n := strings.Count(src, "ref struct WireWalk"); n != 1 {
		return fmt.Errorf("the emitted artifact declares ref struct WireWalk %d times, want once", n)
	}
	body, err := csWireFuzzSlice(src, csWireFuzzBodySignature, csWireFuzzBodyEnd)
	if err != nil {
		return err
	}
	if !strings.Contains(body, csWireFuzzSeenAlloc) {
		return fmt.Errorf("WireWalk.Body allocates no seen bitset (%q)", csWireFuzzSeenAlloc)
	}
	if !strings.Contains(body, csWireFuzzBodyRepeat) {
		return fmt.Errorf("WireWalk.Body does not refuse a repeated field reference (%q)", csWireFuzzBodyRepeat)
	}
	if !strings.Contains(body, csWireFuzzBodySeen) {
		return fmt.Errorf("WireWalk.Body does not record a field reference in its seen bitset (%q)", csWireFuzzBodySeen)
	}
	if strings.Index(body, csWireFuzzBodyRepeat) > strings.Index(body, csWireFuzzBodySeen) {
		return fmt.Errorf("WireWalk.Body records a field reference before testing it, so a repeat cannot be damage")
	}
	keyed, err := csWireFuzzSlice(src, csWireFuzzKeyedSig, csWireFuzzKeyedEnd)
	if err != nil {
		return err
	}
	if !strings.Contains(keyed, csWireFuzzSeenAlloc) {
		return fmt.Errorf("WireWalk.KeyedBody allocates no seen bitset (%q)", csWireFuzzSeenAlloc)
	}
	if !strings.Contains(keyed, csWireFuzzKeyedRepeat) {
		return fmt.Errorf("WireWalk.KeyedBody does not refuse a repeated keyed slot (%q)", csWireFuzzKeyedRepeat)
	}
	if !strings.Contains(keyed, csWireFuzzKeyedSeen) {
		return fmt.Errorf("WireWalk.KeyedBody does not record a keyed slot in its seen bitset (%q)", csWireFuzzKeyedSeen)
	}
	if strings.Index(keyed, csWireFuzzKeyedRepeat) > strings.Index(keyed, csWireFuzzKeyedSeen) {
		return fmt.Errorf("WireWalk.KeyedBody records a keyed slot before testing it, so a repeat cannot be damage")
	}
	return nil
}

// TestCSharpWireFuzzRepeatedFieldId: the emitted C# runtime's wire-alone walk
// refuses a repeated field id and a repeated keyed slot.
func TestCSharpWireFuzzRepeatedFieldId(t *testing.T) {
	t.Parallel()
	files := generateFiles(t, "Probe", csWireFuzzRepeatedIdSchema)
	if err := csWireFuzzRepeatedIdRule(files); err != nil {
		t.Fatalf("cs-wire-fuzz-repeated-id: %v", err)
	}
}

// TestCSharpWireFuzzRepeatedFieldIdNegativeControl plants the pre-fix fault in
// the REAL artifact: the two duplicate refusals are taken out of the emitted
// runtime, which is exactly the old last-wins read the fuzzer stopped on. The
// check must go red on that artifact.
func TestCSharpWireFuzzRepeatedFieldIdNegativeControl(t *testing.T) {
	t.Parallel()
	files := generateFiles(t, "Probe", csWireFuzzRepeatedIdSchema)
	if err := csWireFuzzRepeatedIdRule(files); err != nil {
		t.Fatalf("NEGATIVE CONTROL setup: the real artifact is already red before sabotage: %v", err)
	}
	planted := 0
	for name, data := range files {
		s := string(data)
		if strings.Contains(s, csWireFuzzBodyRepeat) {
			s = strings.ReplaceAll(s, csWireFuzzBodyRepeat, "/* SABOTAGED: a repeated field id is last-wins */")
			planted++
		}
		if strings.Contains(s, csWireFuzzKeyedRepeat) {
			s = strings.ReplaceAll(s, csWireFuzzKeyedRepeat, "/* SABOTAGED: a repeated keyed slot is last-wins */")
			planted++
		}
		files[name] = []byte(s)
	}
	if planted != 2 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the duplicate refusals appeared %d times across the emitted files, want 2", planted)
	}
	err := csWireFuzzRepeatedIdRule(files)
	if err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: taking the duplicate refusals out of the real artifact left the check GREEN")
	}
	t.Logf("negative control: the duplicate refusal removed from the real artifact turns the check red, as required: %v", err)
}
