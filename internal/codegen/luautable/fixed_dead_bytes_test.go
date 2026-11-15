package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO (docs/FIXED-FORM-ALGORITHM.md
// §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4, §13.10; docs/rulings.json
// fixed-dead-bytes-are-zero). The shared vectors are fixedvector.DeadBytes.
// A file the list marks refused is refused by the generated load — the verdict
// only, never the reason — and a value the caller still holds is unchanged.
// A control reads and saves back byte for byte. This leg's read returns fresh
// values or nil; nil is the untouched destination.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	u := luauVersionUnit(t, "DeadBytes")
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("generate DeadBytes: %v", err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("generate DeadBytes: %v", err)
	}
	maps.Copy(files, tables)

	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused == vec.Reads {
				t.Fatalf("%s: the manifest says refused=%v, DeadBytes says reads=%v", vec.File, want.Refused, vec.Reads)
			}
			dir := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			probe := fmt.Sprintf(luauDeadBytesProbe, "DeadBytesTable", vec.Root)
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			wire := luauCorpusFile(t, corpus, vec.File)
			out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"file": wire})
			dest, read, saved := luauDeadBytesVerdict(t, vec.File, out)
			if !dest {
				t.Fatalf("%s: the load changed a value the caller still holds", vec.File)
			}
			if !vec.Reads {
				if read {
					t.Fatalf("%s: the load read the file, want a refusal", vec.File)
				}
				if saved {
					t.Fatalf("%s: a refusal saved the file", vec.File)
				}
				return
			}
			if !read {
				t.Fatalf("%s: the control did not read", vec.File)
			}
			if !saved {
				t.Fatalf("%s: the control did not save back byte for byte", vec.File)
			}
		})
	}
}

// luauDeadBytesVerdict reads the probe's three lines. A missing SAVE line is
// an unsaved load, which a refusal owes and a control does not.
func luauDeadBytesVerdict(t *testing.T, file string, out []byte) (dest, read, saved bool) {
	t.Helper()
	seen := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		key, val, ok := strings.Cut(line, " ")
		if !ok || (key != "DEST" && key != "READ" && key != "SAVE") || seen[key] {
			t.Fatalf("%s: the probe printed %q\n%s", file, line, out)
		}
		seen[key] = true
		switch key {
		case "DEST":
			dest = val == "true"
		case "READ":
			read = val == "true"
		case "SAVE":
			saved = val == "true"
		}
	}
	if !seen["DEST"] || !seen["READ"] {
		t.Fatalf("%s: the probe printed no verdict:\n%s", file, out)
	}
	return dest, read, saved
}

const luauDeadBytesProbe = `--!strict
local T = require("./%s")
local F = require("./schema_fixed")
local inputs = require("./inputs")
local fixed = { hash = T.%[2]sFixedHash, recordBytes = T.%[2]sFixedRecordBytes, layout = T.%[2]sFixedLayout,
	lineage = T.%[2]sFixedLineage, floor = T.%[2]sFixedFloor }

local function snap(v: any, path: string, out: { string })
	if typeof(v) == "buffer" then
		local parts = table.create(buffer.len(v))
		for i = 0, buffer.len(v) - 1 do
			parts[i + 1] = string.format("%%02x", buffer.readu8(v, i))
		end
		table.insert(out, path .. "=buf:" .. table.concat(parts))
		return
	end
	if typeof(v) ~= "table" then
		table.insert(out, path .. "=" .. tostring(v))
		return
	end
	local keys: { string } = {}
	for k in v do
		table.insert(keys, tostring(k))
	end
	table.sort(keys)
	for _, k in keys do
		local child = if path == "" then k else path .. "." .. k
		snap((v :: any)[k], child, out)
	end
end

local function picture(v: any): string
	local out: { string } = {}
	snap(v, "", out)
	return table.concat(out, "\n")
end

local caller = T.New%[2]s()
caller.ArrCount = 2
caller.Arr[1] = 9
caller.Arr[2] = 8
caller.ObjsCount = 1
caller.Objs[1].X = 3
caller.TextLength = 3
buffer.writestring(caller.Text, 0, "xyz")
caller.BlobLength = 1
buffer.writeu8(caller.Blob, 0, 0x5A)
caller.Pick.Type = 1
caller.Pick.Narrow = 4
caller.OptPresent = false
caller.Opt.N = 77
local before = picture(caller)
local report: any = {}
local values = F.read(T.%[2]sDescriptor, fixed, inputs.file, report)
local after = picture(caller)
print("DEST " .. tostring(after == before))
local read = values ~= nil and #values == 1
print("READ " .. tostring(read))
if values ~= nil then
	local back = F.save(T.%[2]sDescriptor, fixed, values)
	local same = buffer.len(back) == buffer.len(inputs.file)
	if same then
		for i = 0, buffer.len(back) - 1 do
			if buffer.readu8(back, i) ~= buffer.readu8(inputs.file, i) then
				same = false
				break
			end
		end
	end
	print("SAVE " .. tostring(same))
end
`
