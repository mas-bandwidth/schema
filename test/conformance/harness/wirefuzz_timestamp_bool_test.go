package main

import (
	"encoding/hex"
	"testing"
)

// Historical hostile vector: the complete read must refuse.
func TestWireFuzzTimestampMismatchRefuses(t *testing.T) {
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
	if answer.accepted || len(answer.encoded) != 0 {
		t.Fatalf("hostile wire produced a value: %+v", answer)
	}
}

// A bool byte outside 0 and 1 cannot produce an accepted value.
func TestWireFuzzBoolByteAboveOneRefuses(t *testing.T) {
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
	if answer.accepted || len(answer.encoded) != 0 {
		t.Fatalf("hostile wire produced a value: %+v", answer)
	}
}
