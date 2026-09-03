// variantgen — the bench_mixed variant-data tool (issue #191).
//
//	cd bench/tools/variantgen && go run .          # or: make bench-variants
//
// THE DESIGN THIS SERVES. Nine language harnesses used to carry hand-written
// BenchMixed shape code — a pinned initializer, an LCG vary function, a sink
// fold — one transcription per language, rewritten by hand on every shape
// change. That is the divergence class the owner named: nine hand-written
// approximations of one benchmark. Here the shape knowledge lives in exactly
// two places instead: the DATA this tool commits, and the schema-GENERATED
// codecs. Every language's driver is serialize-blind — it names no field, and
// so it cannot diverge in what it measures.
//
// WHAT IT EMITS. 64 BenchMixed wire buffers, concatenated at a fixed stride
// into bench/corpus/variants/bench_mixed.variants.bin:
//
//   - variant 0 IS the pinned golden instance. The tool asserts its bytes
//     equal testdata/wire/bench_mixed.bin and refuses to write otherwise, so
//     the variant file is bound to the corpus golden by construction and
//     every driver re-checks that binding before it benches.
//   - variants 1..63 vary VALUE fields only, through the BENCH-STANDARD §2.7
//     Knuth-MMIX LCG, seeded at 1 and stepped once per variant. STRUCTURE
//     fields — the two array counts, the two used lengths, the union tag, the
//     `if` gate — are pinned and never touched, so every variant is exactly
//     as long as the golden (438 bytes) and bytes/op cannot move under
//     variation.
//
// NO INDEX, BY CONSTRUCTION. The records are fixed-width because §2.7 makes
// fixed width a gate, not a hope: a driver derives the record size as
// filesize/64 in two lines in any language, and "every variant is 438 bytes"
// reduces to one file-length check. An offsets index would only earn its
// keep for a variable-width shape, and a variable-width shape would first
// have to repeal §2.7's structure-pinning rule.
//
// THE CODEC CHOICE IS IMMATERIAL. The wire is cross-language conformant and
// the golden assertion pins it; the generated Go codec is used here because
// the tool is a Go tool (the estate's tooling rule), not because Go is
// privileged. Any language's generated codec would emit the same bytes, and
// the assertion would catch it if it did not.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"bench"

	serialize "github.com/mas-bandwidth/serialize.go"
)

// NumVariants is BENCH-STANDARD §2.7's rotating-buffer count: 64. One buffer
// is memorized by the branch predictor and the caches; 64 is what every
// runner in the estate rotates through.
const NumVariants = 64

// BufferSize matches the runners' write buffer: a multiple of 8 bytes (the
// qword-flush contract), with slack over the 438-byte shape.
const BufferSize = 4096

// benchRng is BENCH-STANDARD §2.7's LCG — Knuth MMIX, the same constants
// every runner in the estate uses.
func benchRng(rng uint64) uint64 {
	return rng*6364136223846793005 + 1442695040888963407
}

// pinGenMixed — the pinned BenchMixed instance, transcribed from
// test/bench/main.cpp (the golden producer). STRUCTURE fields (the two array
// counts, the two used lengths, the union tag, the `if` gate) are set here
// and never touched by varyGenMixed, so bytes/op is constant (§2.7).
func pinGenMixed() bench.BenchMixed {
	var in bench.BenchMixed
	in.Sequence = 52428
	in.AckSequence = 12345
	in.AckBits = 0xA5A5A5A5
	in.SessionId = 0x123456789ABCDEF0
	in.ClientId = 0xDEADBEEF
	in.Nonce = 0xFEDCBA9876543210
	in.WorldTime = -987654321000
	in.FrameTick = 0x123456789ABC
	in.ServerTime = 12345678
	in.EntitiesCount = 8
	for i := 0; i < 8; i++ {
		e := &in.Entities[i]
		e.EntityId = uint32(2049 + i*17)
		e.PosX = int32(-16383 + i*4096)
		e.PosY = int32(16383 - i*4096)
		e.PosZ = int32(-1 + i*2048)
		e.Yaw = uint32(511 - i*64)
		e.Pitch = uint32(i * 73)
		e.VelX = int32(-2048 + i*512)
		e.VelY = int32(2047 - i*512)
		e.VelZ = int32(-1024 + i*256)
		e.Health = int32(1000 - i*100)
		e.Weapon = bench.MixedWeapon(1 + i)
		e.Damage = bench.MixedDamage(0x5A + i)
		e.Moving = i%2 == 0
		e.Firing = i%3 == 0
	}
	in.StatsCount = 80
	for i := 0; i < 80; i++ {
		in.Stats[i].StatId = uint32((i * 3) % 256)
		in.Stats[i].Delta = int32(-512 + (i*13)%1024)
	}
	in.GameEvent.Type = bench.MixedEventTypeHit
	in.GameEvent.Hit.TargetId = 4095
	in.GameEvent.Hit.Damage = 4095
	in.GameEvent.Hit.HitKind = 7
	in.GameEvent.Hit.Crit = true
	copy(in.Loadout[:], []byte{0x11, 0x22, 0x33, 0x44})
	copy(in.PlayerName[:], "Rowan_01")
	in.PlayerNameLength = 8
	copy(in.Payload[:], []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04})
	in.PayloadLength = 8
	in.AimX = 0.5
	in.AimY = -0.25
	in.AimZ = 0.75
	in.Recoil = 1.5
	in.Drift = -3.25
	in.WideKey = serialize.Uint128{Lo: 0xFEDCBA9876543210, Hi: 0x0123456789ABCDEF}
	in.Flux = serialize.Int128{Lo: 7, Hi: 0x800000000} // 2^99 + 7
	in.Ping = 12345
	in.CrcHint = 0xABCDEF
	in.HasExtra = true
	in.Extra = 200
	return in
}

// varyGenMixed — the §2.7 LCG field mapping. VALUE fields only: every array
// count, used length, union tag and branch gate is STRUCTURE and stays where
// pinGenMixed put it. All 8 entities vary; the 80 stats vary Delta (StatId
// stays pinned) — the family convention of varying a representative subset,
// stated out loud. Every assignment stays inside its field's declared wire
// range, so a write can never fail on a range check.
func varyGenMixed(f *bench.BenchMixed, rng uint64) {
	f.Sequence = uint32(rng>>8) & 65535
	f.AckSequence = int32(uint32(rng>>24) & 65535)
	f.AckBits = uint32(rng >> 16)
	f.SessionId = rng
	f.ClientId = uint32(rng >> 32)
	f.Nonce = rng ^ 0xA5A5A5A5A5A5A5A5
	f.WorldTime = int64((rng>>12)&0xFFFFFFFFF) - 34359738368 // within +/-1e12
	f.FrameTick = rng & 0xFFFFFFFFFFFF
	f.ServerTime = int32((rng >> 20) & 0x7FFFFF) // raw Q24.8 <= 65535 << 8
	for i := 0; i < 8; i++ {
		e := &f.Entities[i]
		e.EntityId = uint32((rng >> uint(i)) & 4095)
		e.PosX = int32((rng>>uint(i+4))&16383) - 8192
		e.PosY = int32((rng>>uint(i+12))&16383) - 8192
		e.Health = int32((rng >> uint(i+20)) & 511) // within [0, 1000]
		e.Weapon = bench.MixedWeapon((rng >> uint(i+40)) & 15)
		e.Damage = bench.MixedDamage((rng >> uint(i+28)) & 255)
		e.Moving = (rng>>uint(i))&1 != 0
	}
	for i := 0; i < 80; i++ {
		f.Stats[i].Delta = int32((rng>>uint(i&31))&1023) - 512
	}
	f.GameEvent.Hit.TargetId = uint32((rng >> 6) & 4095)
	f.GameEvent.Hit.Damage = int32((rng >> 18) & 4095)
	f.GameEvent.Hit.HitKind = int32((rng >> 30) & 7)
	f.GameEvent.Hit.Crit = rng&4 != 0
	f.Loadout[0] = uint8(rng >> 56)
	f.PlayerName[7] = byte(65 + ((rng >> 50) & 15)) // stays ASCII, never NUL
	f.Payload[0] = uint8(rng >> 48)
	f.AimX = float32(uint32(rng>>2)&255)*(1.0/256.0) - 0.5 // within [-1, 1]
	f.AimY = float32(uint32(rng>>10)&255)*(1.0/256.0) - 0.5
	f.AimZ = float32(uint32(rng>>18)&255)*(1.0/256.0) - 0.5
	f.Recoil = float32(uint32(rng) & 0xFFFF)
	f.Drift = float64(int64((rng>>8)&0xFFFFFF)) * 0.5
	f.WideKey = serialize.Uint128{Lo: rng, Hi: rng >> 1}
	f.Flux = serialize.Int128From64(int64(rng >> 16)) // well within +/-2^100
	f.Ping = uint16((rng >> 40) & 0x7FFF)             // raw UQ8.8 <= 250 << 8
	f.CrcHint = uint32((rng >> 24) & 0xFFFFFF)
	f.Extra = int32((rng >> 52) & 255)
}

func encode(buffer []byte, value *bench.BenchMixed) ([]byte, error) {
	stream := serialize.NewWriteStream(buffer)
	if err := bench.WriteBenchMixed(stream, value); err != nil {
		return nil, err
	}
	stream.Flush()
	return buffer[:stream.BytesProcessed()], nil
}

func main() {
	out := flag.String("out", "../../corpus/variants/bench_mixed.variants.bin", "variant data file to write")
	golden := flag.String("golden", "../../../testdata/wire/bench_mixed.bin", "the pinned wire golden variant 0 must equal")
	flag.Parse()

	// variant 0: the pinned instance, byte-equal to the golden or nothing ships.
	base := pinGenMixed()
	buffer := make([]byte, BufferSize)
	first, err := encode(buffer, &base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "variantgen: write of the pinned instance failed: %v\n", err)
		os.Exit(1)
	}
	want, err := os.ReadFile(*golden)
	if err != nil {
		fmt.Fprintf(os.Stderr, "variantgen: %v\n", err)
		os.Exit(1)
	}
	if !bytes.Equal(first, want) {
		fmt.Fprintf(os.Stderr, "variantgen: variant 0 is not the golden — %d bytes vs %s's %d; refusing to write variant data that is not bound to the corpus\n",
			len(first), *golden, len(want))
		os.Exit(1)
	}
	size := len(first)

	data := make([]byte, 0, NumVariants*size)
	data = append(data, first...)

	// variants 1..63: the §2.7 LCG, seeded at 1, stepped once per variant and
	// applied cumulatively to the pinned instance. Structure is untouched, so
	// every record must come out exactly `size` bytes — that is checked here
	// and re-checked by every driver's pre-bench gate.
	rng := uint64(1)
	for k := 1; k < NumVariants; k++ {
		rng = benchRng(rng)
		varyGenMixed(&base, rng)
		record, err := encode(buffer, &base)
		if err != nil {
			fmt.Fprintf(os.Stderr, "variantgen: write of variant %d failed: %v\n", k, err)
			os.Exit(1)
		}
		if len(record) != size {
			fmt.Fprintf(os.Stderr, "variantgen: variant %d is %d bytes, not %d — variation moved a STRUCTURE field (§2.7)\n", k, len(record), size)
			os.Exit(1)
		}
		data = append(data, record...)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "variantgen: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "variantgen: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "variantgen: %s — %d variants x %d bytes = %d bytes; variant 0 == %s\n",
		*out, NumVariants, size, len(data), *golden)
}
