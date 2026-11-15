package elixirtable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestFixedDeadBytesAreZero holds the Elixir leg to the dead-byte rule
// (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4,
// §13.10; docs/rulings.json fixed-dead-bytes-are-zero): every dead byte in a
// fixed record is zero on write and a non-zero one refuses the whole load. The
// shared corpus carries one changed file per dead region and a zero control;
// each changed file is refused, each control reads and saves back its own bytes.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "elixir (it runs the generated reader over the dead-byte corpus)")
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			expect, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if expect.Refused == vec.Reads {
				t.Fatalf("%s: the manifest and the corpus list disagree about the verdict", vec.File)
			}
			body := fmt.Sprintf(`  data = File.read!(file())
  result = load(data)
  if %t do
    case result do
      {:ok, values, _report} ->
        check(T.dead_root_fixed_save(values) == data, "the control does not save back byte for byte")
      {:error, reason, _report} ->
        check(false, "the control was refused: #{inspect(reason)}")
    end
  else
    case result do
      {:ok, _values, _report} -> check(false, "a non-zero dead byte was read")
      {:error, _reason, _report} -> check(true, "refused")
    end
  end`, vec.Reads)
			out, err := runRootProbe(t, elixirBin, "DeadBytes", "DeadRoot", nil, 0, filepath.Join(corpus, vec.File), body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}
