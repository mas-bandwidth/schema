// Verify SPEC §4.3's finite float rule on the Go packet target, against
// serialize.go format 2.0. Flat is packed as one flat run, so the generated
// code places its float bits itself and owns the refusal; Single32 and Single64
// keep the per-field form and reach the runtime's SerializeFloat32/64. Every
// shape must refuse a NaN or an infinity on write and on read with
// non_finite_float, and carry negative zero, the subnormals and the largest
// finite value bit exact.
//
// Each failing vector prints one line, "FAILED: <vector>: <detail>". The
// negative controls compare the set of failing vector names against the
// .expected list beside this file.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	f "packetfinite"

	"github.com/mas-bandwidth/serialize.go/v2"
)

var failed bool

func fail(name, format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAILED: %s: %s\n", name, fmt.Sprintf(format, args...))
	failed = true
}

// refusedNonFinite reports whether err is the format's non_finite_float.
func refusedNonFinite(err error) bool {
	if errors.Is(err, errWriteTrapped) {
		return true
	}
	var refusal serialize.Refusal
	return errors.Is(err, serialize.ErrNonFiniteFloat) && errors.As(err, &refusal) && refusal.String() == "non_finite_float"
}

// The non-finite patterns: quiet and signalling NaN of either sign, a NaN with
// a payload, and both infinities. Each has an all-ones exponent field.
var nonFinite32 = []struct {
	name string
	bits uint32
}{
	{"quiet-nan", 0x7fc00000},
	{"signalling-nan", 0x7f800001},
	{"negative-nan", 0xffc00000},
	{"payload-nan", 0x7fc0dead},
	{"positive-inf", 0x7f800000},
	{"negative-inf", 0xff800000},
}

var nonFinite64 = []struct {
	name string
	bits uint64
}{
	{"quiet-nan", 0x7ff8000000000000},
	{"signalling-nan", 0x7ff0000000000001},
	{"negative-nan", 0xfff8000000000000},
	{"payload-nan", 0x7ff800000000dead},
	{"positive-inf", 0x7ff0000000000000},
	{"negative-inf", 0xfff0000000000000},
}

// The finite edge patterns that must ride bit exact: no writer normalizes one.
var finite32 = []struct {
	name string
	bits uint32
}{
	{"negative-zero", 0x80000000},
	{"min-subnormal", 0x00000001},
	{"max-subnormal", 0x007fffff},
	{"negative-subnormal", 0x80000001},
	{"max-finite", 0x7f7fffff},
	{"negative-max-finite", 0xff7fffff},
}

var finite64 = []struct {
	name string
	bits uint64
}{
	{"negative-zero", 0x8000000000000000},
	{"min-subnormal", 0x0000000000000001},
	{"max-subnormal", 0x000fffffffffffff},
	{"negative-subnormal", 0x8000000000000001},
	{"max-finite", 0x7fefffffffffffff},
	{"negative-max-finite", 0xffefffffffffffff},
}

// rawWire writes the given raw patterns the way the generated code lays them
// out, bypassing every float entry point, so a reader can be handed a pattern
// no conforming writer emits.
func rawWire(write func(*serialize.WriteStream)) []byte {
	stream := serialize.NewWriteStream(make([]byte, 64))
	write(stream)
	stream.Flush()
	if err := stream.Err(); err != nil {
		panic(err)
	}
	return bytes.Clone(stream.Data())
}

func flatWire(a uint32, b uint64) []byte {
	return rawWire(func(s *serialize.WriteStream) {
		s.SerializeBits(&a, 32)
		s.SerializeBits64(&b, 64)
		c := true
		s.SerializeBool(&c)
	})
}

func single32Wire(x uint32) []byte {
	return rawWire(func(s *serialize.WriteStream) { s.SerializeBits(&x, 32) })
}

func single64Wire(x uint64) []byte {
	return rawWire(func(s *serialize.WriteStream) { s.SerializeBits64(&x, 64) })
}

// errWriteTrapped is the runtime's write-side refusal. serialize.go v2.1.1
// (IMPLEMENTING U9) traps write misuse by panicking at the call, before any bit
// is written, as the C++ reference's asserts do; SPEC says a writer refuses a
// non-finite float "the runtime asserts or fails". The generated flat form,
// which packs float bits itself, returns serialize.ErrNonFiniteFloat instead.
var errWriteTrapped = errors.New("the runtime trapped a non-finite float on write")

const nonFiniteTrap = "NaN or an infinity"

// write runs a generated writer into a fresh stream, returning its bytes or
// its refusal. A runtime trap for a non-finite float is the refusal
// errWriteTrapped; any other panic is not swallowed.
func write[T any](value *T, writer func(*serialize.WriteStream, *T) error) (wire []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			if strings.Contains(fmt.Sprint(r), nonFiniteTrap) {
				wire, err = nil, errWriteTrapped
				return
			}
			panic(r)
		}
	}()
	stream := serialize.NewWriteStream(make([]byte, 64))
	if err := writer(stream, value); err != nil {
		return nil, err
	}
	stream.Flush()
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return bytes.Clone(stream.Data()), nil
}

func checkWriteRefused[T any](name string, value *T, writer func(*serialize.WriteStream, *T) error) {
	wire, err := write(value, writer)
	if err == nil {
		fail(name, "write accepted a non-finite float (% x)", wire)
		return
	}
	if !refusedNonFinite(err) {
		fail(name, "write refused with %v, want non_finite_float", err)
	}
}

func checkReadRefused[T any](name string, wire []byte, reader func(*serialize.ReadStream, *T) error) {
	var value T
	err := reader(serialize.NewReadStream(wire), &value)
	if err == nil {
		fail(name, "read accepted a non-finite pattern")
		return
	}
	if !refusedNonFinite(err) {
		fail(name, "read refused with %v, want non_finite_float", err)
	}
}

// checkRoundTrip writes value, requires exactly the raw pattern's wire, and
// reads it back; same reports whether the read value carries the same bits.
func checkRoundTrip[T any](name string, value *T, want []byte,
	writer func(*serialize.WriteStream, *T) error, reader func(*serialize.ReadStream, *T) error, same func(*T) bool) {
	wire, err := write(value, writer)
	if err != nil {
		fail(name, "write refused a finite value: %v", err)
		return
	}
	if !bytes.Equal(wire, want) {
		fail(name, "wire % x, want % x: the pattern did not ride bit exact", wire, want)
		return
	}
	var got T
	if err := reader(serialize.NewReadStream(wire), &got); err != nil {
		fail(name, "read refused a finite pattern: %v", err)
		return
	}
	if !same(&got) {
		fail(name, "read back %+v, not the pattern written", got)
	}
}

func main() {
	vectors := 0
	for _, c := range nonFinite32 {
		value := math.Float32frombits(c.bits)
		checkWriteRefused("flat-f32/write/"+c.name, &f.Flat{A: value}, f.WriteFlat)
		checkWriteRefused("single32/write/"+c.name, &f.Single32{X: value}, f.WriteSingle32)
		checkReadRefused("flat-f32/read/"+c.name, flatWire(c.bits, 0), f.ReadFlat)
		checkReadRefused("single32/read/"+c.name, single32Wire(c.bits), f.ReadSingle32)
		vectors += 4
	}
	for _, c := range nonFinite64 {
		value := math.Float64frombits(c.bits)
		checkWriteRefused("flat-f64/write/"+c.name, &f.Flat{B: value}, f.WriteFlat)
		checkWriteRefused("single64/write/"+c.name, &f.Single64{X: value}, f.WriteSingle64)
		checkReadRefused("flat-f64/read/"+c.name, flatWire(0, c.bits), f.ReadFlat)
		checkReadRefused("single64/read/"+c.name, single64Wire(c.bits), f.ReadSingle64)
		vectors += 4
	}
	for _, c := range finite32 {
		value := math.Float32frombits(c.bits)
		checkRoundTrip("flat-f32/roundtrip/"+c.name, &f.Flat{A: value, C: true}, flatWire(c.bits, 0),
			f.WriteFlat, f.ReadFlat, func(v *f.Flat) bool { return math.Float32bits(v.A) == c.bits && v.B == 0 && v.C })
		checkRoundTrip("single32/roundtrip/"+c.name, &f.Single32{X: value}, single32Wire(c.bits),
			f.WriteSingle32, f.ReadSingle32, func(v *f.Single32) bool { return math.Float32bits(v.X) == c.bits })
		vectors += 2
	}
	for _, c := range finite64 {
		value := math.Float64frombits(c.bits)
		checkRoundTrip("flat-f64/roundtrip/"+c.name, &f.Flat{B: value, C: true}, flatWire(0, c.bits),
			f.WriteFlat, f.ReadFlat, func(v *f.Flat) bool { return math.Float64bits(v.B) == c.bits && v.A == 0 && v.C })
		checkRoundTrip("single64/roundtrip/"+c.name, &f.Single64{X: value}, single64Wire(c.bits),
			f.WriteSingle64, f.ReadSingle64, func(v *f.Single64) bool { return math.Float64bits(v.X) == c.bits })
		vectors += 2
	}
	if failed {
		os.Exit(1)
	}
	fmt.Printf("packet finite floats Go: %d vectors OK\n", vectors)
}
