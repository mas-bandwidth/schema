package elixirtable

// A compressed float's -0 lands as +0 and is written as +0
// (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero). The
// vector is the reference writer's file holding the sign bit alone in the
// compressed field; this leg reads it through its generated load, lands the
// value 0 with the sign bit clear, refuses nothing, and saves the canonical
// bytes the +0 record writes. A plain float's -0 is carried bit-exact and is
// not this vector's subject.
//
// It compiles and runs the generated unit, so it is a functional test:
// without SCHEMA_SLOW it skips by name, and under SCHEMA_REQUIRE_CORPUS=1 a
// missing corpus or toolchain is a failure, never a skip.
import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
)

func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(corpus, vec.File)
			// THE VALUE IS TESTED, NOT A LITERAL: the compressed field must land
			// as the value 0 with the sign bit clear, and the save must write the
			// canonical image the +0 record writes. The header is the form byte,
			// seven reserved zeros, the layout hash and a u32 layout length, so
			// the compressed field sits after it, the record hash and `lead`.
			body := `  data = File.read!(file())
  <<_::binary-size(16), layout_bytes::little-unsigned-32, _::binary>> = data
  at = 16 + 4 + layout_bytes + 8 + 4
  <<_::binary-size(^at), vector_bits::little-unsigned-32, _::binary>> = data
  check(vector_bits == 0x80000000,
    "the vector does not hold a compressed float's -0: 0x#{Integer.to_string(vector_bits, 16)}")
  {tag, values, report} = load(data)
  check(tag == :ok, "the reader refused a compressed -0: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  check(v.lead == 1 and v.trail == 2, "the row moved a neighbour: #{inspect({v.lead, v.trail})}")
  <<value_bits::little-unsigned-32>> = <<v.value::little-float-32>>
  check(value_bits == 0,
    "a compressed float's -0 kept its sign: 0x#{Integer.to_string(value_bits, 16)}")
  check(report.unknown == 0 and report.widened == 0 and report.layout_hash == 0,
    "the read of a compressed -0 reported #{why(report)}")
  want =
    binary_part(data, 0, at) <>
      <<0::little-unsigned-32>> <> binary_part(data, at + 4, byte_size(data) - at - 4)
  saved = T.neg_zero_fixed_save(values)
  check(saved == want, "saving the landed +0 did not write the canonical bytes")
`
			out, err := runRootProbe(t, elixirBin, "FNZ", vec.Root, nil, 0, file, body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
		})
	}
}
