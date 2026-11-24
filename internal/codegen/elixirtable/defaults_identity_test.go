package elixirtable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestSparseDefaultsKeepDeclarationIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain")
	source := filepath.Join(t.TempDir(), "Defaults.schema")
	const schema = `package defaultidentity
table First { count int32 = 3 | min = 2, max = 4 }
table Second { count int32 = 9 | min = 8, max = 10 }
`
	if err := os.WriteFile(source, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, source)
	files := generateElixir(t, u)
	const probe = `
for {root, declared} <- [{"First", 3}, {"Second", 9}] do
  {:ok, expected} = Defaultidentity.Table.save(root, %{"count" => declared})
  case Defaultidentity.Table.save(root, %{}) do
    {:ok, ^expected} -> :ok
    other -> raise "#{root} sparse save disagrees with explicit declared default: #{inspect(other)}"
  end
end
`
	if out, err := runArmProbe(t, elixirBinary(t), files, probe); err != nil {
		t.Fatalf("declaration default identity: %v\n%s", err, out)
	}
}
