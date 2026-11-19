package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

type sliceSpec struct {
	TypeName string
	Bytes    int
}

type singleGolden struct {
	Name   string
	Module string // "Types", "Wire", "Ludicrous"
	Type   string
}

var canonicalSingleGoldens = []singleGolden{
	{"shipcreate_flags", "Types", "ShipCreate"},
	{"rigidbody_moving", "Types", "RigidBody"},
	{"rigidbody_at_rest", "Types", "RigidBody"},
	{"probe_header", "Wire", "ProbeHeader"},
	{"probebits", "Wire", "ProbeBits"},
	{"chat", "Wire", "Chat"},
	{"compressed_probe", "Wire", "CompressedProbe"},
	{"probecollider", "Wire", "ProbeCollider"},
	{"testdata", "Wire", "TestData"},
	{"inputpacket", "Types", "InputPacket"},
	{"probearray", "Wire", "ProbeArray"},
	{"ludicrous_state", "Ludicrous", "LudicrousState"},
	{"ludicrous_state_untargeted", "Ludicrous", "LudicrousState"},
	{"degenerate_probe", "Ludicrous", "DegenerateProbe"},
	{"unsigned_probe", "Ludicrous", "UnsignedProbe"},
}

type hostileReadCase struct {
	Name string
	Type string // type in VerifyHostile
}

var hostileReadCases = []hostileReadCase{
	{"bad_const", "ConstReservedProbe"},
	{"bad_reserved_lo", "ConstReservedProbe"},
	{"bad_reserved_hi", "ConstReservedProbe"},
	{"bad_align_lo", "AlignProbe"},
	{"bad_align_hi", "AlignProbe"},
	{"bad_trail_bit5", "TrailProbe"},
	{"bad_trail_bit7", "TrailProbe"},
	{"bad_trail_extra", "TrailProbe"},
	{"bad_enum", "RangeProbe"},
	{"bad_ranged_int", "RangeProbe"},
	{"bad_ranged_int128", "RangeProbe"},
	{"bad_text_align", "TextProbe"},
	{"bad_text_len", "TextProbe"},
	{"bad_utf8_lead", "TextProbe"},
	{"bad_utf8_overlong", "TextProbe"},
	{"bad_utf8_surrogate", "TextProbe"},
	{"bad_utf8_trunc", "TextProbe"},
	{"bad_utf8_nul", "TextProbe"},
	{"bad_f32_nan", "FloatProbe"},
	{"bad_f32_pos_inf", "FloatProbe"},
	{"bad_f32_neg_inf", "FloatProbe"},
	{"bad_f64_nan", "FloatProbe"},
	{"bad_f64_pos_inf", "FloatProbe"},
	{"bad_f64_neg_inf", "FloatProbe"},
	{"bad_cf_range", "FloatProbe"},
}

func ceilBytes(bits int) int {
	return (bits + 7) / 8
}

func buildClausesSlices() []sliceSpec {
	var out []sliceSpec
	for _, c := range []int{0, 1, 3, 4, 5, 7, 12} {
		out = append(out, sliceSpec{"W13", ceilBytes(4 + 13*c)})
	}
	for _, c := range []int{0, 1, 2, 3, 4, 9} {
		out = append(out, sliceSpec{"W17", ceilBytes(4 + 17*c)})
	}
	for _, c := range []int{0, 1, 2, 3, 6} {
		out = append(out, sliceSpec{"W26", ceilBytes(3 + 26*c)})
	}
	for _, c := range []int{0, 1, 3, 4, 5, 20} {
		out = append(out, sliceSpec{"W1", ceilBytes(5 + c)})
	}
	for c := range 4 {
		out = append(out, sliceSpec{"W52", ceilBytes(2 + 52*c)})
	}
	for c := range 4 {
		out = append(out, sliceSpec{"W50", ceilBytes(2 + 50*c)})
	}
	out = append(out, sliceSpec{"F13", ceilBytes(91)})
	for _, c := range []int{0, 1, 3, 4, 5, 10} {
		out = append(out, sliceSpec{"ArrTri3", ceilBytes(4 + 3*c)})
	}
	out = append(out, sliceSpec{"ArrEleven", ceilBytes(99)})
	for range 3 {
		out = append(out, sliceSpec{"HoldsEmptyUnion", ceilBytes(14)})
	}
	for _, bits := range []int{27, 155, 75} {
		out = append(out, sliceSpec{"Strs", ceilBytes(bits)})
	}
	for c := range 5 {
		out = append(out, sliceSpec{"ArrNested", ceilBytes(11 + 11*c)})
	}
	out = append(out, sliceSpec{"Sole", ceilBytes(13)})
	return out
}

func buildJoinsSlices() []sliceSpec {
	var out []sliceSpec
	for _, f := range []bool{false, true} {
		out = append(out, sliceSpec{"ArmsAgree", ceilBytes(24)})
		if f {
			out = append(out, sliceSpec{"ArmsDisagree", ceilBytes(24)})
			out = append(out, sliceSpec{"ArmEmpty", ceilBytes(32)})
			out = append(out, sliceSpec{"ArmAlign", ceilBytes(55)})
		} else {
			out = append(out, sliceSpec{"ArmsDisagree", ceilBytes(16)})
			out = append(out, sliceSpec{"ArmEmpty", ceilBytes(13)})
			out = append(out, sliceSpec{"ArmAlign", ceilBytes(23)})
		}
		out = append(out, sliceSpec{"ArmAlign", ceilBytes(23)})
	}
	for _, o := range []bool{false, true} {
		for _, i := range []bool{false, true} {
			bits := 23
			if o {
				if i {
					bits = 40
				} else {
					bits = 16
				}
			}
			out = append(out, sliceSpec{"ArmsNested", ceilBytes(bits)})
		}
	}
	for _, f := range []bool{false, true} {
		for c := range 4 {
			bits := 22
			if f {
				bits = 15 + 13*c
			}
			out = append(out, sliceSpec{"ArmArray", ceilBytes(bits)})
		}
	}
	for _, bits := range []int{18, 21, 55} {
		out = append(out, sliceSpec{"HoldsUneven", ceilBytes(bits)})
	}
	unevenItemBits := []int{0, 5, 44, 49}
	for c := range 4 {
		out = append(out, sliceSpec{"ArrUneven", ceilBytes(10 + unevenItemBits[c])})
	}
	for c := range 4 {
		for _, sl := range []int{0, 4} {
			afterAlign := ceilBytes(5+2+13*c+3) * 8
			out = append(out, sliceSpec{"RegainAfterAlign", ceilBytes(afterAlign + 8*sl + 84)})
		}
	}
	return out
}

type bitPacker struct {
	buf  []byte
	bits int
}

func (p *bitPacker) writeBits(v uint64, n int) {
	for i := range n {
		byteIdx := p.bits >> 3
		bitIdx := uint(p.bits & 7)
		if byteIdx >= len(p.buf) {
			p.buf = append(p.buf, 0)
		}
		if (v>>uint(i))&1 != 0 {
			p.buf[byteIdx] |= 1 << bitIdx
		}
		p.bits++
	}
}

func (p *bitPacker) align() {
	for (p.bits & 7) != 0 {
		p.writeBits(0, 1)
	}
}

func (p *bitPacker) writeBytes(b []byte) {
	p.align()
	p.buf = append(p.buf, b...)
	p.bits += 8 * len(b)
}

func (p *bitPacker) bytes() []byte {
	if len(p.buf) == 0 && p.bits == 0 {
		return []byte{}
	}
	return append([]byte(nil), p.buf...)
}

func packFloatProbe(f32 uint32, f64 uint64, cfStep uint64) []byte {
	var p bitPacker
	p.writeBits(uint64(f32), 32)
	p.writeBits(f64, 64)
	p.writeBits(cfStep, 11)
	return p.bytes()
}

func packReuseEnvelope(active, inner bool, val uint64, choiceTag int, choicePayload uint64, marker uint8) []byte {
	var p bitPacker
	if active {
		p.writeBits(1, 1)
		if inner {
			p.writeBits(1, 1)
			p.writeBits(val, 32)
		} else {
			p.writeBits(0, 1)
			p.writeBits(val, 16)
		}
	} else {
		p.writeBits(0, 1)
		p.writeBits(val, 32)
	}
	p.writeBits(uint64(choiceTag), 2)
	switch choiceTag {
	case 2: // Tiny (5 bits)
		p.writeBits(choicePayload, 5)
	case 3: // Heavy (64 bits)
		p.writeBits(choicePayload, 64)
	}
	p.writeBits(uint64(marker), 8)
	return p.bytes()
}

func buildPacketInputs(repoRoot string) (map[string][]byte, error) {
	in := map[string][]byte{}
	names := []string{"clauses", "joins"}
	for _, g := range canonicalSingleGoldens {
		names = append(names, g.Name)
	}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "wire", n+".bin"))
		if err != nil {
			return nil, fmt.Errorf("read golden %s: %w", n, err)
		}
		in[n] = b
	}

	// Self-verify slice sums against C++ canonical goldens.
	sumSlices := func(slices []sliceSpec) int {
		tot := 0
		for _, s := range slices {
			tot += s.Bytes
		}
		return tot
	}
	if got := sumSlices(buildClausesSlices()); got != len(in["clauses"]) {
		return nil, fmt.Errorf("clauses slices sum to %d, want %d", got, len(in["clauses"]))
	}
	if got := sumSlices(buildJoinsSlices()); got != len(in["joins"]) {
		return nil, fmt.Errorf("joins slices sum to %d, want %d", got, len(in["joins"]))
	}

	// Valid VerifyHostile wire vectors.
	in["valid_const_reserved"] = []byte{0xAA, 0xA0} // const=42, tag=2, reserved=0, tail=5
	in["valid_align"] = []byte{0x05, 0xCC}          // lead=5, align=0, body=0xCC
	in["valid_trail"] = []byte{0x13}                // val=19, trailing=0
	in["valid_range"] = []byte{0x9E, 0x25}          // mode=2, small=17(d=7), wide=50(d=150)
	utf8Sample := []byte("hé🎉")                     // 7 bytes: 68 c3 a9 f0 9f 8e 89
	in["valid_text"] = append([]byte{byte(len(utf8Sample))}, utf8Sample...)
	in["valid_float_negzero"] = packFloatProbe(0x80000000, 0x8000000000000000, 0)

	// Stateful ReuseEnvelope vectors:
	// 1: active=true, inner=true, a=0xDEADBEEF, choice=Heavy(0x1122334455667788), marker=0xA1
	// 2: active=true, inner=false, b=0x4321, choice=VoidArm, marker=0xB2
	// 3: active=false, c=0xCAFEBABE, choice=Tiny(19), marker=0xC3
	// 4: active=true, inner=true, a=7, choice=None, marker=0xD4
	in["reuse_env_1"] = packReuseEnvelope(true, true, 0xDEADBEEF, 3, 0x1122334455667788, 0xA1)
	in["reuse_env_2"] = packReuseEnvelope(true, false, 0x4321, 1, 0, 0xB2)
	in["reuse_env_3"] = packReuseEnvelope(false, false, 0xCAFEBABE, 2, 19, 0xC3)
	in["reuse_env_4"] = packReuseEnvelope(true, true, 7, 0, 0, 0xD4)
	in["bad_rb_trunc"] = in["rigidbody_moving"][:10]
	in["bad_reuse_trunc"] = in["reuse_env_1"][:2]

	// Hostile packet bit-mutations.
	in["bad_const"] = []byte{0xAB, 0xA0}
	in["bad_reserved_lo"] = []byte{0xAA, 0xA1}
	in["bad_reserved_hi"] = []byte{0xAA, 0xB0}
	in["bad_align_lo"] = []byte{0x0D, 0xCC}
	in["bad_align_hi"] = []byte{0x85, 0xCC}
	in["bad_trail_bit5"] = []byte{0x33}
	in["bad_trail_bit7"] = []byte{0x93}
	in["bad_trail_extra"] = []byte{0x13, 0x00}
	in["bad_enum"] = []byte{0x03, 0x00}
	in["bad_ranged_int"] = []byte{0x2D, 0x00}
	in["bad_ranged_int128"] = []byte{0x41, 0x32}
	in["bad_text_align"] = []byte{0x03 | 0x20, 'a', 'b', 'c'}
	in["bad_text_len"] = append([]byte{17}, bytes.Repeat([]byte{'a'}, 17)...)
	in["bad_utf8_lead"] = []byte{2, 0xFF, 0x61}
	in["bad_utf8_overlong"] = []byte{2, 0xC0, 0x80}
	in["bad_utf8_surrogate"] = []byte{3, 0xED, 0xA0, 0x80}
	in["bad_utf8_trunc"] = []byte{2, 0xE2, 0x82}
	in["bad_utf8_nul"] = []byte{3, 0x61, 0x00, 0x62}
	in["bad_f32_nan"] = packFloatProbe(0x7fc00000, 0, 0)
	in["bad_f32_pos_inf"] = packFloatProbe(0x7f800000, 0, 0)
	in["bad_f32_neg_inf"] = packFloatProbe(0xff800000, 0, 0)
	in["bad_f64_nan"] = packFloatProbe(0, 0x7ff8000000000000, 0)
	in["bad_f64_pos_inf"] = packFloatProbe(0, 0x7ff0000000000000, 0)
	in["bad_f64_neg_inf"] = packFloatProbe(0, 0xfff0000000000000, 0)
	in["bad_cf_range"] = packFloatProbe(math.Float32bits(1.0), math.Float64bits(1.0), 2047)

	_ = binary.LittleEndian
	return in, nil
}
