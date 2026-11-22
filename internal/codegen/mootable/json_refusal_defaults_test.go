package mootable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
)

const jsonRefusalDefaultsSchema = `package probe

table Root
{
    a int32 = 4
    opt ?int32
    b int8 | min = 0, max = 10
}
`

// TestMooJsonRefusalLeavesExactDefaults holds ruling R4 on <Root>FromJson: a
// refused text leaves the caller's value holding EXACTLY its declared
// defaults. Each text sets the optional field (nil in a fresh value) before
// the read refuses: once for data after the root object, once for a value
// past its declared range. The probe prints every key of the value that a
// fresh M.NewRoot() does not hold, or holds differently; the reset that only
// copied the fresh value's keys over left the optional field behind.
func TestMooJsonRefusalLeavesExactDefaults(t *testing.T) {
	t.Parallel()
	u := mooBase64UnitFromSource(t, jsonRefusalDefaultsSchema)
	dir := generateUnitDir(t, u, nil)
	var b strings.Builder
	b.WriteString(probePrelude)
	fmt.Fprintf(&b, "local M: any = require(\"./%s\")\n", tableModule(u, "Root"))
	b.WriteString(`local function probe(name: string)
	local value = M.NewRoot()
	local report: any = {}
	local ok = M.RootFromJson(value, inputs[name], report)
	local fresh = M.NewRoot()
	local off = {}
	for k, v in value do
		if fresh[k] ~= v and type(v) ~= "table" then
			table.insert(off, tostring(k))
		end
	end
	for k in fresh do
		if value[k] == nil then
			table.insert(off, tostring(k))
		end
	end
	table.sort(off)
	hexs(name .. " " .. (if ok then "read" else "refused") .. " [" .. table.concat(off, ",") .. "]")
end
probe("trailing")
probe("range")
`)
	if err := os.WriteFile(filepath.Join(dir, "main.moo"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{
		"trailing": []byte(`{"a":9,"opt":5} 1`),
		"range":    []byte(`{"a":9,"opt":5,"b":11}`),
	}
	got := strings.Join(decodeHexLines(t, moorun.Run(t, dir, "main.moo", inputs)), "\n")
	if want := "trailing refused []\nrange refused []"; got != want {
		t.Fatalf("a refused FromJson left keys off their declared defaults:\ngot  %q\nwant %q", got, want)
	}
}
