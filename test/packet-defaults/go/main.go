// Verify constructor storage and the eight C++ packet goldens. Reads also
// exercise reused storage: field prefixes overlay, absent branches clear,
// and every union selection constructs its payload with declared defaults.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	d "packetdefaults"
	p "packetplain"

	"github.com/mas-bandwidth/serialize.go"
)

var failed bool

func check(ok bool, name string) {
	if !ok {
		fmt.Fprintf(os.Stderr, "FAILED: %s\n", name)
		failed = true
	}
}

func checkErr(err error, name string) bool {
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: %s: %v\n", name, err)
		failed = true
		return false
	}
	return true
}

// These are fixture values, independent of generated constructors. Schema
// literals have no escape syntax: token contains backslash-n, backslash-t.
func expectedSample() d.Sample {
	return d.Sample{
		Name: [6]byte{0xc3, 0xa9, 0xf0, 0x90, 0x80, 0x80}, NameLength: 6,
		Token: [4]byte{0x5c, 0x6e, 0x5c, 0x74}, TokenLength: 4,
		Caps: 5,
	}
}

func shortSample() d.Sample {
	return d.Sample{
		Name: [6]byte{'A'}, NameLength: 1,
		Token: [4]byte{0, 0xff}, TokenLength: 2,
		Caps: 2,
	}
}

func dirtySample() d.Sample {
	return d.Sample{
		Name: [6]byte{'d', 'i', 'r', 't', 'y', '!'}, NameLength: 6,
		Token: [4]byte{0xa1, 0xa2, 0xa3, 0xa4}, TokenLength: 4,
		Caps:      7,
		EmptyName: [3]byte{'o', 'l', 'd'}, EmptyNameLength: 3,
		EmptyToken: [2]byte{0xb1, 0xb2}, EmptyTokenLength: 2,
		EmptyCaps: 7,
	}
}

func writeWire[T any](name string, value T, write func(*serialize.WriteStream, *T) error) ([]byte, int64, bool) {
	stream := serialize.NewWriteStream(make([]byte, 4096))
	if !checkErr(write(stream, &value), name+": write") {
		return nil, 0, false
	}
	bits := stream.BitsProcessed()
	stream.Flush()
	if !checkErr(stream.Err(), name+": flush") {
		return nil, 0, false
	}
	return stream.Data(), bits, true
}

func readWire[T comparable](name string, wire []byte, bits int64, initial, want T, read func(*serialize.ReadStream, *T) error) T {
	stream := serialize.NewReadStream(wire)
	value := initial
	if !checkErr(read(stream, &value), name+": read") {
		return value
	}
	check(stream.BitsProcessed() == bits, name+": read consumed bits")
	check(value == want, name+": read values and backing storage")
	return value
}

func golden[T comparable](directory, name string, value, initial, want T,
	write func(*serialize.WriteStream, *T) error, read func(*serialize.ReadStream, *T) error) {
	wire, bits, ok := writeWire(name, value, write)
	if !ok {
		return
	}
	expected, err := os.ReadFile(filepath.Join(directory, name+".bin"))
	if !checkErr(err, name+": read C++ byte golden") {
		return
	}
	bitText, err := os.ReadFile(filepath.Join(directory, name+".bits"))
	if !checkErr(err, name+": read C++ bit golden") {
		return
	}
	expectedBits, err := strconv.ParseInt(strings.TrimSpace(string(bitText)), 10, 64)
	if !checkErr(err, name+": parse C++ bit golden") {
		return
	}
	check(bytes.Equal(wire, expected), name+": C++ golden bytes")
	check(bits == expectedBits, name+": C++ golden bits")
	// Decode the oracle's bytes, not the Go writer's output.
	readWire(name, expected, expectedBits, initial, want, read)
}

func roundTrip[T comparable](name string, value, initial, want T,
	write func(*serialize.WriteStream, *T) error, read func(*serialize.ReadStream, *T) error) T {
	wire, bits, ok := writeWire(name, value, write)
	if ok {
		return readWire(name, wire, bits, initial, want, read)
	}
	return initial
}

func run(directory string) {
	sample := d.NewSample()
	expected := expectedSample()
	// Keep this stable: the named negative control sabotages constructor bytes.
	check(sample == expected, "packet-default constructor bytes")
	check(sample.NameLength == 6 && sample.TokenLength == 4, "constructor byte lengths")
	check(uint64(sample.Caps) == 5 && uint64(sample.EmptyCaps) == 0, "constructor nonzero and empty flags masks")
	check(sample.EmptyNameLength == 0 && sample.EmptyName == [3]byte{} &&
		sample.EmptyTokenLength == 0 && sample.EmptyToken == [2]byte{}, "constructor explicit empty byte storage")
	var zero d.Sample
	check(zero != sample && zero == (d.Sample{}), "plain Go zero value remains distinct from specified defaults")
	var plain p.Sample
	check(plain.Name == [6]byte{} && plain.NameLength == 0 &&
		plain.Token == [4]byte{} && plain.TokenLength == 0 && uint64(plain.Caps) == 0 &&
		plain.EmptyName == [3]byte{} && plain.EmptyNameLength == 0 &&
		plain.EmptyToken == [2]byte{} && plain.EmptyTokenLength == 0 && uint64(plain.EmptyCaps) == 0,
		"defaultless Sample has zero storage")
	check(d.NewEmptyOnly() == (d.EmptyOnly{}), "explicit-empty-only constructor exists and returns zero")
	prefix := d.NewPrefix()
	check(prefix == (d.Prefix{
		Name: [5]byte{0xc3, 0xa9}, NameLength: 2,
		Token: [5]byte{0x5c, 0x6e}, TokenLength: 2,
	}), "constructor short literals have exact bytes, lengths and zero backing tails")
	wide := d.NewWideMask()
	check(uint64(wide.High) == uint64(1)<<63 && uint64(wide.All) == ^uint64(0), "constructor bit63 and all64 flags masks")
	wideWire, wideBits, wideOK := writeWire("wide flags defaults", wide, d.WriteWideMask)
	plainWide := p.WideMask{High: p.WideCaps(uint64(1) << 63), All: p.WideCaps(^uint64(0))}
	plainWideWire, plainWideBits, plainWideOK := writeWire("wide flags explicit plain values", plainWide, p.WriteWideMask)
	if wideOK && plainWideOK {
		check(wideBits == 128 && plainWideBits == wideBits && bytes.Equal(wideWire, plainWideWire),
			"wide flags defaults retain the defaultless 128-bit packet wire")
		readWire("wide flags defaults", wideWire, wideBits, d.WideMask{}, wide, d.ReadWideMask)
	}

	golden(directory, "sample-defaults", sample, d.Sample{}, expected, d.WriteSample, d.ReadSample)
	zeroCount := d.NewZeroCount()
	check(zeroCount.ItemsCount == 0 && zeroCount.Items == [2]d.Sample{expected, expected}, "zero-count backing defaults")
	zeroCountInitial := d.ZeroCount{Items: [2]d.Sample{dirtySample(), shortSample()}, ItemsCount: 2}
	zeroCountWant := zeroCountInitial
	zeroCountWant.ItemsCount = 0
	golden(directory, "zero-count", zeroCount, zeroCountInitial, zeroCountWant, d.WriteZeroCount, d.ReadZeroCount)

	batch := d.NewBatch()
	check(batch.Head == expected, "nested constructor defaults")
	for i, item := range batch.Items {
		check(item == expected, fmt.Sprintf("fixed array constructor defaults %d", i))
	}
	for i, item := range batch.Counted {
		check(item == expected, fmt.Sprintf("counted backing constructor defaults %d", i))
	}
	check(batch.CountedCount == 1, "counted constructor BornCount 1")
	plainBatch := p.NewBatch()
	check(plainBatch.CountedCount == 1 && plainBatch.Head == (p.Sample{}) &&
		plainBatch.Items == [2]p.Sample{} && plainBatch.Counted == [3]p.Sample{}, "defaultless constructor retains BornCount and zero elements")
	batchInitial := d.NewBatch()
	batchInitial.Counted[1] = dirtySample()
	batchInitial.Counted[2] = shortSample()
	batchWant := d.Batch{
		Head: expected, Items: [2]d.Sample{expected, expected},
		Counted: [3]d.Sample{expected, batchInitial.Counted[1], batchInitial.Counted[2]}, CountedCount: 1,
	}
	golden(directory, "batch-defaults", batch, batchInitial, batchWant, d.WriteBatch, d.ReadBatch)

	conditional := d.NewConditional()
	check(conditional.Enabled && conditional.Value == expected, "conditional constructor defaults")
	golden(directory, "conditional-on", conditional, d.Conditional{},
		d.Conditional{Enabled: true, Value: expected}, d.WriteConditional, d.ReadConditional)
	conditional.Enabled = false
	golden(directory, "conditional-off", conditional,
		d.Conditional{Enabled: true, Value: dirtySample()}, d.Conditional{}, d.WriteConditional, d.ReadConditional)

	var choice d.Choice
	var plainChoice p.Choice
	// C++ None has indeterminate arm bytes; only its tag is part of this check.
	check(choice.Type == d.ChoiceTypeNone && plainChoice.Type == p.ChoiceTypeNone, "union zero tag is None")
	choice.Type = d.ChoiceTypeSample
	choice.Sample = sample
	choiceInitial := d.Choice{
		Type: d.ChoiceTypeConditional, Sample: dirtySample(),
		Conditional: d.Conditional{Enabled: true, Value: dirtySample()},
	}
	choiceWant := choiceInitial
	choiceWant.Type = d.ChoiceTypeSample
	choiceWant.Sample = expected
	golden(directory, "choice-sample", choice, choiceInitial, choiceWant, d.WriteChoice, d.ReadChoice)

	// Keep defaults in the full name/token buffers. Dirty the explicitly empty
	// fields too, so their zero-length wire values must preserve those tails.
	overlay := d.NewSample()
	overlay.EmptyName = [3]byte{'o', 'l', 'd'}
	overlay.EmptyNameLength = 3
	overlay.EmptyToken = [2]byte{0xb1, 0xb2}
	overlay.EmptyTokenLength = 2
	overlay.EmptyCaps = 7
	shortWant := overlay
	shortWant.Name[0], shortWant.NameLength = 'A', 1
	shortWant.Token[0], shortWant.Token[1], shortWant.TokenLength = 0, 0xff, 2
	shortWant.Caps = 2
	shortWant.EmptyNameLength, shortWant.EmptyTokenLength, shortWant.EmptyCaps = 0, 0, 0
	golden(directory, "sample-short", shortSample(), overlay, shortWant, d.WriteSample, d.ReadSample)
	emptyWant := overlay
	emptyWant.NameLength, emptyWant.TokenLength, emptyWant.Caps = 0, 0, 0
	emptyWant.EmptyNameLength, emptyWant.EmptyTokenLength, emptyWant.EmptyCaps = 0, 0, 0
	golden(directory, "sample-empty", d.Sample{}, overlay, emptyWant, d.WriteSample, d.ReadSample)

	// Every selection starts from the declared defaults, even with the same
	// tag. Received prefixes overlay those fresh buffers; no previous poison
	// survives, and the unselected arm keeps its storage unchanged.
	selectedShort := expectedSample()
	selectedShort.Name[0], selectedShort.NameLength = 'A', 1
	selectedShort.Token[0], selectedShort.Token[1], selectedShort.TokenLength = 0, 0xff, 2
	selectedShort.Caps = 2
	selectedEmpty := expectedSample()
	selectedEmpty.NameLength, selectedEmpty.TokenLength, selectedEmpty.Caps = 0, 0, 0
	for _, tc := range []struct {
		name string
		wire d.Sample
		want d.Sample
	}{
		{"short", shortSample(), selectedShort},
		{"empty", d.Sample{}, selectedEmpty},
	} {
		choice.Sample = tc.wire
		choiceWant.Sample = tc.want
		reused := choiceInitial
		for attempt := 0; attempt < 2; attempt++ {
			reused.Sample = dirtySample()
			reused = roundTrip(fmt.Sprintf("choice-%s fresh declared-default tails, selection %d", tc.name, attempt),
				choice, reused, choiceWant, d.WriteChoice, d.ReadChoice)
		}
	}

	// Selection first constructs Conditional (Enabled=true and Sample defaults).
	// The received false gate must then apply the ordinary explicit zero rule.
	choice.Type = d.ChoiceTypeConditional
	choice.Conditional = d.Conditional{Value: expected}
	reused := choiceInitial
	reused.Type = d.ChoiceTypeSample
	choiceWant = reused
	choiceWant.Type = d.ChoiceTypeConditional
	choiceWant.Conditional = d.Conditional{}
	for attempt := 0; attempt < 2; attempt++ {
		reused.Conditional = d.Conditional{Enabled: true, Value: dirtySample()}
		reused = roundTrip(fmt.Sprintf("choice-conditional-off zero overrides construction defaults, selection %d", attempt),
			choice, reused, choiceWant, d.WriteChoice, d.ReadChoice)
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <golden-directory>\n", os.Args[0])
		os.Exit(2)
	}
	run(os.Args[1])
	if failed {
		os.Exit(1)
	}
	fmt.Println("packet defaults Go: constructors, eight C++ goldens and reused storage OK")
}
