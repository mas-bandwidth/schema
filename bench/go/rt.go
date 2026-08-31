// schema bench — families rt and bits for the Go runner.
//
// Family rt (BENCH-STANDARD.md §1.3, §1.5): the serialize.go runtime API
// called BY HAND — the four Bench.schema shapes as hand-written packets over
// the Serialize* surface, the way a game would write them. The §1.5 oracle
// gate byte-compares the hand-written wire against the goldens the GENERATED
// code pinned (testdata/wire/bench_*.bin) and round-trips before any number.
// Unexported identifiers in the runner's own package per §3.1. Per §3.2 every
// benched op has EXACTLY two call sites: its untimed once-helper and its
// timed loop (//go:noinline, so the §4.1 verdict has a loop body to count).
//
// Family bits (§1.4): the raw BitWriter/BitReader with the 16-width table
// (227 bits/group) over a 65536-byte buffer. Values vary per pass through
// the LCG (widths are the structure and stay fixed; bytes/pass asserted
// constant); reads rotate 64 pre-written variant buffers, each verified to
// read back exactly what was written before any number is produced.
package main

import (
	"bytes"
	"errors"
	"runtime"
	"time"

	"github.com/mas-bandwidth/serialize.go"
)

// errRtContract is the hand-written path's refusal for the two contract
// constructs the runtime API does not check for you: const(V, N) and
// reserved(N). The generated readers refuse the same bytes (SPEC §4.3).
var errRtContract = errors.New("rt: contract field mismatch")

// ---- the four shapes, hand-written (Bench.schema §1.3) ----

type rtBenchPacket struct {
	a, b, c               int32
	bits7, bits13, bits23 uint32
	flag                  bool
	x, y, z               float32
	big                   uint64
	blob                  [17]byte
}

func writeRtPacket(s *serialize.WriteStream, p *rtBenchPacket) error {
	s.SerializeInt(&p.a, -100, 100)
	s.SerializeInt(&p.b, 0, 65535)
	s.SerializeInt(&p.c, -1000000, 1000000)
	s.SerializeBits(&p.bits7, 7)
	s.SerializeBits(&p.bits13, 13)
	s.SerializeBits(&p.bits23, 23)
	s.SerializeBool(&p.flag)
	s.SerializeFloat32(&p.x)
	s.SerializeFloat32(&p.y)
	s.SerializeFloat32(&p.z)
	s.SerializeUint64(&p.big)
	s.SerializeBytes(p.blob[:]) // aligns internally — the schema says `align` out loud
	return s.Err()
}

func readRtPacket(s *serialize.ReadStream, p *rtBenchPacket) error {
	s.SerializeInt(&p.a, -100, 100)
	s.SerializeInt(&p.b, 0, 65535)
	s.SerializeInt(&p.c, -1000000, 1000000)
	s.SerializeBits(&p.bits7, 7)
	s.SerializeBits(&p.bits13, 13)
	s.SerializeBits(&p.bits23, 23)
	s.SerializeBool(&p.flag)
	s.SerializeFloat32(&p.x)
	s.SerializeFloat32(&p.y)
	s.SerializeFloat32(&p.z)
	s.SerializeUint64(&p.big)
	s.SerializeBytes(p.blob[:])
	return s.Err()
}

type rtBenchInts struct {
	f0, f1, f2, f3, f4, f5, f6, f7, f8, f9 int32
}

func writeRtInts(s *serialize.WriteStream, f *rtBenchInts) error {
	s.SerializeInt(&f.f0, -100, 100)
	s.SerializeInt(&f.f1, 0, 65535)
	s.SerializeInt(&f.f2, -1000000, 1000000)
	s.SerializeInt(&f.f3, 0, 3)
	s.SerializeInt(&f.f4, -15, 15)
	s.SerializeInt(&f.f5, 0, 1000)
	s.SerializeInt(&f.f6, -2048, 2047)
	s.SerializeInt(&f.f7, 0, 255)
	s.SerializeInt(&f.f8, -600000, 600000)
	s.SerializeInt(&f.f9, 0, 100)
	return s.Err()
}

func readRtInts(s *serialize.ReadStream, f *rtBenchInts) error {
	s.SerializeInt(&f.f0, -100, 100)
	s.SerializeInt(&f.f1, 0, 65535)
	s.SerializeInt(&f.f2, -1000000, 1000000)
	s.SerializeInt(&f.f3, 0, 3)
	s.SerializeInt(&f.f4, -15, 15)
	s.SerializeInt(&f.f5, 0, 1000)
	s.SerializeInt(&f.f6, -2048, 2047)
	s.SerializeInt(&f.f7, 0, 255)
	s.SerializeInt(&f.f8, -600000, 600000)
	s.SerializeInt(&f.f9, 0, 100)
	return s.Err()
}

type rtBenchBits struct {
	b7, b13, b23, b3, b32, b11, b19 uint32
	b48                             uint64
}

func writeRtBits(s *serialize.WriteStream, f *rtBenchBits) error {
	s.SerializeBits(&f.b7, 7)
	s.SerializeBits(&f.b13, 13)
	s.SerializeBits(&f.b23, 23)
	s.SerializeBits(&f.b3, 3)
	s.SerializeBits(&f.b32, 32)
	s.SerializeBits(&f.b11, 11)
	s.SerializeBits(&f.b19, 19)
	s.SerializeBits64(&f.b48, 48)
	return s.Err()
}

func readRtBits(s *serialize.ReadStream, f *rtBenchBits) error {
	s.SerializeBits(&f.b7, 7)
	s.SerializeBits(&f.b13, 13)
	s.SerializeBits(&f.b23, 23)
	s.SerializeBits(&f.b3, 3)
	s.SerializeBits(&f.b32, 32)
	s.SerializeBits(&f.b11, 11)
	s.SerializeBits(&f.b19, 19)
	s.SerializeBits64(&f.b48, 48)
	return s.Err()
}

// BenchMixed by hand (issue #184): every serialize runtime operation the
// schema language expresses, in the order the generated code emits them. The
// §1.5 oracle gate byte-compares this against the generated code's golden.
type rtMixedEntity struct {
	entityID         uint32
	posX, posY, posZ int32
	yaw, pitch       uint32
	velX, velY, velZ int32
	health           int32
	weapon           int32  // the enum wire
	damage           uint32 // the flags wire, 8 bits
	moving, firing   bool
}

type rtMixedStat struct {
	statID uint32
	delta  int32
}

type rtMixedHitEvent struct {
	targetID        uint32
	damage, hitKind int32
	crit            bool
}

type rtMixedChatEvent struct {
	channel int32
	speaker uint32
}

type rtMixedPickupEvent struct {
	itemID uint32
	amount int32
}

type rtBenchMixed struct {
	magic            uint32
	sequence         uint32
	ackSequence      int32
	ackBits          uint32
	sessionID        uint64
	clientID         uint32
	nonce            uint64
	worldTime        int64
	frameTick        uint64
	serverTime       int64 // raw Q24.8 (serialize.go's fixed API is 64-bit)
	entitiesCount    int32
	entities         [8]rtMixedEntity
	statsCount       int32
	stats            [80]rtMixedStat
	eventType        int32 // the union tag: 0 = None
	hit              rtMixedHitEvent
	chat             rtMixedChatEvent
	pickup           rtMixedPickupEvent
	loadout          [4]uint8
	playerNameLength int32
	playerName       [15]byte
	payloadLength    int32
	payload          [16]byte
	aimX, aimY, aimZ float32
	recoil           float32
	drift            float64
	wideKey          serialize.Uint128
	flux             serialize.Int128
	ping             int64 // raw UQ8.8
	reservedBits     uint32
	crcHint          uint32
	hasExtra         bool
	extra            int32
	idleTicks        int32
}

// the ±2^100 band flux rides in
var rtFluxMin = serialize.Int128{Lo: 0, Hi: 0xFFFFFFF000000000}
var rtFluxMax = serialize.Int128{Lo: 0, Hi: 0x1000000000}

func writeRtMixed(s *serialize.WriteStream, f *rtBenchMixed) error {
	s.SerializeBits(&f.magic, 16)
	s.SerializeBits(&f.sequence, 16)
	s.SerializeInt(&f.ackSequence, 0, 65535)
	s.SerializeBits(&f.ackBits, 32)
	s.SerializeUint64(&f.sessionID)
	s.SerializeUint32(&f.clientID)
	s.SerializeBits64(&f.nonce, 64) // the full-unsigned ranged path is width-computed bits
	s.SerializeInt64(&f.worldTime, -1000000000000, 1000000000000)
	s.SerializeBits64(&f.frameTick, 48)
	s.SerializeFixed64(&f.serverTime, 24, 8, 0, 65535)

	s.SerializeInt(&f.entitiesCount, 1, 8)
	for i := int32(0); i < f.entitiesCount; i++ {
		e := &f.entities[i]
		s.SerializeBits(&e.entityID, 12)
		s.SerializeInt(&e.posX, -16383, 16383)
		s.SerializeInt(&e.posY, -16383, 16383)
		s.SerializeInt(&e.posZ, -16383, 16383)
		s.SerializeBits(&e.yaw, 9)
		s.SerializeBits(&e.pitch, 9)
		s.SerializeInt(&e.velX, -2048, 2047)
		s.SerializeInt(&e.velY, -2048, 2047)
		s.SerializeInt(&e.velZ, -2048, 2047)
		s.SerializeInt(&e.health, 0, 1000)
		s.SerializeInt(&e.weapon, 0, 15)
		s.SerializeBits(&e.damage, 8)
		s.SerializeBool(&e.moving)
		s.SerializeBool(&e.firing)
	}

	s.SerializeInt(&f.statsCount, 0, 80)
	for i := int32(0); i < f.statsCount; i++ {
		s.SerializeBits(&f.stats[i].statID, 8)
		s.SerializeInt(&f.stats[i].delta, -512, 511)
	}

	s.SerializeInt(&f.eventType, 0, 3)
	switch f.eventType {
	case 1:
		s.SerializeBits(&f.hit.targetID, 12)
		s.SerializeInt(&f.hit.damage, 0, 4095)
		s.SerializeInt(&f.hit.hitKind, 0, 7)
		s.SerializeBool(&f.hit.crit)
	case 2:
		s.SerializeInt(&f.chat.channel, 0, 3)
		s.SerializeBits(&f.chat.speaker, 12)
	case 3:
		s.SerializeBits(&f.pickup.itemID, 10)
		s.SerializeInt(&f.pickup.amount, 0, 255)
	}

	for i := 0; i < 4; i++ {
		s.SerializeUint8(&f.loadout[i])
	}

	// string(15) and bytes(16) ride as their §4.3 decomposition in every rt
	// leg — see bench/cpp/bench_main.cpp for the reasoning
	s.SerializeInt(&f.playerNameLength, 0, 15)
	s.SerializeBytes(f.playerName[:f.playerNameLength])
	s.SerializeInt(&f.payloadLength, 0, 16)
	s.SerializeBytes(f.payload[:f.payloadLength])

	s.SerializeCompressedFloat32(&f.aimX, -1.0, 1.0, 0.01)
	s.SerializeCompressedFloat32(&f.aimY, -1.0, 1.0, 0.01)
	s.SerializeCompressedFloat32(&f.aimZ, -1.0, 1.0, 0.01)
	s.SerializeFloat32(&f.recoil)
	s.SerializeFloat64(&f.drift)
	s.SerializeUint128(&f.wideKey)
	s.SerializeInt128(&f.flux, rtFluxMin, rtFluxMax)
	s.SerializeFixed64(&f.ping, 8, 8, 0, 250)

	s.SerializeBits(&f.reservedBits, 4)
	s.SerializeAlign()
	s.SerializeBits(&f.crcHint, 24)
	s.SerializeBool(&f.hasExtra)
	if f.hasExtra {
		s.SerializeInt(&f.extra, 0, 255)
	} else {
		s.SerializeInt(&f.idleTicks, 0, 15)
	}
	return s.Err()
}

func readRtMixed(s *serialize.ReadStream, f *rtBenchMixed) error {
	s.SerializeBits(&f.magic, 16)
	if f.magic != 0xC0DE {
		return errRtContract // const(0xC0DE, 16): a read REJECTS any other value
	}
	s.SerializeBits(&f.sequence, 16)
	s.SerializeInt(&f.ackSequence, 0, 65535)
	s.SerializeBits(&f.ackBits, 32)
	s.SerializeUint64(&f.sessionID)
	s.SerializeUint32(&f.clientID)
	s.SerializeBits64(&f.nonce, 64)
	s.SerializeInt64(&f.worldTime, -1000000000000, 1000000000000)
	s.SerializeBits64(&f.frameTick, 48)
	s.SerializeFixed64(&f.serverTime, 24, 8, 0, 65535)

	s.SerializeInt(&f.entitiesCount, 1, 8)
	if s.Err() != nil {
		return s.Err()
	}
	for i := int32(0); i < f.entitiesCount; i++ {
		e := &f.entities[i]
		s.SerializeBits(&e.entityID, 12)
		s.SerializeInt(&e.posX, -16383, 16383)
		s.SerializeInt(&e.posY, -16383, 16383)
		s.SerializeInt(&e.posZ, -16383, 16383)
		s.SerializeBits(&e.yaw, 9)
		s.SerializeBits(&e.pitch, 9)
		s.SerializeInt(&e.velX, -2048, 2047)
		s.SerializeInt(&e.velY, -2048, 2047)
		s.SerializeInt(&e.velZ, -2048, 2047)
		s.SerializeInt(&e.health, 0, 1000)
		s.SerializeInt(&e.weapon, 0, 15)
		s.SerializeBits(&e.damage, 8)
		s.SerializeBool(&e.moving)
		s.SerializeBool(&e.firing)
	}

	s.SerializeInt(&f.statsCount, 0, 80)
	if s.Err() != nil {
		return s.Err()
	}
	for i := int32(0); i < f.statsCount; i++ {
		s.SerializeBits(&f.stats[i].statID, 8)
		s.SerializeInt(&f.stats[i].delta, -512, 511)
	}

	s.SerializeInt(&f.eventType, 0, 3)
	if s.Err() != nil {
		return s.Err()
	}
	switch f.eventType {
	case 1:
		s.SerializeBits(&f.hit.targetID, 12)
		s.SerializeInt(&f.hit.damage, 0, 4095)
		s.SerializeInt(&f.hit.hitKind, 0, 7)
		s.SerializeBool(&f.hit.crit)
	case 2:
		s.SerializeInt(&f.chat.channel, 0, 3)
		s.SerializeBits(&f.chat.speaker, 12)
	case 3:
		s.SerializeBits(&f.pickup.itemID, 10)
		s.SerializeInt(&f.pickup.amount, 0, 255)
	}

	for i := 0; i < 4; i++ {
		s.SerializeUint8(&f.loadout[i])
	}

	s.SerializeInt(&f.playerNameLength, 0, 15)
	if s.Err() != nil {
		return s.Err()
	}
	s.SerializeBytes(f.playerName[:f.playerNameLength])
	s.SerializeInt(&f.payloadLength, 0, 16)
	if s.Err() != nil {
		return s.Err()
	}
	s.SerializeBytes(f.payload[:f.payloadLength])

	s.SerializeCompressedFloat32(&f.aimX, -1.0, 1.0, 0.01)
	s.SerializeCompressedFloat32(&f.aimY, -1.0, 1.0, 0.01)
	s.SerializeCompressedFloat32(&f.aimZ, -1.0, 1.0, 0.01)
	s.SerializeFloat32(&f.recoil)
	s.SerializeFloat64(&f.drift)
	s.SerializeUint128(&f.wideKey)
	s.SerializeInt128(&f.flux, rtFluxMin, rtFluxMax)
	s.SerializeFixed64(&f.ping, 8, 8, 0, 250)

	s.SerializeBits(&f.reservedBits, 4)
	if f.reservedBits != 0 {
		return errRtContract // reserved(4): a read rejects nonzero
	}
	s.SerializeAlign()
	s.SerializeBits(&f.crcHint, 24)
	s.SerializeBool(&f.hasExtra)
	if s.Err() != nil {
		return s.Err()
	}
	if f.hasExtra {
		s.SerializeInt(&f.extra, 0, 255)
	} else {
		s.SerializeInt(&f.idleTicks, 0, 15)
	}
	return s.Err()
}

// ---- pinned instances: test/bench/main.cpp (the golden producer), verbatim ----

func pinRtPacket() rtBenchPacket {
	p := rtBenchPacket{
		a: -37, b: 12345, c: 987654,
		bits7: 97, bits13: 5000, bits23: 1234567,
		flag: true,
		x:    1.5, y: -3.25, z: 100.125,
		big: 0x123456789ABCDEF0,
	}
	for i := 0; i < 17; i++ {
		p.blob[i] = byte(i * 31)
	}
	return p
}

func pinRtInts() rtBenchInts {
	return rtBenchInts{f0: -37, f1: 12345, f2: 987654, f3: 2, f4: -15,
		f5: 777, f6: -2048, f7: 200, f8: -543210, f9: 99}
}

func pinRtBits() rtBenchBits {
	return rtBenchBits{b7: 97, b13: 5000, b23: 1234567, b3: 5,
		b32: 0xDEADBEEF, b11: 1024, b19: 333333, b48: 0xFEDCBA987654}
}

func pinRtMixed() rtBenchMixed {
	var in rtBenchMixed
	in.magic = 0xC0DE
	in.sequence = 52428
	in.ackSequence = 12345
	in.ackBits = 0xA5A5A5A5
	in.sessionID = 0x123456789ABCDEF0
	in.clientID = 0xDEADBEEF
	in.nonce = 0xFEDCBA9876543210
	in.worldTime = -987654321000
	in.frameTick = 0x123456789ABC
	in.serverTime = 12345678
	in.entitiesCount = 8
	for i := 0; i < 8; i++ {
		e := &in.entities[i]
		e.entityID = uint32(2049 + i*17)
		e.posX = int32(-16383 + i*4096)
		e.posY = int32(16383 - i*4096)
		e.posZ = int32(-1 + i*2048)
		e.yaw = uint32(511 - i*64)
		e.pitch = uint32(i * 73)
		e.velX = int32(-2048 + i*512)
		e.velY = int32(2047 - i*512)
		e.velZ = int32(-1024 + i*256)
		e.health = int32(1000 - i*100)
		e.weapon = int32(1 + i)
		e.damage = uint32(0x5A + i)
		e.moving = i%2 == 0
		e.firing = i%3 == 0
	}
	in.statsCount = 80
	for i := 0; i < 80; i++ {
		in.stats[i].statID = uint32((i * 3) % 256)
		in.stats[i].delta = int32(-512 + (i*13)%1024)
	}
	in.eventType = 1 // Hit
	in.hit.targetID = 4095
	in.hit.damage = 4095
	in.hit.hitKind = 7
	in.hit.crit = true
	copy(in.loadout[:], []byte{0x11, 0x22, 0x33, 0x44})
	copy(in.playerName[:], "Rowan_01")
	in.playerNameLength = 8
	copy(in.payload[:], []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04})
	in.payloadLength = 8
	in.aimX = 0.5
	in.aimY = -0.25
	in.aimZ = 0.75
	in.recoil = 1.5
	in.drift = -3.25
	in.wideKey = serialize.Uint128{Lo: 0xFEDCBA9876543210, Hi: 0x0123456789ABCDEF}
	in.flux = serialize.Int128{Lo: 7, Hi: 0x800000000} // 2^99 + 7
	in.ping = 12345
	in.crcHint = 0xABCDEF
	in.hasExtra = true
	in.extra = 200
	return in
}

// ---- vary functions: bench/cpp/bench_main.cpp's rt mappings exactly ----

func varyRtPacket(p *rtBenchPacket, rng uint64) {
	p.a = int32((rng>>8)&63) - 32
	p.b = int32(uint32(rng>>16) & 65535)
	p.c = int32((rng>>24)&0xFFFFF) - 500000
	p.bits7 = uint32(rng) & 127
	p.bits13 = uint32(rng>>3) & 8191
	p.bits23 = uint32(rng>>5) & 8388607
	p.flag = (rng & 1) != 0
	p.x = float32(uint32(rng) & 0xFFFF)
	p.big = rng
	p.blob[0] = byte(rng >> 32)
}

func varyRtInts(f *rtBenchInts, rng uint64) {
	f.f0 = int32((rng>>8)&63) - 32
	f.f1 = int32(uint32(rng>>16) & 65535)
	f.f2 = int32((rng>>24)&0xFFFFF) - 500000
	f.f3 = int32(uint32(rng>>2) & 3)
	f.f4 = int32((rng>>11)&15) - 8
	f.f5 = int32(uint32(rng>>22) & 511)
	f.f6 = int32((rng>>33)&2047) - 1024
	f.f7 = int32(uint32(rng>>40) & 255)
	f.f8 = int32((rng>>30)&0xFFFFF) - 500000
	f.f9 = int32(uint32(rng>>57) & 63)
}

func varyRtBits(f *rtBenchBits, rng uint64) {
	f.b7 = uint32(rng) & 127
	f.b13 = uint32(rng>>3) & 8191
	f.b23 = uint32(rng>>5) & 8388607
	f.b3 = uint32(rng>>29) & 7
	f.b32 = uint32(rng >> 16)
	f.b11 = uint32(rng>>37) & 2047
	f.b19 = uint32(rng>>44) & 524287
	f.b48 = rng & 0xFFFFFFFFFFFF
}

func varyRtMixed(f *rtBenchMixed, rng uint64) {
	f.sequence = uint32(rng>>8) & 65535
	f.ackSequence = int32(uint32(rng>>24) & 65535)
	f.ackBits = uint32(rng >> 16)
	f.sessionID = rng
	f.clientID = uint32(rng >> 32)
	f.nonce = rng ^ 0xA5A5A5A5A5A5A5A5
	f.worldTime = int64((rng>>12)&0xFFFFFFFFF) - 34359738368
	f.frameTick = rng & 0xFFFFFFFFFFFF
	f.serverTime = int64((rng >> 20) & 0x7FFFFF)
	for i := 0; i < 8; i++ {
		e := &f.entities[i]
		e.entityID = uint32((rng >> uint(i)) & 4095)
		e.posX = int32((rng>>uint(i+4))&16383) - 8192
		e.posY = int32((rng>>uint(i+12))&16383) - 8192
		e.health = int32((rng >> uint(i+20)) & 511)
		e.weapon = int32((rng >> uint(i+40)) & 15)
		e.damage = uint32((rng >> uint(i+28)) & 255)
		e.moving = (rng>>uint(i))&1 != 0
	}
	for i := 0; i < 80; i++ {
		f.stats[i].delta = int32((rng>>uint(i&31))&1023) - 512
	}
	f.hit.targetID = uint32((rng >> 6) & 4095)
	f.hit.damage = int32((rng >> 18) & 4095)
	f.hit.hitKind = int32((rng >> 30) & 7)
	f.hit.crit = rng&4 != 0
	f.loadout[0] = uint8(rng >> 56)
	f.playerName[7] = byte(65 + ((rng >> 50) & 15))
	f.payload[0] = uint8(rng >> 48)
	f.aimX = float32(uint32(rng>>2)&255)*(1.0/256.0) - 0.5
	f.aimY = float32(uint32(rng>>10)&255)*(1.0/256.0) - 0.5
	f.aimZ = float32(uint32(rng>>18)&255)*(1.0/256.0) - 0.5
	f.recoil = float32(uint32(rng) & 0xFFFF)
	f.drift = float64(int64((rng>>8)&0xFFFFFF)) * 0.5
	f.wideKey = serialize.Uint128{Lo: rng, Hi: rng >> 1}
	f.flux = serialize.Int128From64(int64(rng >> 16))
	f.ping = int64((rng >> 40) & 0x7FFF)
	f.crcHint = uint32((rng >> 24) & 0xFFFFFF)
	f.extra = int32((rng >> 52) & 255)
}

// ---- the single untimed call sites (§3.2), one pair per shape ----

func rtOnceWritePacket(p *rtBenchPacket, buf []byte) int {
	ws := serialize.NewWriteStream(buf)
	if writeRtPacket(ws, p) != nil {
		return -1
	}
	ws.Flush()
	return int(ws.BytesProcessed())
}

func rtOnceReadPacket(p *rtBenchPacket, buf []byte) bool {
	rs := serialize.NewReadStream(buf)
	return readRtPacket(rs, p) == nil
}

func rtOnceWriteInts(f *rtBenchInts, buf []byte) int {
	ws := serialize.NewWriteStream(buf)
	if writeRtInts(ws, f) != nil {
		return -1
	}
	ws.Flush()
	return int(ws.BytesProcessed())
}

func rtOnceReadInts(f *rtBenchInts, buf []byte) bool {
	rs := serialize.NewReadStream(buf)
	return readRtInts(rs, f) == nil
}

func rtOnceWriteBits(f *rtBenchBits, buf []byte) int {
	ws := serialize.NewWriteStream(buf)
	if writeRtBits(ws, f) != nil {
		return -1
	}
	ws.Flush()
	return int(ws.BytesProcessed())
}

func rtOnceReadBits(f *rtBenchBits, buf []byte) bool {
	rs := serialize.NewReadStream(buf)
	return readRtBits(rs, f) == nil
}

func rtOnceWriteMixed(f *rtBenchMixed, buf []byte) int {
	ws := serialize.NewWriteStream(buf)
	if writeRtMixed(ws, f) != nil {
		return -1
	}
	ws.Flush()
	return int(ws.BytesProcessed())
}

func rtOnceReadMixed(f *rtBenchMixed, buf []byte) bool {
	rs := serialize.NewReadStream(buf)
	return readRtMixed(rs, f) == nil
}

// ---- the timed loops, one symbol per (shape, path) so the §4.1 verdict is a
// direct transitive call count over the loop body. Streams reuse via Reset —
// the runtime's documented no-allocation path, as the gen benches do. ----

//go:noinline
func rtBenchPacketWriteLoop(base *rtBenchPacket, iters int64, rng *uint64) bool {
	ws := serialize.NewWriteStream(gBuffer[:])
	for i := int64(0); i < iters; i++ {
		*rng = benchRng(*rng)
		varyRtPacket(base, *rng)
		ws.Reset(gBuffer[:])
		if writeRtPacket(ws, base) != nil {
			return false
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	return true
}

//go:noinline
func rtBenchPacketReadLoop(out *rtBenchPacket, iters int64, bytesPerOp int) bool {
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for i := int64(0); i < iters; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if readRtPacket(rs, out) != nil {
			return false
		}
		runtime.KeepAlive(out) // every decoded field is observed (§2.7)
		gSink = gSink + 1
	}
	return true
}

//go:noinline
func rtBenchIntsWriteLoop(base *rtBenchInts, iters int64, rng *uint64) bool {
	ws := serialize.NewWriteStream(gBuffer[:])
	for i := int64(0); i < iters; i++ {
		*rng = benchRng(*rng)
		varyRtInts(base, *rng)
		ws.Reset(gBuffer[:])
		if writeRtInts(ws, base) != nil {
			return false
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	return true
}

//go:noinline
func rtBenchIntsReadLoop(out *rtBenchInts, iters int64, bytesPerOp int) bool {
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for i := int64(0); i < iters; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if readRtInts(rs, out) != nil {
			return false
		}
		runtime.KeepAlive(out) // every decoded field is observed (§2.7)
		gSink = gSink + 1
	}
	return true
}

//go:noinline
func rtBenchBitsWriteLoop(base *rtBenchBits, iters int64, rng *uint64) bool {
	ws := serialize.NewWriteStream(gBuffer[:])
	for i := int64(0); i < iters; i++ {
		*rng = benchRng(*rng)
		varyRtBits(base, *rng)
		ws.Reset(gBuffer[:])
		if writeRtBits(ws, base) != nil {
			return false
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	return true
}

//go:noinline
func rtBenchBitsReadLoop(out *rtBenchBits, iters int64, bytesPerOp int) bool {
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for i := int64(0); i < iters; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if readRtBits(rs, out) != nil {
			return false
		}
		runtime.KeepAlive(out) // every decoded field is observed (§2.7)
		gSink = gSink + 1
	}
	return true
}

//go:noinline
func rtBenchMixedWriteLoop(base *rtBenchMixed, iters int64, rng *uint64) bool {
	ws := serialize.NewWriteStream(gBuffer[:])
	for i := int64(0); i < iters; i++ {
		*rng = benchRng(*rng)
		varyRtMixed(base, *rng)
		ws.Reset(gBuffer[:])
		if writeRtMixed(ws, base) != nil {
			return false
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	return true
}

//go:noinline
func rtBenchMixedReadLoop(out *rtBenchMixed, iters int64, bytesPerOp int) bool {
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for i := int64(0); i < iters; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if readRtMixed(rs, out) != nil {
			return false
		}
		runtime.KeepAlive(out) // every decoded field is observed (§2.7)
		gSink = gSink + 1
	}
	return true
}

// ---- the family rt driver: §1.5 oracle gate, then the timed loops ----

func benchRt[T any](name string, baseIters int64, pinned T,
	onceWrite func(*T, []byte) int,
	onceRead func(*T, []byte) bool,
	writeLoop func(*T, int64, *uint64) bool,
	readLoop func(*T, int64, int) bool,
	vary func(*T, uint64)) {

	iters := baseIters

	// oracle 1: the hand-written wire must equal the generated-code golden
	base := pinned
	bytesPerOp := onceWrite(&base, gBuffer[:])
	if bytesPerOp < 0 {
		fail(name, "write of pinned instance failed")
		return
	}
	if !checkGolden(name, gBuffer[:bytesPerOp]) {
		failed = true
		return
	}

	// oracle 2: round-trip write -> read -> re-write -> identical bytes
	var out T
	if !onceRead(&out, gBuffer[:bytesPerOp]) {
		fail(name, "read of pinned instance failed")
		return
	}
	if onceWrite(&out, gTwin[:]) != bytesPerOp ||
		!bytes.Equal(gBuffer[:bytesPerOp], gTwin[:bytesPerOp]) {
		fail(name, "round-trip bytes differ")
		return
	}

	// variant buffers (and proof that variation keeps bytes/op constant)
	rng := uint64(1)
	for k := 0; k < NumVariants; k++ {
		rng = benchRng(rng)
		vary(&base, rng)
		if onceWrite(&base, gVariants[k][:BufferSize]) != bytesPerOp {
			fail(name, "variation changed bytes/op — vary must keep structure fields fixed")
			return
		}
	}

	writeRates := make([]float64, gNumRuns)
	readRates := make([]float64, gNumRuns)

	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		if !writeLoop(&base, iters, &rng) {
			fail(name, "write failed in loop")
			return
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			writeRates[run] = float64(iters) / elapsed
		}
	}

	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		if !readLoop(&out, iters, bytesPerOp) {
			fail(name, "read failed in loop")
			return
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			readRates[run] = float64(iters) / elapsed
		}
	}

	report(name, "write", iters, int64(bytesPerOp), stats(writeRates), "rt")
	report(name, "read", iters, int64(bytesPerOp), stats(readRates), "rt")
}

// ------------------------------------------------------------------------------------------
// family bits (§1.4)
// ------------------------------------------------------------------------------------------

const BitsNumWidths = 16
const BitsBufferSize = 65536

var bitsWidths = [BitsNumWidths]int{1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22} // 227 bits/group

var gBitsBuffer [BitsBufferSize]byte
var gBitsVariants [NumVariants][BitsBufferSize]byte

func bitsMask(width int) uint32 {
	if width == 32 {
		return 0xFFFFFFFF
	}
	return (1 << width) - 1
}

// the per-pass value variation: one LCG step per pass, values from its bits
func varyBitsValues(values *[BitsNumWidths]uint32, rng uint64) {
	for i := 0; i < BitsNumWidths; i++ {
		values[i] = uint32(rng>>i) & bitsMask(bitsWidths[i])
	}
}

// the single untimed WriteBits call site (§3.2)
func bitsWritePass(buffer []byte, values *[BitsNumWidths]uint32) int64 {
	w := serialize.NewBitWriter(buffer)
	for w.BitsAvailable() >= 256 {
		for i := 0; i < BitsNumWidths; i++ {
			w.WriteBits(values[i], bitsWidths[i])
		}
	}
	w.FlushBits()
	return w.BytesWritten()
}

// the single untimed ReadBits call site (§3.2): the buffer must read back
// exactly the values written — the bits family's refusal gate
func bitsReadVerify(buffer []byte, values *[BitsNumWidths]uint32) bool {
	r := serialize.NewBitReader(buffer)
	for r.BitsRemaining() >= 256 {
		for i := 0; i < BitsNumWidths; i++ {
			if r.ReadBits(bitsWidths[i]) != values[i] {
				return false
			}
		}
	}
	return true
}

//go:noinline
func bitpackerWriteLoop(passes int64, bytesPerPass int64, rng *uint64, values *[BitsNumWidths]uint32) bool {
	w := serialize.NewBitWriter(gBitsBuffer[:])
	for pass := int64(0); pass < passes; pass++ {
		*rng = benchRng(*rng)
		varyBitsValues(values, *rng)
		w.Reset(gBitsBuffer[:])
		for w.BitsAvailable() >= 256 {
			for i := 0; i < BitsNumWidths; i++ {
				w.WriteBits(values[i], bitsWidths[i])
			}
		}
		w.FlushBits()
		if w.BytesWritten() != bytesPerPass {
			return false // the bytes_per_op assertion (§2.7)
		}
		gSink = gSink + uint64(w.BytesWritten())
	}
	return true
}

//go:noinline
func bitpackerReadLoop(passes int64) bool {
	r := serialize.NewBitReader(gBitsVariants[0][:])
	for pass := int64(0); pass < passes; pass++ {
		r.Reset(gBitsVariants[pass&(NumVariants-1)][:])
		sum := uint64(0)
		for r.BitsRemaining() >= 256 {
			for i := 0; i < BitsNumWidths; i++ {
				sum += uint64(r.ReadBits(bitsWidths[i]))
			}
		}
		gSink = gSink + sum
	}
	return true
}

func benchBitpacker(basePasses int64) {
	passes := basePasses
	var values [BitsNumWidths]uint32

	rng := uint64(1)
	bytesPerPass := int64(-1)
	for k := 0; k < NumVariants; k++ {
		rng = benchRng(rng)
		varyBitsValues(&values, rng)
		wrote := bitsWritePass(gBitsVariants[k][:], &values)
		if bytesPerPass < 0 {
			bytesPerPass = wrote
		}
		if wrote != bytesPerPass {
			fail("bitpacker", "variation changed bytes/pass — widths are the structure and must stay fixed")
			return
		}
		if !bitsReadVerify(gBitsVariants[k][:], &values) {
			fail("bitpacker", "read-back disagrees with written values — refusing to bench")
			return
		}
	}

	writeRates := make([]float64, gNumRuns)
	readRates := make([]float64, gNumRuns)

	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		if !bitpackerWriteLoop(passes, bytesPerPass, &rng, &values) {
			fail("bitpacker", "bytes/pass changed in the timed loop (§2.7 assertion)")
			return
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			writeRates[run] = float64(passes) / elapsed
		}
	}

	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		if !bitpackerReadLoop(passes) {
			fail("bitpacker", "read loop failed")
			return
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			readRates[run] = float64(passes) / elapsed
		}
	}

	report("bitpacker", "write", passes, bytesPerPass, stats(writeRates), "bits")
	report("bitpacker", "read", passes, bytesPerPass, stats(readRates), "bits")
}
