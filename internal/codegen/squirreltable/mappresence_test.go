package squirreltable_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

// mapPresenceGoldens are the Go oracle's pinned wires (TestGoMapPresence,
// internal/codegen/gotable/mappresence_test.go, schema-go GO TIP d808c3f54)
// for the three values of `tags ?map[string(8)]int32` (docs/SPEC-TABLES.md
// §2.8): ABSENT writes no field, PRESENT-EMPTY writes kind 14 with L = 2 and
// the two-byte body `0d 00`, and PRESENT-WITH-ENTRIES writes the plain map.
const (
	mapPresenceAbsent  = "01" + "010401000000" + "00" + "c03a5cb5072eb708" + "0100000000000000"
	mapPresenceEmpty   = "01" + "010401000000" + "020e020d00" + "00" + "c03a5cb5072eb708" + "80856f07ef3b1cd9" + "0200000000000000"
	mapPresenceEntries = "01" + "010401000000" + "020e0e0d01" + "0b" + "030c0161" + "040405000000" + "00" + "00" + "c03a5cb5072eb708" + "80856f07ef3b1cd9" + "ec105b36194ac93d" + "ea0ce83094fde47c" + "0400000000000000"
)

// mapPresenceJSON is the oracle's text for the same three values: absent
// omits the key, present-empty is `{}`, present-with-entries is the map.
var mapPresenceJSON = map[string]string{
	"absent":  `{"id":1}`,
	"empty":   `{"id":1,"tags":{}}`,
	"entries": `{"id":1,"tags":{"a":5}}`,
}

// mapPresenceProbe builds each shape, writes its wire and its text, reads the
// oracle's text and wire back, and prints the presence it reads as the one of
// three values it is (docs/SPEC-TABLES.md §2.8, §16.2).
const mapPresenceProbe = `local inputs = dofile("inputs.nut", true);
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local hexOf = function(b) {
  if (b == null) return "null";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += HEXB[b[i] & 0xff];
  return s;
};
local fresh = function() {
  return { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
};
local run = function(unit, mod) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local S = schema.require("schema_table_save");
  local J = schema.require("schema_table_json");
  local build = function(shape) {
    local v = T.M();
    v.Id = 1;
    if (shape == "empty") v.TagsPresent = true;
    if (shape == "entries") {
      v.TagsPresent = true;
      local e = T.MTagsEntry();
      e.Key = "a";
      e.Value = 5;
      v.Tags.append(e);
    }
    return v;
  };
  local describe = function(v) {
    if (v == null) return "null";
    if (!v.TagsPresent) {
      if (v.Tags.len() != 0) return "absent-with-entries";
      return "absent";
    }
    if (v.Tags.len() == 0) return "empty";
    if (v.Tags.len() != 1 || v.Tags[0].Key != "a" || v.Tags[0].Value != 5) return "entries-wrong";
    return "entries";
  };
  local built = function(shape) {
    local v = build(shape);
    local b = S.save(T, "M", v);
    local t = J.ToJson(T, "M", v);
    print("build " + shape + " " + hexOf(b) + " " + hexOf(t) + "\n");
  };
  local fromJson = function(shape, text) {
    local r = fresh();
    local v = J.FromJsonTolerant(T, "M", text, r);
    local b = r.malformed ? null : S.save(T, "M", v);
    print("json " + shape + " " + describe(v) + " " + hexOf(b) + "\n");
  };
  local fromWire = function(shape, data) {
    local r = fresh();
    local v = L.loadTolerant(T, "M", data, r);
    local b = r.malformed ? null : S.save(T, "M", v);
    print("wire " + shape + " " + describe(v) + " " + hexOf(b) + "\n");
  };
  built("absent"); built("empty"); built("entries");
  fromJson("absent", "{\"id\":1}");
  fromJson("empty", "{\"id\":1,\"tags\":{}}");
  fromJson("entries", "{\"id\":1,\"tags\":{\"a\":5}}");
  fromJson("null", "{\"id\":1,\"tags\":null}");
  fromWire("absent", inputs["absent"]);
  fromWire("empty", inputs["empty"]);
  fromWire("entries", inputs["entries"]);
};
`

// TestSquirrelMapPresence holds docs/SPEC-TABLES.md §2.8's OPTIONAL MAP in the
// Squirrel table leg: absent, present-empty and present-with-entries are three
// distinct values, each writes the Go oracle's wire byte for byte and its JSON
// text, and each reads back from the oracle's wire and text as itself
// (absent omits the field, present-empty writes it, `null` is absent).
func TestSquirrelMapPresence(t *testing.T) {
	t.Parallel()
	unit := loadSource(t, "package probe\n\ntable M\n{\n    id int32\n    tags ?map[string(8)]int32\n}\n")
	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", unit)

	var p strings.Builder
	p.WriteString(mapPresenceProbe)
	fmt.Fprintf(&p, "run(%q, %q);\n", "probe", mod)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	inputs := map[string][]byte{}
	for _, c := range []struct{ name, wire string }{
		{"absent", mapPresenceAbsent},
		{"empty", mapPresenceEmpty},
		{"entries", mapPresenceEntries},
	} {
		b, err := hex.DecodeString(c.wire)
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.name] = b
	}
	out := sqrun.Run(t, dir, "main.nut", inputs)
	rows := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("probe line %q is not a case, a shape and its bytes", line)
		}
		rows[f[0]+" "+f[1]] = f[2:]
	}
	goldens := map[string]string{
		"absent":  mapPresenceAbsent,
		"empty":   mapPresenceEmpty,
		"entries": mapPresenceEntries,
	}
	compared := 0
	for _, shape := range []string{"absent", "empty", "entries"} {
		golden := goldens[shape]
		// THE WIRE AND TEXT THIS TARGET WRITES (§2.8, §16.2)
		fields, ok := rows["build "+shape]
		if !ok {
			t.Errorf("%s: the probe printed no built row", shape)
		} else {
			if fields[0] != golden {
				t.Errorf("%s: Squirrel writes\n got %s\nwant %s", shape, fields[0], golden)
			} else {
				compared++
			}
			have, err := hex.DecodeString(fields[1])
			if err != nil {
				t.Errorf("%s: ToJson is not hex: %v", shape, err)
			} else if got, want := compactJSON(t, have), compactJSON(t, []byte(mapPresenceJSON[shape])); got != want {
				t.Errorf("%s: ToJson\n got %s\nwant %s", shape, got, want)
			} else {
				compared++
			}
		}
		// THE TEXT READS BACK AS THE SAME ONE OF THREE (§16.2)
		fields, ok = rows["json "+shape]
		switch {
		case !ok:
			t.Errorf("%s: the probe printed no json row", shape)
		case fields[0] != shape:
			t.Errorf("%s: FromJson reads back %s", shape, fields[0])
		case fields[1] != golden:
			t.Errorf("%s: FromJson then save\n got %s\nwant %s", shape, fields[1], golden)
		default:
			compared++
		}
		// THE WIRE READS BACK AS THE SAME ONE OF THREE (§2.8)
		fields, ok = rows["wire "+shape]
		switch {
		case !ok:
			t.Errorf("%s: the probe printed no wire row", shape)
		case fields[0] != shape:
			t.Errorf("%s: load reads back %s", shape, fields[0])
		case fields[1] != golden:
			t.Errorf("%s: load then save\n got %s\nwant %s", shape, fields[1], golden)
		default:
			compared++
		}
	}
	// A JSON null IS ABSENT and writes the absent wire (§16.2)
	fields, ok := rows["json null"]
	switch {
	case !ok:
		t.Errorf("null: the probe printed no json row")
	case fields[0] != "absent":
		t.Errorf("null: FromJson reads back %s, want absent", fields[0])
	case fields[1] != mapPresenceAbsent:
		t.Errorf("null: null writes\n got %s\nwant %s", fields[1], mapPresenceAbsent)
	default:
		compared++
	}
	// three built wires, three built texts, three FromJson wires, three
	// loaded wires and the null's absent wire
	if want := 13; compared != want {
		t.Errorf("compared %d of %d oracle bytes/texts", compared, want)
	}
	t.Logf("compared %d oracle bytes and texts over absent, present-empty and present-with-entries (wire, JSON, null)", compared)
}

func compactJSON(t *testing.T, text []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, text); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return out.String()
}
