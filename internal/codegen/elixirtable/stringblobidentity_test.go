package elixirtable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Equal strings in distinct slots must remain distinct blob nodes. An
// explicitly shared node must instead be refused by wire and text writers.
func TestElixirStringBlobIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Elixir string blob identity")
	u := unitFrom(t, `package stringidentity
 table Pair {
  first *string
  second *string
 }
 `)
	probe := `
 alias Stringidentity.Table, as: T
 {:ok, value, _} = T.from_json("Pair", ~s({"first":"same","second":"same"}))
 {:ok, wire} = T.save("Pair", value)
 {:ok, loaded, report} = T.load("Pair", wire)
 root = loaded
 unless report.reason == "" and root["first"] == "same" and root["second"] == "same", do: raise("equal strings did not round trip")
 {:ok, json} = T.to_json("Pair", loaded)
 {:ok, _, _} = T.from_json("Pair", json)
 shared = Map.put(root, "second", {:blob_shared, root["first"]})
 unless T.save("Pair", shared) == {:error, :blob_shared}, do: raise("wire accepted shared string")
 unless T.to_json("Pair", shared) == {:error, :blob_shared}, do: raise("text accepted shared string")
 IO.puts("STRING_BLOB_IDENTITY_OK")
 `
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe)
	if err != nil || !strings.Contains(out, "STRING_BLOB_IDENTITY_OK") || strings.Contains(out, "warning:") {
		t.Fatalf("string blob identity: %v\n%s", err, out)
	}
}
