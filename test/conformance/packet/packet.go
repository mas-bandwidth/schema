// Package packet is the packet carrier of the conformance harness.
// The table corpus proves the table wire. This carrier proves the packet
// wire (docs/SPEC.md §7.2 gates 4 and 5) for schemas seeded outside examples/.
//
// A generator emits random instances per schema as canonical JSON. Each
// participating driver writes those instances and reads every other driver's
// bytes, and a bit-flip sweep compares read verdicts. The drivers here run
// in-process, so the tests are unit tests: no real time, no sockets, no
// network, no outside services, and no external compiler or runtime.
//
// The seeded wire matches the generated codecs. align at the start of T
// pads to a byte, which is zero bits on a fresh packet. The union then
// writes a one-bit tag, None = 0 and a = 1, and when the arm is taken the
// value in seven bits. The byte array follows at the next bit, not the
// next byte: the union's width depends on the arm, so the array is not
// statically byte-aligned. None with data {1, 2, 3, 4} is 02 04 06 08 00,
// 33 bits in five bytes. A reader stops at the last field. Leftover bits
// in the final byte are not a refusal, and a short buffer is.
package packet

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Schema is one seeded packet schema.
type Schema struct {
	Name   string
	Source string
	Root   string
}

const seedName = "union-before-byte-array"

// The shape is a ranged scalar in a one-arm union, an alignment point,
// and a fixed byte array after the union. It is not one of examples/.
const seedSource = `package packetcarrier

type Small
{
    v uint8 | min = 0, max = 127
}

union U
{
    a Small
}

type T
{
    align
    u U
    data [4]uint8
}
`

// Schemas returns the seeded schema set, in generation order.
func Schemas() []Schema {
	return []Schema{{Name: seedName, Source: seedSource, Root: "T"}}
}

// Instance is one value of the seeded root. V is zero when the arm is not taken.
type Instance struct {
	HasA bool
	V    uint8
	Data [4]uint8
}

// rng is xorshift64*, the generator in test/random_main.cpp. Seed 0 stays 0.
type rng struct{ state uint64 }

func (r *rng) next() uint64 {
	r.state ^= r.state >> 12
	r.state ^= r.state << 25
	r.state ^= r.state >> 27
	return r.state * 0x2545F4914F6CDD1D
}

func (r *rng) rangeIn(lo, hi uint64) uint64 { return lo + r.next()%(hi-lo+1) }

// Generate returns n deterministic instances of schema from seed.
func Generate(schema Schema, seed uint64, n int) ([]Instance, error) {
	if schema.Name != seedName {
		return nil, fmt.Errorf("packet: schema %q has no generator", schema.Name)
	}
	if n < 0 {
		return nil, errors.New("packet: negative instance count")
	}
	r := &rng{state: seed}
	out := make([]Instance, n)
	for i := range out {
		if r.next()&1 != 0 {
			out[i].HasA = true
			out[i].V = uint8(r.rangeIn(0, 127))
		}
		for j := range out[i].Data {
			out[i].Data[j] = uint8(r.next())
		}
	}
	return out, nil
}

// GenerateJSON returns the generated instances as canonical JSON.
func GenerateJSON(schema Schema, seed uint64, n int) ([][]byte, error) {
	instances, err := Generate(schema, seed, n)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(instances))
	for i, inst := range instances {
		out[i] = inst.CanonicalJSON()
	}
	return out, nil
}

// CanonicalJSON is the instance's one text form: fixed field order, no
// insignificant whitespace, an untaken arm spelled null.
func (inst Instance) CanonicalJSON() []byte {
	var b bytes.Buffer
	b.WriteString(`{"u":`)
	if inst.HasA {
		b.WriteString(`{"a":{"v":`)
		b.WriteString(strconv.Itoa(int(inst.V)))
		b.WriteString(`}}`)
	} else {
		b.WriteString(`null`)
	}
	b.WriteString(`,"data":[`)
	for i, v := range inst.Data {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(int(v)))
	}
	b.WriteString(`]}`)
	return b.Bytes()
}

type canonicalJSON struct {
	U    *unionJSON `json:"u"`
	Data []uint16   `json:"data"`
}

type unionJSON struct {
	A *smallJSON `json:"a"`
}

type smallJSON struct {
	V uint16 `json:"v"`
}

// ParseCanonicalJSON reads one canonical-JSON instance and refuses a value
// the schema does not admit.
func ParseCanonicalJSON(data []byte) (Instance, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wire canonicalJSON
	if err := dec.Decode(&wire); err != nil {
		return Instance{}, fmt.Errorf("packet: canonical JSON: %w", err)
	}
	if dec.More() {
		return Instance{}, errors.New("packet: canonical JSON: trailing data")
	}
	if len(wire.Data) != 4 {
		return Instance{}, fmt.Errorf("packet: canonical JSON: data has %d bytes, want 4", len(wire.Data))
	}
	inst := Instance{}
	for i, b := range wire.Data {
		if b > 255 {
			return Instance{}, fmt.Errorf("packet: canonical JSON: data[%d]=%d outside [0, 255]", i, b)
		}
		inst.Data[i] = uint8(b)
	}
	if wire.U != nil {
		if wire.U.A == nil {
			return Instance{}, errors.New("packet: canonical JSON: union names no arm")
		}
		if wire.U.A.V > 127 {
			return Instance{}, fmt.Errorf("packet: canonical JSON: v=%d outside [0, 127]", wire.U.A.V)
		}
		inst.HasA = true
		inst.V = uint8(wire.U.A.V)
	}
	return inst, nil
}

// Driver is one implementation of the packet wire for the seeded schemas.
type Driver interface {
	Name() string
	Write(canonical []byte) ([]byte, error)
	Read(wire []byte) ([]byte, error)
}

// Drivers returns every participating driver. Each must write identical
// bytes and agree on every read verdict.
func Drivers() []Driver {
	return []Driver{bitstreamDriver{}, oracleDriver{}}
}

var (
	errRefused = errors.New("packet: wire refused")
	errRange   = errors.New("packet: value outside its declared range")
)

type bitstreamDriver struct{}

func (bitstreamDriver) Name() string { return "bitstream" }

func (bitstreamDriver) Write(canonical []byte) ([]byte, error) {
	inst, err := ParseCanonicalJSON(canonical)
	if err != nil {
		return nil, err
	}
	return writeBits(inst)
}

func (bitstreamDriver) Read(wire []byte) ([]byte, error) {
	inst, err := readBits(wire)
	if err != nil {
		return nil, err
	}
	return inst.CanonicalJSON(), nil
}

type bitWriter struct {
	buf []byte
	pos int
}

func (w *bitWriter) bits(value uint64, n int) {
	for i := 0; i < n; i++ {
		for w.pos/8 >= len(w.buf) {
			w.buf = append(w.buf, 0)
		}
		if value>>uint(i)&1 != 0 {
			w.buf[w.pos/8] |= 1 << uint(w.pos%8)
		}
		w.pos++
	}
}

func (w *bitWriter) align() {
	if extra := (8 - w.pos%8) % 8; extra != 0 {
		w.bits(0, extra)
	}
}

func writeBits(inst Instance) ([]byte, error) {
	if inst.HasA && inst.V > 127 {
		return nil, errRange
	}
	w := &bitWriter{}
	w.align()
	if inst.HasA {
		w.bits(1, 1)
		w.bits(uint64(inst.V), 7)
	} else {
		w.bits(0, 1)
	}
	for _, b := range inst.Data {
		w.bits(uint64(b), 8)
	}
	return append([]byte(nil), w.buf...), nil
}

type bitReader struct {
	buf []byte
	pos int
}

func (r *bitReader) bits(n int) (uint64, error) {
	if r.pos+n > len(r.buf)*8 {
		return 0, errRefused
	}
	var value uint64
	for i := 0; i < n; i++ {
		if r.buf[r.pos/8]>>uint(r.pos%8)&1 != 0 {
			value |= 1 << uint(i)
		}
		r.pos++
	}
	return value, nil
}

func (r *bitReader) align() error {
	extra := (8 - r.pos%8) % 8
	if extra == 0 {
		return nil
	}
	pad, err := r.bits(extra)
	if err != nil {
		return err
	}
	if pad != 0 {
		return errRefused
	}
	return nil
}

func readBits(wire []byte) (Instance, error) {
	r := &bitReader{buf: wire}
	if err := r.align(); err != nil {
		return Instance{}, err
	}
	tag, err := r.bits(1)
	if err != nil {
		return Instance{}, err
	}
	inst := Instance{}
	if tag == 1 {
		v, err := r.bits(7)
		if err != nil {
			return Instance{}, err
		}
		inst.HasA = true
		inst.V = uint8(v)
	}
	for i := range inst.Data {
		v, err := r.bits(8)
		if err != nil {
			return Instance{}, err
		}
		inst.Data[i] = uint8(v)
	}
	return inst, nil
}

// oracleDriver packs the same wire with a little-endian shift, not a bit
// loop, so agreement with bitstreamDriver is two implementations.
type oracleDriver struct{}

func (oracleDriver) Name() string { return "oracle" }

func (oracleDriver) Write(canonical []byte) ([]byte, error) {
	inst, err := ParseCanonicalJSON(canonical)
	if err != nil {
		return nil, err
	}
	return writeShift(inst)
}

func (oracleDriver) Read(wire []byte) ([]byte, error) {
	inst, err := readShift(wire)
	if err != nil {
		return nil, err
	}
	return inst.CanonicalJSON(), nil
}

func writeShift(inst Instance) ([]byte, error) {
	if inst.HasA && inst.V > 127 {
		return nil, errRange
	}
	var acc uint64
	n := 0
	put := func(value uint64, bits int) {
		if bits < 64 {
			value &^= ^uint64(0) << uint(bits)
		}
		acc |= value << uint(n)
		n += bits
	}
	if extra := (8 - n%8) % 8; extra != 0 { // the align item; zero at bit 0
		n += extra
	}
	if inst.HasA {
		put(1, 1)
		put(uint64(inst.V), 7)
	} else {
		put(0, 1)
	}
	for _, b := range inst.Data {
		put(uint64(b), 8)
	}
	out := make([]byte, (n+7)/8)
	for i := range out {
		out[i] = byte(acc >> uint(8*i))
	}
	return out, nil
}

func readShift(wire []byte) (Instance, error) {
	if len(wire) == 0 {
		return Instance{}, errRefused
	}
	var acc uint64
	for i, b := range wire {
		if i >= 8 {
			break
		}
		acc |= uint64(b) << uint(8*i)
	}
	// align at bit 0 consumes nothing. A later align would refuse nonzero pad.
	tag := acc & 1
	need := 1 + 32
	shift := 1
	inst := Instance{}
	if tag == 1 {
		need = 1 + 7 + 32
		shift = 8
		inst.HasA = true
		inst.V = uint8((acc >> 1) & 0x7f)
	}
	if len(wire)*8 < need {
		return Instance{}, errRefused
	}
	for i := range inst.Data {
		inst.Data[i] = uint8((acc >> uint(shift+8*i)) & 0xff)
	}
	return inst, nil
}
