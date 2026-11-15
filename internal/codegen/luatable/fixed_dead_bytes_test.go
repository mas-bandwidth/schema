package luatable

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO (docs/FIXED-FORM-ALGORITHM.md
// §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4, §13.10; docs/rulings.json
// fixed-dead-bytes-are-zero). The shared corpus (internal/fixedvector.
// DeadBytes) carries one control whose dead bytes are the writer's zeroes and
// one changed file per dead region: an absent optional's payload, a counted
// array's slots past its count, a text's or bytes' units past its used length,
// and an inactive union arm. The generated Lua load refuses every changed file
// whole, and the control reads and saves back byte for byte.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedDeadBytesAreZero(t *testing.T) {
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime over the reference corpus)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	unit := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "DeadBytes.schema"))

	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			g := luaEvoGen(t, unit, nil)
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused == vec.Reads {
				t.Fatalf("%s: the manifest says refused=%v, DeadBytes says reads=%v", vec.File, want.Refused, vec.Reads)
			}
			wire, err := os.ReadFile(filepath.Join(corpus, vec.File))
			if err != nil {
				t.Fatal(err)
			}
			if !vec.Reads {
				// A refusal refuses the whole load and the one-record read.
				// Lua's loads allocate their result, so a refusal returns no
				// record at all.
				main := fmt.Sprintf(`local T = require(%q)
local I = require("inputs")
local ok, rows = pcall(T.FixedLoad, %q, I.file)
if ok then print("reads " .. #rows) else print("refused") end
local ok1, row = pcall(T.ReadFixed, %q, I.file)
if ok1 then print("read reads") else print("read refused") end
`, g.mod, vec.Root, vec.Root)
				got := strings.Split(g.run(t, main, map[string][]byte{"file": wire}), "\n")
				if len(got) != 2 || got[0] != "refused" || got[1] != "read refused" {
					t.Fatalf("%s: got %q, want the whole load and the one-record read refused", vec.File, got)
				}
				return
			}
			// The control reads, and the generated save reproduces the
			// reference writer's image byte for byte.
			main := fmt.Sprintf(`local T = require(%q)
local I = require("inputs")
local rows = T.FixedLoad(%q, I.file)
local out = T.FixedSave(%q, rows)
io.write((out:gsub(".", function(c) return string.format("%%02x", c:byte()) end)))
`, g.mod, vec.Root, vec.Root)
			back, err := hex.DecodeString(g.run(t, main, map[string][]byte{"file": wire}))
			if err != nil {
				t.Fatalf("%s: the save back: %v", vec.File, err)
			}
			if !bytes.Equal(back, wire) {
				t.Fatalf("%s: the save back differs from the reference writer's image", vec.File)
			}
		})
	}
}
