package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSharedFixturesCanonical inventories shared file, message and witness
// wires. Framing damage is reported separately: the walk checks its trustworthy
// prefix and independently framed siblings, but cannot certify a damaged suffix.
func TestSharedFixturesCanonical(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	reports, err := readReports(filepath.Join(root, defaultReports))
	if err != nil {
		t.Fatal(err)
	}
	units := newUnits(m)
	type fileRow struct {
		name, unit, table, path string
		read, variable          bool
	}
	var files []fileRow
	for _, x := range m.Instances {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, !x.WireRefused, false})
	}
	for _, x := range m.Reports {
		verdict, ok := reports[x.Name]
		if !ok || (verdict != "read" && verdict != "refused") {
			t.Errorf("%s: missing or invalid report verdict %q", x.Name, verdict)
			continue
		}
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, verdict == "read", false})
	}
	for _, x := range m.Framings {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, !x.Refused, true})
	}
	for _, x := range m.Rewrites {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, true, true})
	}
	// Every pinned framing vector, the HELD ones with no manifest row
	// included: each holds its one purpose and no spelling beside it. A row
	// above that reads the same file keeps it judged as read.
	for _, v := range append(append([]framingVector{}, framingVectors...), noWriterVectors...) {
		if v.unit == "" || v.root == "" {
			continue
		}
		files = append(files, fileRow{v.name, v.unit, v.root, filepath.Join(framingDir, v.name+".bin"), false, true})
	}
	for _, x := range m.Retains {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, true, false})
	}
	for _, x := range m.JSONWriteRefusals {
		files = append(files, fileRow{x.Name, x.Unit, x.Root, x.Wire, true, false})
	}
	for _, x := range m.WriteRefusals {
		unit := x.Unit
		if x.Form == "message" {
			c, e := m.LookupConnection(x.Unit)
			if e != nil {
				t.Fatal(e)
			}
			unit = c.Unit
		}
		files = append(files, fileRow{x.Name, unit, x.Root, x.Wire, true, false})
	}
	// THE PURPOSE TABLE. Each row is an assertion that the file holds exactly
	// its one named spelling and no other, never an exemption. Every other
	// fixture, read or refused, holds none of them.
	purposes := sharedFixturePurposes
	seen := map[string]bool{}
	foundPurposes := map[string]bool{}
	counts := map[string]int{}
	checkFile := func(x fileRow) {
		key := fmt.Sprintf("%s/%s/%s/variable=%t", x.unit, x.table, x.path, x.variable)
		if seen[key] {
			return
		}
		seen[key] = true
		path := x.path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			t.Errorf("%s: %v", x.name, e)
			return
		}
		if len(data) == 0 {
			if x.read {
				t.Errorf("%s: empty readable file", x.name)
			} else {
				counts["empty-refused"]++
			}
			return
		}
		u, e := units.get(x.unit)
		if e != nil {
			t.Errorf("%s: %v", x.name, e)
			return
		}
		st := u.Tables[x.table]
		if st == nil {
			t.Errorf("%s: missing root %s", x.name, x.table)
			return
		}
		if st.Integrity != "" && data[0] == 1 {
			if len(data) < 5 {
				t.Errorf("%s: integrity trailer missing", x.name)
				return
			}
			data = data[:len(data)-4]
		}
		if data[0] != 1 {
			if data[0] == 3 {
				counts["fixed"]++
				return
			}
			if x.read {
				t.Errorf("%s: unsupported form %d on readable input", x.name, data[0])
			} else {
				counts["refused-form"]++
			}
			return
		}
		counts["variable"]++
		idShapes, walkErr := idTableSpellings(data)
		var damage *fixtureDamage
		if walkErr != nil {
			if _, ok := errors.AsType[*fixtureUnsupported](walkErr); ok {
				t.Errorf("%s: %v", x.name, walkErr)
				return
			}
			if !errors.As(walkErr, &damage) {
				t.Errorf("%s: %v", x.name, walkErr)
				return
			}
			counts["damaged-prefix-only"]++
			t.Logf("damage %s (%s): prefix ends byte %d; %v", x.name, x.path, damage.PrefixEnd, damage.Issues)
			if x.read {
				t.Errorf("%s: readable input has framing damage: %v", x.name, walkErr)
			}
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			t.Errorf("%s: %v", x.name, e)
			return
		}
		purpose := filepath.ToSlash(rel)
		if purposes[purpose] != "" {
			foundPurposes[purpose] = true
		}
		// Framing rows deliberately exercise the variable surface even when
		// their declared type also has a fixed file representation.
		if st.FixedDeclared && x.variable {
			u, e = framingVariableUnit(m, x.unit, x.table)
			if e != nil {
				t.Errorf("%s: %v", x.name, e)
				return
			}
			st = u.Tables[x.table]
			counts["framing-variable"]++
		} else if st.FixedDeclared {
			counts["previous-form"]++
			if x.read {
				t.Errorf("%s: fixed reader receives variable form", x.name)
			}
			judgeSharedFixture(t, x.name, x.path, purposes[purpose], idShapes)
			return
		}
		// A shared fixture's writer is known: it is the unit its row names.
		// So every spelling a writer of that unit would not produce is
		// judged against that declaration, whatever a reader of some other
		// build may be able to infer.
		shapes, e := sharedFixtureSpellings(u, st, data)
		if e != nil {
			if !errors.As(e, &damage) {
				t.Errorf("%s: %v", x.name, e)
				return
			}
		}
		for shape := range idShapes {
			shapes[shape] = true
		}
		judgeSharedFixture(t, x.name, x.path, purposes[purpose], shapes)
	}
	// Canonical output files are fixtures too: read every declared save and
	// rewrite, and every file side of a message row.
	for _, x := range m.Reports {
		if reports[x.Name] == "read" {
			files = append(files, fileRow{x.Name + "/save", x.Unit, x.Root, x.Save, true, false})
		}
	}
	for _, x := range m.Framings {
		if !x.Refused {
			files = append(files, fileRow{x.Name + "/save", x.Unit, x.Root, x.Save, true, true})
		}
	}
	for _, x := range m.Rewrites {
		files = append(files, fileRow{x.Name + "/canonical", x.Unit, x.Root, x.Canonical, true, true})
	}
	for _, x := range m.Retains {
		if x.Saves != "" {
			files = append(files, fileRow{x.Name + "/save", x.Unit, x.Root, x.Saves, true, false})
		}
	}
	for _, x := range m.Hostiles {
		if !x.Refused {
			files = append(files, fileRow{x.Name + "/save", x.Unit, x.Root, x.Save, true, false})
		}
	}
	for _, x := range m.Messages {
		files = append(files, fileRow{x.Name + "/file", x.Unit, x.Root, x.FileWire, true, false})
	}
	for _, c := range m.Connections {
		announcement, e := os.ReadFile(filepath.Join(root, c.Wire))
		if e != nil {
			t.Errorf("%s announcement: %v", c.Key, e)
			continue
		}
		if _, _, e := sharedMessageAnnouncement(announcement); e != nil {
			t.Errorf("%s announcement: %v", c.Key, e)
		} else {
			counts["announcements"]++
		}
	}
	// The ID-order refusal remains pinned even when its old rewrite row is
	// removed by the strict engine integration.
	files = append(files, fileRow{"rewrite_ids_reversed/purpose", "tblw1", "Fleet", "testdata/wire/tables/rewrite_ids_reversed.bin", false, true})
	// Aggregate repeated surface rows before walking so a refused occurrence
	// cannot hide another row that requires the same bytes to read.
	rows := map[string]fileRow{}
	for _, x := range files {
		key := fmt.Sprintf("%s/%s/%s/variable=%t", x.unit, x.table, x.path, x.variable)
		if old, ok := rows[key]; ok {
			old.read = old.read || x.read
			rows[key] = old
		} else {
			rows[key] = x
		}
	}
	for _, x := range rows {
		checkFile(x)
	}
	index, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors/INDEX.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(index), "\n") {
		p := strings.Fields(line)
		if len(p) == 0 || strings.HasPrefix(p[0], "#") {
			continue
		}
		if len(p) == 4 {
			checkFile(fileRow{p[0], p[1], p[2], p[3], false, false})
			continue
		}
		if len(p) != 5 || p[4] != "message" {
			t.Errorf("bad fuzz index row: %s", line)
			continue
		}
		data, e := os.ReadFile(filepath.Join(root, p[3]))
		if e != nil {
			t.Error(e)
			continue
		}
		u, e := units.get(p[1])
		if e != nil {
			t.Error(e)
			continue
		}
		st := u.Tables[p[2]]
		if st == nil {
			t.Errorf("%s missing root", p[0])
			continue
		}
		var conn string
		for _, c := range m.Connections {
			if c.Unit == p[1] {
				if conn != "" {
					t.Errorf("%s has multiple announcements", p[0])
				}
				conn = c.Wire
			}
		}
		if conn == "" {
			t.Errorf("%s has no announcement", p[0])
			continue
		}
		announce, e := os.ReadFile(filepath.Join(root, conn))
		if e != nil {
			t.Error(e)
			continue
		}
		shapes, e := messageStructuralSpellings(u, st, announce, data)
		if e != nil {
			if _, ok := errors.AsType[*sharedMessageUnsupported](e); ok {
				t.Errorf("%s unsupported message: %v", p[0], e)
			} else if damage, ok := errors.AsType[*sharedMessageDamage](e); ok {
				// These two named damage witnesses have independently pinned
				// complete prefixes; the remainder is not frameable.
				switch p[0] {
				case "message_bytes_truncated":
					if len(damage.PrefixShapes) != 1 || !damage.PrefixShapes["same-build-unknown-field"] {
						t.Errorf("%s: certified prefix=%v", p[0], damage.PrefixShapes)
					}
				case "message_bytes_truncated_isolated", "message_ill_formed_text":
					if len(damage.PrefixShapes) != 0 {
						t.Errorf("%s: certified prefix=%v", p[0], damage.PrefixShapes)
					}
				default:
					t.Errorf("%s: unclassified message damage: %v", p[0], e)
				}
				counts["damaged-message-boundary"]++
				t.Logf("message damage %s: prefix=%v; %v", p[0], damage.PrefixShapes, e)
			} else {
				t.Errorf("%s: %v", p[0], e)
			}
			continue
		}
		if want := purposes[p[3]]; want != "" {
			foundPurposes[p[3]] = true
			judgeSharedFixture(t, p[0], p[3], want, shapes)
		} else if len(shapes) > 0 {
			t.Errorf("%s: message shapes=%v", p[0], shapes)
		}
		counts["message-index"]++
	}
	for _, sub := range []struct {
		name string
		fn   func(*testing.T)
	}{{"messages", TestSharedMessageManifestSpellings}, {"message-purposes", TestSharedMessagePurposeSpellings}, {"message-damage-boundary", TestSharedMessageDamageDiscardsProvisionalSpellings}, {"message-value-domains", TestSharedMessageTextFloatDomains}, {"blob-witnesses", TestSharedBlobWitnessSpellings}, {"variable-purposes", TestNoncanonicalVariablePurposeShapes}, {"framing-purposes", TestFramingPurposeSpellingsRemain}, {"reordered-variable", TestVariableFieldOrderAcrossDeclarations}, {"unknown-required-union", TestUnknownEmptyUnionPurpose}, {"damage-boundaries", TestDamageAwareBoundaries}, {"reserved-node-kind", TestDamageAwareReservedNodeKind}, {"boolean-text-domains", TestDamageAwareBooleanAndText}, {"damage-id-controls", TestDamageAwareIDTableControls}, {"retaining-pins", TestRetainingFuzzCanonicalPins}, {"array-retaining-pins", TestRetainedArrayDuplicatePins}, {"blob-owner-pin", TestBlobDuplicateOwnerPin}, {"unknown-arm-pin", TestUnknownArmTextPin}, {"map-kind-pin", TestMapDepthRootKindPin}, {"list-child-pins", TestListDamagedChildPins}, {"variable-arm-pins", TestVariableArmKindPins}, {"a2-pointer-pin", TestFixedRootVariablePointerArmPin}, {"unknown-node-pin", TestUnknownNodeFixedReaderPin}} {
		t.Run(sub.name, sub.fn)
	}
	for path := range purposes {
		if !foundPurposes[path] {
			t.Errorf("missing spelling purpose %s", path)
		}
	}
	if len(seen) == 0 {
		t.Fatal("empty shared fixture inventory")
	}
	t.Logf("inventoried %d unit/root/files: %s", len(seen), fmt.Sprint(counts))
}

// sharedFixturePurposes is the purpose table: each file whose PURPOSE is one
// non-canonical spelling, and that spelling. The testdata/wire/non-canonical
// corpus asserts its own rows against its MANIFEST.tsv (the variable-purposes
// subtest).
var sharedFixturePurposes = map[string]string{
	"testdata/wire/tables/framing/frame_unreferenced_entry.bin": "unused-id-entry",
	"testdata/wire/tables/framing/frame_duplicate_entry.bin":    "duplicate-id-entry",
	"testdata/wire/tables/rewrite_ids_reversed.bin":             "id-table-out-of-order",
	"testdata/wire/tables/rewrite_fields_reversed.bin":          "fields-out-of-order",
	"testdata/wire/tables/framing/array_L2_empty.bin":           "stored-default",
	"testdata/wire/tables/framing/union_arm_zero_in_field.bin":  "union-arm-zero",
	"testdata/wire/tables/framing/node_index_null.bin":          "explicit-null-pointer",
	// The message skip depth pair is a batch whose only field is an entry the
	// reader build does not declare (message_skip_depth_test.go rebuilds both
	// byte for byte), so that unknown field is the purpose of each file.
	"testdata/wire/tables/fuzz-vectors/message_skip_depth_at_limit.bin":   "same-build-unknown-field",
	"testdata/wire/tables/fuzz-vectors/message_skip_depth_over_limit.bin": "same-build-unknown-field",
	// retain_node_flags carries one id entry nothing references; the strict
	// oracle refuses it for that entry, and every leg is held to that refusal.
	"testdata/wire/tables/fuzz-vectors/retain_node_flags.bin": "unused-id-entry",
}

// judgeSharedFixture fails a purpose file that does not hold exactly its one
// spelling, and any other file that holds a spelling at all.
func judgeSharedFixture(t *testing.T, name, path, want string, shapes map[string]bool) {
	t.Helper()
	if want != "" {
		if len(shapes) != 1 || !shapes[want] {
			t.Errorf("%s (%s): spellings %v, want exactly %s", name, path, sortedShapes(shapes), want)
		}
		return
	}
	for _, shape := range sortedShapes(shapes) {
		t.Errorf("%s (%s): non-canonical spelling %s", name, path, shape)
	}
}

func sortedShapes(shapes map[string]bool) []string {
	var out []string
	for shape := range shapes {
		out = append(out, shape)
	}
	slices.Sort(out)
	return out
}

// idTableSpellings is every id-table spelling of one file: a duplicate entry,
// an entry out of first-use order, and an entry nothing uses. First-use order
// is judged on the trustworthy prefix of a damaged file; unused entries only on
// a complete walk, since an unframed suffix may use any remaining entry.
func idTableSpellings(data []byte) (map[string]bool, error) {
	out := map[string]bool{}
	f, err := damageAwareFrame(data)
	if f.countAt == 0 {
		return out, err
	}
	seenIDs := map[uint64]bool{}
	for _, id := range f.entries {
		if seenIDs[id] {
			out["duplicate-id-entry"] = true
		}
		seenIDs[id] = true
	}
	limit := len(data)
	if damage, ok := errors.AsType[*fixtureDamage](err); ok {
		limit = damage.PrefixEnd
	}
	used := map[uint64]bool{}
	next := uint64(1)
	for _, sp := range f.spots {
		if sp.off >= limit {
			continue
		}
		switch sp.kind {
		case spotRef, spotArm, spotKey, spotVariant, spotRecordType:
		default:
			continue
		}
		if sp.value == 0 || sp.value > uint64(len(f.entries)) || used[sp.value] {
			continue
		}
		used[sp.value] = true
		if sp.value != next {
			out["id-table-out-of-order"] = true
		}
		next++
	}
	if err == nil && len(used) != len(f.entries) {
		out["unused-id-entry"] = true
	}
	return out, err
}

// sharedFixtureSpellings is damageAwareFileSpellings plus FIELD ORDER judged
// against the fixture's own declaration: within every body whose declaration
// the walk knows, the declared fields it stores appear in declaration order.
// A reader of another build cannot judge a file's order; a shared fixture's
// writer is the unit its row names, so here it can. Unknown fields and
// repeats are not judged here.
func sharedFixtureSpellings(u *ir.Unit, root *ir.Struct, data []byte) (map[string]bool, error) {
	out, err := damageAwareFileSpellings(u, root, data)
	f, frameErr := damageAwareFrame(data)
	if err == nil {
		// the same bytes framed twice: a frame error the spellings pass did
		// not report is still the file's verdict
		err = frameErr
	}
	scopes := damageAwareScopes(u, root, f)
	complete := map[int]bool{}
	for _, field := range f.fields {
		complete[field.start] = true
	}
	last := map[int]int{}
	for _, sp := range f.spots {
		if sp.kind != spotRef || sp.value == 0 || !complete[sp.off] {
			continue
		}
		key := -1
		if len(sp.enclosing) > 0 {
			key = sp.enclosing[len(sp.enclosing)-1]
		}
		owner := scopes[key].owner
		if owner == nil {
			continue
		}
		id, ok := f.entryOf(sp.value)
		if !ok {
			continue
		}
		at := slices.IndexFunc(owner.Fields, func(fd *ir.Field) bool { return ir.TableFieldWireId(fd) == id })
		if at < 0 {
			continue
		}
		if prev, ok := last[key]; ok && at < prev {
			out["fields-out-of-order"] = true
		}
		last[key] = at
	}
	return out, err
}
