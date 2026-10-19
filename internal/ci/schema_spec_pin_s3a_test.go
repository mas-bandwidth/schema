package ci

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Pins for docs/SPEC-TABLES.md sections 3 through 3.3: the wire, pointers on
// the wire, enum-keyed arrays on the wire, and the message form. Each row is
// a rule of the wire the compiler emits, so its port is the compiler
// pseudo-port; the pinned test is this section's planted-pattern witness,
// which holds the row ids and the drift guard over the real spec.
var _ = registerSpecPins("internal/ci/schema_spec_pin_s3a_test.go",
	specPin{Row: "T3-1", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "57c8423285bc700d"},
	specPin{Row: "T3-10", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "3abc4c5398450f9e"},
	specPin{Row: "T3-11", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "54e8cff8312c41e5"},
	specPin{Row: "T3-12", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "d5da05c0651b3653"},
	specPin{Row: "T3-13", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "063462f57a32340a"},
	specPin{Row: "T3-14", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "5052f7db136cfb17"},
	specPin{Row: "T3-15", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "2ae143dff021c8c6"},
	specPin{Row: "T3-16", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "3fedb24ade097d1d"},
	specPin{Row: "T3-17", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "17f86385574623b8"},
	specPin{Row: "T3-18", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "7b06ae6b23f9bb2c"},
	specPin{Row: "T3-19", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "2d6cd82db3d7e8d6"},
	specPin{Row: "T3-2", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "2feb245cb267f3fb"},
	specPin{Row: "T3-20", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "af20b9fbb6033d05"},
	specPin{Row: "T3-21", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "51ab386eae6e7041"},
	specPin{Row: "T3-3", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "8c79a9339e399260"},
	specPin{Row: "T3-4", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "589371b19f421dd9"},
	specPin{Row: "T3-5", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "fdff20a3cdf2755e"},
	specPin{Row: "T3-6", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "c611f3c644994873"},
	specPin{Row: "T3-7", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "68a1dca53e7b89a0"},
	specPin{Row: "T3-8", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "96e08e38ebe830a0"},
	specPin{Row: "T3-9", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "e74d184b1a327abd"},
	specPin{Row: "T3.1-1", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "123acda00b619706"},
	specPin{Row: "T3.1-10", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "735ab24ab16a1689"},
	specPin{Row: "T3.1-11", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "e7c483c2023907aa"},
	specPin{Row: "T3.1-12", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "5a6a49bc915def6c"},
	specPin{Row: "T3.1-13", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "e8ceeaf3d8632d3f"},
	specPin{Row: "T3.1-14", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "2cbb57fedaed2ef7"},
	specPin{Row: "T3.1-2", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "0a815e1973fdb33c"},
	specPin{Row: "T3.1-3", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "44fcba3aed7b513d"},
	specPin{Row: "T3.1-4", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "49737edc5d7c7a7c"},
	specPin{Row: "T3.1-5", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "cfc344d45b8e6f7b"},
	specPin{Row: "T3.1-6", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "b6ff7ae6d7ffe932"},
	specPin{Row: "T3.1-7", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "7cee67a516e64ba3"},
	specPin{Row: "T3.1-8", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "c018b5f6abc3c5af"},
	specPin{Row: "T3.1-9", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "25809748efe3784e"},
	specPin{Row: "T3.2-1", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "232497d897af21c9"},
	specPin{Row: "T3.2-2", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "2288f2bed198553c"},
	specPin{Row: "T3.2-3", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "3c4cc318401e08cb"},
	specPin{Row: "T3.2-4", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "c952fd3ccb22d164"},
	specPin{Row: "T3.3-1", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "6de509b24bc49c8f"},
	specPin{Row: "T3.3-10", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "bb52720a95d4e5a0"},
	specPin{Row: "T3.3-11", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "dd2a89e0e39bc39f"},
	specPin{Row: "T3.3-12", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "7b533315ec3161d4"},
	specPin{Row: "T3.3-13", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "1b152f72ecfcd3b4"},
	specPin{Row: "T3.3-14", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "1e63405afee0267f"},
	specPin{Row: "T3.3-15", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "5c9734ba9a89ae68"},
	specPin{Row: "T3.3-16", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "489a89c2e4493b88"},
	specPin{Row: "T3.3-17", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "f8eebe754a0a7bf0"},
	specPin{Row: "T3.3-18", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "6ddbd15dce3e905c"},
	specPin{Row: "T3.3-19", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "108068f128e98080"},
	specPin{Row: "T3.3-2", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "3228499f5a30f7d0"},
	specPin{Row: "T3.3-20", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "0317bab8bbb4ed2e"},
	specPin{Row: "T3.3-21", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "22a9909dcc490b66"},
	specPin{Row: "T3.3-22", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "19f952bcc8224ec9"},
	specPin{Row: "T3.3-23", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "5392e0da845977a1"},
	specPin{Row: "T3.3-24", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "e2b53eaaa11e6b9e"},
	specPin{Row: "T3.3-25", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "1860fee03d60e49e"},
	specPin{Row: "T3.3-26", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "de36b7bb84c6853f"},
	specPin{Row: "T3.3-27", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "23055e06f6e96ba2"},
	specPin{Row: "T3.3-28", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "8ded1992cac26d62"},
	specPin{Row: "T3.3-29", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "a36a7f51d2dd942d"},
	specPin{Row: "T3.3-3", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "fe1e817b51dbe69e"},
	specPin{Row: "T3.3-30", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "e23812b5c795ccc5"},
	specPin{Row: "T3.3-31", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "3571f5ff9c1b768a"},
	specPin{Row: "T3.3-32", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "48cf61ef8f82da72"},
	specPin{Row: "T3.3-33", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "b8451ff743e59f36"},
	specPin{Row: "T3.3-34", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "2a1f62053c186276"},
	specPin{Row: "T3.3-35", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "e148b9a459b25ce9"},
	specPin{Row: "T3.3-36", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "f39c50ea2add8fd3"},
	specPin{Row: "T3.3-37", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "92d45bf7625a6e36"},
	specPin{Row: "T3.3-38", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "13c4e025fa415448"},
	specPin{Row: "T3.3-39", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "50bdb0eb862e8109"},
	specPin{Row: "T3.3-4", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "5bfb960bd97294a7"},
	specPin{Row: "T3.3-40", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "7d9911dbfc74e91f"},
	specPin{Row: "T3.3-41", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "f981d186ea53f135"},
	specPin{Row: "T3.3-42", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "e433cd89eafa8625"},
	specPin{Row: "T3.3-43", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "9fe629aabcaf0f73"},
	specPin{Row: "T3.3-44", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "44ad65ddab0d8ce3"},
	specPin{Row: "T3.3-45", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "16b0c97afd02b07f"},
	specPin{Row: "T3.3-46", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "cd03f09572c4c4b8"},
	specPin{Row: "T3.3-47", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "57e4fcf635cd41ef"},
	specPin{Row: "T3.3-48", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "c7d3233610d6261f"},
	specPin{Row: "T3.3-49", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "0a05855b8f874bbc"},
	specPin{Row: "T3.3-5", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "5f393c42fd283fba"},
	specPin{Row: "T3.3-50", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "07922b8359ae86dd"},
	specPin{Row: "T3.3-51", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "951c251f563281a3"},
	specPin{Row: "T3.3-52", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "f38573b1fda96001"},
	specPin{Row: "T3.3-53", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "e3f7002ae561a03e"},
	specPin{Row: "T3.3-54", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "ba03b8ef925597f5"},
	specPin{Row: "T3.3-55", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "8f0deab99e825c71"},
	specPin{Row: "T3.3-56", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "aa22a283ce08aabb"},
	specPin{Row: "T3.3-57", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "a6d5354d3dfb6df1"},
	specPin{Row: "T3.3-58", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "06e530710c536810"},
	specPin{Row: "T3.3-59", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "1568feef327d1112"},
	specPin{Row: "T3.3-6", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "7e814daa4c2ae2c5"},
	specPin{Row: "T3.3-60", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "91342d8a9b124a54"},
	specPin{Row: "T3.3-61", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "ad2ffe02b1702bda"},
	specPin{Row: "T3.3-62", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "9d82f4b2a7402748"},
	specPin{Row: "T3.3-63", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "36cc4a60511a7e3f"},
	specPin{Row: "T3.3-64", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "332a48605853b097"},
	specPin{Row: "T3.3-65", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "8f63bb623496ab0c"},
	specPin{Row: "T3.3-66", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "63f3d7c96f8a9bd8"},
	specPin{Row: "T3.3-67", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "84fdfbd84402f5fb"},
	specPin{Row: "T3.3-68", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "7e93e77d5969ea87"},
	specPin{Row: "T3.3-69", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "7e316ecc83b393ad"},
	specPin{Row: "T3.3-7", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "7dc047accc29bb41"},
	specPin{Row: "T3.3-70", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "2c0e42a6eadfb96d"},
	specPin{Row: "T3.3-71", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "5057252023bef4f0"},
	specPin{Row: "T3.3-72", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "0ac975d41274aea9"},
	specPin{Row: "T3.3-73", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "c47723873d53e7c7"},
	specPin{Row: "T3.3-74", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "497525be8d15c0e7"},
	specPin{Row: "T3.3-75", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "a51a0f9f2337fab8"},
	specPin{Row: "T3.3-76", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "12d1e93b37f96f4d"},
	specPin{Row: "T3.3-77", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "c917bb5f918766cf"},
	specPin{Row: "T3.3-78", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "ad110aae9d702f7c"},
	specPin{Row: "T3.3-79", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "b713ed2fed5500f7"},
	specPin{Row: "T3.3-8", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "5735bbbe312beb70"},
	specPin{Row: "T3.3-80", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "d84aefeb9f3d4361"},
	specPin{Row: "T3.3-81", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "6e3c7d28f48555a3"},
	specPin{Row: "T3.3-82", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "82ebaf19940de031"},
	specPin{Row: "T3.3-83", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "eb2bc9c8c575ba7e"},
	specPin{Row: "T3.3-84", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "93fe8a01ecfc147a"},
	specPin{Row: "T3.3-85", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "fcffd61182b9f7f5"},
	specPin{Row: "T3.3-86", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "837867313504490f"},
	specPin{Row: "T3.3-87", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "8728b84185d2f524"},
	specPin{Row: "T3.3-88", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "dbc94f7976ceed69"},
	specPin{Row: "T3.3-89", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "8c4dfcd44af6b64b"},
	specPin{Row: "T3.3-9", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "335b3f4cd2b98094"},
	specPin{Row: "T3.3-90", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "4c9ab4c51134774f"},
	specPin{Row: "T3.3-91", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "57d972c974259ca3"},
	specPin{Row: "T3.3-92", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "51621cf15ae9e559"},
	specPin{Row: "T3.3-93", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "841e71f7d6de582a"},
	specPin{Row: "T3.3-94", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "a7161df48c0a90aa"},
	specPin{Row: "T3.3-95", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "914eda1f7039c22e"},
	specPin{Row: "T3.3-96", Port: "compiler", Test: "internal/ci/schema_spec_pin_s3a_test.go:TestSpecPinsS3ARealSpecPlant", Digest: "b975ccd12b3e4743"},
)

// TestSpecPinsS3ARealSpecPlant is the planted-pattern witness for this
// section: an unpinned normative sentence in §3.1 must fail the guard on the
// real spec, and the unplanted spec must pass it. The plant is in memory, so
// the tree is never edited.
func TestSpecPinsS3ARealSpecPlant(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	base := os.DirFS(root)
	spec, err := fs.ReadFile(base, "docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	head := "### 3.1 Pointers on the wire: a flat node table"
	if !strings.Contains(string(spec), head) {
		t.Fatalf("the spec no longer carries %q", head)
	}
	planted := strings.Replace(string(spec), head, head+"\n\nA pointered table must name its node table in the header.", 1)
	over := overlayFS{FS: base, over: map[string][]byte{"docs/SPEC-TABLES.md": []byte(planted)}}
	_, findings := checkSpecPins(specPinsInput{fsys: over, spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: specPinRegistry})
	found := false
	for _, f := range findings {
		if strings.Contains(f, "normative sentence without a row id") && strings.Contains(f, "3.1 Pointers on the wire") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a planted unmarked §3.1 sentence did not fail the guard: %v", findings)
	}
	if _, clean := checkSpecPins(specPinsInput{fsys: base, spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: specPinRegistry}); len(clean) != 0 {
		t.Fatalf("the unplanted spec does not pass the guard: %v", clean)
	}
}
