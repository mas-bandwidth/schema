package pack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ir"
)

// The manifest is the TEMPORARY grammar of the table layer: it declares the
// collections (ordered!), their JSON sources and their target types, until
// the language grows collection declarations under Glenn's grammar decisions
// (SPEC §1's open questions, notes/table-layer-model.md §7). Everything the
// manifest holds is exactly what a future `collection` declaration would.
type Manifest struct {
	Unit    string   `json:"unit"`    // schema unit directory (relative to the manifest's own dir)
	Outputs []Output `json:"outputs"` // one .bin each
}

type Output struct {
	File        string       `json:"file"` // output path (relative to the manifest's dir)
	Collections []Collection `json:"collections"`
}

type Collection struct {
	// Type is the schema type each instance encodes as.
	Type string `json:"type"`
	// Dir holds one JSON file per instance; File holds exactly one instance.
	Dir  string `json:"dir,omitempty"`
	File string `json:"file,omitempty"`
	// KeyEnum names an enum whose non-None variants must correspond 1:1 with
	// the instance files (basename == variant name) — the update_config count
	// invariant, kept. Instances are packed in ENUM ORDER (declared order),
	// never directory order: order is index semantics (the renumbering trap,
	// notes/table-layer-model.md §5).
	KeyEnum string `json:"key_enum,omitempty"`
	// KeyField, when set with KeyEnum, names an enum field inside each
	// instance that must equal its file's variant (the triple-identity check).
	KeyField string `json:"key_field,omitempty"`
	// Unions declares the flatbuffers-JSON union lowerings for this
	// collection's types — the transition adapter; dies with flatbuffers.
	Unions []UnionRule `json:"unions,omitempty"`
	// Renames maps JSON keys onto schema field names per owner type — for
	// keys the schema language cannot spell (`type` is a keyword). Applied
	// before encoding; the JSON stays untouched on disk.
	Renames map[string]map[string]string `json:"renames,omitempty"`
	// ClampBounds opts this collection into clamp-with-warning for
	// out-of-range integers (the historical LevelInfo semantics; see
	// Encoder.ClampBounds). Strict refusal is the default.
	ClampBounds bool `json:"clamp_bounds,omitempty"`
	// PresenceGuards derives top-level bool guard fields from JSON key
	// presence: {"has_gunner_settings": "gunner_settings"} sets the guard
	// true iff the named key is present and non-null in the instance —
	// the flatbuffers null-table seam, made explicit at pack time.
	PresenceGuards map[string]string `json:"presence_guards,omitempty"`
}

// UnionRule lowers one flatbuffers-JSON union field pair onto bool-guarded
// schema arms: {"<key>_type": "BoxCollider", "<key>": {...}} becomes
// {"<arm guard>": true, "<arm field>": {...}}.
type UnionRule struct {
	// OwnerType is the schema type whose JSON objects carry the union pair.
	OwnerType string `json:"owner_type"`
	// JsonKey is the union value key ("collider"); the discriminator is
	// JsonKey + "_type" (the flatbuffers-JSON convention).
	JsonKey string `json:"json_key"`
	// Arms maps each discriminator value to [guard field, value field].
	Arms map[string][2]string `json:"arms"`
}

// lowerUnions applies this type's rename and union rules to a JSON object,
// returning a rewritten copy (the input is never mutated — instances may
// share maps).
func (e *Encoder) lowerUnions(typeName string, obj map[string]any, path string) (map[string]any, error) {
	rules := e.UnionLower[typeName]
	renames := e.Renames[typeName]
	if len(rules) == 0 && len(renames) == 0 {
		return obj, nil
	}
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		if to, ok := renames[k]; ok {
			k = to
		}
		out[k] = v
	}
	for _, r := range rules {
		discKey := r.JsonKey + "_type"
		disc, hasDisc := out[discKey]
		val, hasVal := out[r.JsonKey]
		if !hasDisc && !hasVal {
			continue // union absent — all guards stay false
		}
		if !hasDisc || !hasVal {
			return nil, fmt.Errorf("%s: union %q needs both %q and %q keys", path, r.JsonKey, discKey, r.JsonKey)
		}
		name, ok := disc.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s: union discriminator must be a string, got %T", path, discKey, disc)
		}
		arm, ok := r.Arms[name]
		if !ok {
			return nil, fmt.Errorf("%s.%s: union arm %q is not declared in the manifest (arms: %s)", path, discKey, name, armNames(r.Arms))
		}
		delete(out, discKey)
		delete(out, r.JsonKey)
		out[arm[0]] = true
		out[arm[1]] = val
	}
	return out, nil
}

func armNames(arms map[string][2]string) string {
	names := make([]string, 0, len(arms))
	for n := range arms {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ---- the container ----
//
// The .bin container is deliberately minimal and byte-aligned:
//
//	magic    "SBN1"                       4 bytes
//	protocol id (unit)                    8 bytes LE
//	content hash (fnv64a of everything    8 bytes LE
//	  after this field)
//	per output collection, in manifest order:
//	  instance count                      4 bytes LE
//	  per instance, in key-enum order:
//	    payload byte length               4 bytes LE
//	    payload (schema wire bytes)       N bytes
//
// Exact-match versioning rides the protocol id (the unit hash); the content
// hash detects data changes at equal schema. Loaders reject unknown magic,
// wrong protocol id, or wrong hash — drift is detected, never tolerated.

const Magic = "SBN1"

// Fnv64a matches the game's hash_data / modules/common HashData.
func Fnv64a(data []byte) uint64 {
	h := uint64(0xCBF29CE484222325)
	for _, b := range data {
		h ^= uint64(b)
		h *= 0x100000001B3
	}
	return h
}

func putU32(buf []byte, v uint32) []byte {
	return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func putU64(buf []byte, v uint64) []byte {
	return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}

// BuildOutput packs one Output's collections into container bytes — each
// instance on the TABLE wire (notes/table-wire.md), the evolution-tolerant
// encoding config data outlives builds on.
func BuildOutput(u *ir.Unit, enc *Encoder, out Output, baseDir string) ([]byte, error) {
	if err := CheckTableIds(u); err != nil {
		return nil, err
	}
	for _, c := range out.Collections {
		if st, ok := u.Structs[c.Type]; !ok || !st.IsTable {
			return nil, fmt.Errorf("collection type %s is not declared as a `table` — pack roots must be tables (reflection and table codecs follow the declaration)", c.Type)
		}
	}
	var payload []byte
	for _, c := range out.Collections {
		instances, err := collectInstances(u, c, baseDir)
		if err != nil {
			return nil, err
		}
		payload = putU32(payload, uint32(len(instances)))
		for _, inst := range instances {
			enc.UnionLower = unionIndex(c.Unions)
			enc.Renames = c.Renames
			enc.ClampBounds = c.ClampBounds
			source := inst.source
			enc.Warn = func(format string, args ...any) {
				fmt.Fprintf(os.Stderr, "pack warning: %s: %s\n", source, fmt.Sprintf(format, args...))
			}
			for guard, key := range c.PresenceGuards {
				v, present := inst.obj[key]
				inst.obj[guard] = present && v != nil
			}
			wire, err := enc.EncodeTable(c.Type, inst.obj)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", inst.source, err)
			}
			payload = putU32(payload, uint32(len(wire)))
			payload = append(payload, wire...)
		}
	}
	var bin []byte
	bin = append(bin, Magic...)
	bin = putU64(bin, u.ProtocolId)
	bin = putU64(bin, Fnv64a(payload))
	bin = append(bin, payload...)
	return bin, nil
}

func unionIndex(rules []UnionRule) map[string][]UnionRule {
	idx := map[string][]UnionRule{}
	for _, r := range rules {
		idx[r.OwnerType] = append(idx[r.OwnerType], r)
	}
	return idx
}

type instance struct {
	source string // the JSON path, for error messages
	obj    map[string]any
}

// collectInstances loads a collection's JSON files. With KeyEnum, instances
// are ordered by ENUM DECLARATION ORDER and the file set must match the
// variant set exactly (both directions — the update_config invariants, kept:
// a missing variant file and an orphan file are both authoring errors).
func collectInstances(u *ir.Unit, c Collection, baseDir string) ([]instance, error) {
	if (c.Dir == "") == (c.File == "") {
		return nil, fmt.Errorf("collection %s: exactly one of dir or file", c.Type)
	}
	if c.File != "" {
		obj, err := loadJSON(filepath.Join(baseDir, c.File))
		if err != nil {
			return nil, err
		}
		return []instance{{source: c.File, obj: obj}}, nil
	}
	dir := filepath.Join(baseDir, c.Dir)
	byName := map[string]string{} // basename -> path (recursive: levels nest per-level dirs)
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		base := strings.TrimSuffix(d.Name(), ".json")
		if prev, dup := byName[base]; dup {
			return fmt.Errorf("collection %s: two instance files named %s (%s, %s)", c.Type, base, prev, p)
		}
		byName[base] = p
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collection %s: %w", c.Type, err)
	}
	if c.KeyEnum == "" {
		// unkeyed collection: deterministic name order
		names := make([]string, 0, len(byName))
		for n := range byName {
			names = append(names, n)
		}
		sort.Strings(names)
		var out []instance
		for _, n := range names {
			obj, err := loadJSON(byName[n])
			if err != nil {
				return nil, err
			}
			out = append(out, instance{source: byName[n], obj: obj})
		}
		return out, nil
	}
	enum, ok := u.Enums[c.KeyEnum]
	if !ok {
		return nil, fmt.Errorf("collection %s: key_enum %s is not declared in the unit", c.Type, c.KeyEnum)
	}
	seen := map[string]bool{}
	var out []instance
	for _, variant := range enum.Variants { // DECLARED order — order is index semantics
		path, ok := byName[variant]
		if !ok {
			return nil, fmt.Errorf("collection %s: missing instance file %s/%s.json for %s variant %s", c.Type, c.Dir, variant, c.KeyEnum, variant)
		}
		seen[variant] = true
		obj, err := loadJSON(path)
		if err != nil {
			return nil, err
		}
		if c.KeyField != "" {
			if got, _ := obj[c.KeyField].(string); got != variant {
				return nil, fmt.Errorf("%s: %s = %q but the filename says %s (the triple-identity check)", path, c.KeyField, got, variant)
			}
		}
		out = append(out, instance{source: path, obj: obj})
	}
	for name := range byName {
		if !seen[name] {
			return nil, fmt.Errorf("collection %s: orphan instance file %s/%s.json — %s has no variant %s (stale file, or the enum is missing a member)", c.Type, c.Dir, name, c.KeyEnum, name)
		}
	}
	return out, nil
}

func loadJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(StripTrailingCommas(string(data))))
	dec.UseNumber() // full-range uint64 values must survive exactly (json.Number)
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return obj, nil
}

// StripTrailingCommas makes the game's flatbuffers-JSON dialect strict:
// flatc's parser tolerates a comma before a closing } or ] (the shipped
// config files use it liberally) and strict JSON does not. String-aware —
// a comma inside a quoted string is never touched. This is the dialect
// adapter's whole extent: the files carry no comments (checked 2026-08-11).
// Exported so the fuzzer (internal/fuzz) drives the exact dialect the data
// compiler reads rather than an approximation of it.
func StripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // drop the trailing comma
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
