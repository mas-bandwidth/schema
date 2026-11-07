package main

import (
	"encoding/hex"
	"testing"
)

// THE ORACLE'S ANSWER ON TWO MUTANTS THE LEGS ONCE GOT WRONG, pinned so the
// side every leg is compared against cannot drift. Both were reds of the C++
// and C# wire fuzzers on ci-full run 39774840059 (seed 24845619678); the
// legs were wrong and the oracle right, by docs/SPEC-TABLES.md.

// Mutant 688 (corpus seed root_full, pass kind #72): the root's second field
// arrives under kind 36, the timestamp, which is an eight-byte payload a
// reader skips by its width (§2.11, §3). The oracle counts the
// kind_mismatch, steps over eight bytes and reads on; the next field names an
// id this reader cannot name under a kind outside the closed set, which counts
// one unknown and is framing damage (§3: "A kind a reader does not know at all
// is not skippable"). A leg that could not skip kind 36 stopped one field
// early and reported unknown 0.
func TestWireFuzzTimestampKindSkipsEightBytes(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "tabledemo", "RootConfig", false, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString("01010c09676f6c64656e2d76310224360d0225030a00002242040a00007a4305040700000006062d070101080f090d070a0a0000504000000d080f0b0d070c042a00000000000d0eb8010d01b4010e0c0a706c61796572206f6e650f0e0506030102fa1008090300001102f41203d08a1305000efad5feffffff1406c81507409c16098877665544332211170b0000000000000440180e120a0400000000000000000000003f000000001901011a0d511b1e1c1d0e041e021e1c1f0e051e031c002021090600000000000000220d0705040700000000230e0c0d0207030a0000803f000100240e180d020d250403000000260a0000004000072504050000000000000094d3468772c9678ab6ab7fb801d9cb41c07fb38abe08637fd2b1ac025705331732e9139e30c5314fa4edcad59a3e01a5593f2e60bc8765ad9443f2513bc8c136cc69e4e29bbeb5ffc6875c803a62dc9ad72f3de7edc5cd136969b1a27efe1381676743c01fe68181861b638ebaadbcc4f092206cc54cffdb69b9801f526d8babc6af9b2eef88501e750fca90918228b10334f58e3392dc5d185509fe81a9bd10a63f93a80ddb2c8ca66b6e19d19796fcf705bdbaf5278428f9b00c3d8aeb21b93c258e140a5e6fa0a3b5bb8675ce5957d48ac477299ba832b318320d7ec916c4c599f782764e0ad991c5c4aa397792c44925d7235af46fb48025f1eade1ae5c34ae5967084f5064b39e983f77f1331bf24cc8a11f5f028de419a244003aa01f9d131fef61816776ae6b4e78a35d1f9ee2600000000000000")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if answer.report != (Counts{Unknown: 1, KindMismatch: 1, Malformed: true}) {
		t.Fatalf("kind 36 skip: the oracle says %s, want 1,1,0,0,0,true", answer.report)
	}
}

// The tblm1.Msg mutant: Save.force, a bool under a union arm, rides the byte
// 0x6d. A bool byte that is neither 0 nor 1 reads true and is MALFORMED on
// the tolerant read (§6.5); nothing else about the read changes. A leg that
// folded the byte to true and said nothing reported malformed false.
func TestWireFuzzBoolByteAboveOneIsMalformed(t *testing.T) {
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "tblm1", "Msg", false, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString("01010802000000020f030d0b040c04622e6d6405016d00003c13355c198a3b829532c9c69be74dcd287c85bf185e6a097606d7a20d2dc503e4ebeeb3afd85ae70500000000000000")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := root.oracle(wire)
	if err != nil {
		t.Fatal(err)
	}
	if answer.report != (Counts{Unknown: 2, Malformed: true}) {
		t.Fatalf("hostile bool: the oracle says %s, want 2,0,0,0,0,true", answer.report)
	}
}
