package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type squirrelMessageConnection struct{ unit, file string }
type squirrelMessageRow struct{ name, connection, root, file, batch string }

func readMessageRows(t *testing.T, root string) (map[string]squirrelMessageConnection, []squirrelMessageRow) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]squirrelMessageConnection{}
	var rows []squirrelMessageRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "connection":
			if len(f) >= 5 {
				connections[f[1]] = squirrelMessageConnection{unit: f[2], file: f[4]}
			}
		case "message":
			if len(f) >= 6 && f[5] != "-" { // a named refusal row (a variable table, §3.3) carries no message bytes
				rows = append(rows, squirrelMessageRow{name: f[1], connection: f[2], root: f[3], file: f[4], batch: f[5]})
			}
		}
	}
	return connections, rows
}

func appendMessageLEB(dst []byte, value uint64) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}
func announcementWithEntries(u *ir.Unit, entries []ir.TableVocabularyEntry) []byte {
	words := ir.TableVocabularyBytes(entries)
	body := []byte{byte(ir.TableKindU8)}
	body = appendMessageLEB(body, uint64(len(words)))
	body = append(body, words...)
	out := []byte{ir.TableWireForm, 1, byte(ir.TableKindU64)}
	out = binary.LittleEndian.AppendUint64(out, ir.BuildVersion(u))
	out = append(out, 2, byte(ir.TableKindArray))
	out = appendMessageLEB(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	out = binary.LittleEndian.AppendUint64(out, 2)
	return out
}
func announcementWithPeerEntry(u *ir.Unit, entry ir.TableVocabularyEntry) ([]byte, int) {
	entries := ir.TableVocabulary(u)
	if len(entries) > 0 && entries[len(entries)-1].Id == ^uint64(0) {
		entries = append(entries[:len(entries)-1], append([]ir.TableVocabularyEntry{entry}, entries[len(entries)-1:]...)...)
	} else {
		entries = append(entries, entry)
	}
	return announcementWithEntries(u, entries), len(entries)
}
func announcementWithChangedEntry(u *ir.Unit, target, replacement ir.TableVocabularyEntry) []byte {
	entries := ir.TableVocabulary(u)
	found := false
	for i := range entries {
		if entries[i].Id == target.Id {
			entries[i] = replacement
			found = true
		}
	}
	if !found {
		panic("target vocabulary entry not found")
	}
	return announcementWithEntries(u, entries)
}
func messageBatchTwoUnknownString(refBits int, slot uint64, value string, bound int64) []byte {
	out := []byte{2, 1}
	bits := 16
	put := func(value uint64, width int) {
		for i := range width {
			at := bits / 8
			if at == len(out) {
				out = append(out, 0)
			}
			if (value>>i)&1 != 0 {
				out[at] |= 1 << (bits % 8)
			}
			bits++
		}
	}
	width := 0
	for n := uint64(bound); n > 0; n >>= 1 {
		width++
	}
	put(slot, refBits)
	put(uint64(len(value)), width)
	for bits%8 != 0 {
		put(0, 1)
	}
	for i := 0; i < len(value); i++ {
		put(uint64(value[i]), 8)
	}
	put(0, refBits)
	put(0, refBits)
	for bits%8 != 0 {
		put(0, 1)
	}
	return out
}
func messageBatchEnumField(refBits int, fieldSlot, variantSlot uint64) []byte {
	out := []byte{2, 0}
	bits := 16
	put := func(value uint64, width int) {
		for i := range width {
			at := bits / 8
			if at == len(out) {
				out = append(out, 0)
			}
			if (value>>i)&1 != 0 {
				out[at] |= 1 << (bits % 8)
			}
			bits++
		}
	}
	put(fieldSlot, refBits)
	put(variantSlot, refBits)
	put(0, refBits)
	for bits%8 != 0 {
		put(0, 1)
	}
	return out
}
func messageBatchRefs(refBits int, refs ...uint64) []byte {
	out := []byte{2, 0}
	bits := 16
	put := func(value uint64, width int) {
		for i := range width {
			at := bits / 8
			if at == len(out) {
				out = append(out, 0)
			}
			if (value>>i)&1 != 0 {
				out[at] |= 1 << (bits % 8)
			}
			bits++
		}
	}
	for _, ref := range refs {
		put(ref, refBits)
	}
	for bits%8 != 0 {
		put(0, 1)
	}
	return out
}
func messageBatchWithInnerRef(refBits int, slot, inner uint64) []byte {
	out := []byte{2, 0}
	bits := 16
	put := func(value uint64, width int) {
		for i := range width {
			at := bits / 8
			if at == len(out) {
				out = append(out, 0)
			}
			if (value>>i)&1 != 0 {
				out[at] |= 1 << (bits % 8)
			}
			bits++
		}
	}
	put(slot, refBits)
	put(inner, refBits)
	put(0, refBits)
	for bits%8 != 0 {
		put(0, 1)
	}
	return out
}
func messageBatchWithUnknownString(refBits int, slot uint64, value string, bound int64) []byte {
	out := []byte{2, 0}
	bits := 16
	put := func(value uint64, width int) {
		for i := range width {
			at := bits / 8
			if at == len(out) {
				out = append(out, 0)
			}
			if (value>>i)&1 != 0 {
				out[at] |= 1 << (bits % 8)
			}
			bits++
		}
	}
	countBits := 0
	for n := uint64(bound); n > 0; n >>= 1 {
		countBits++
	}
	put(slot, refBits)
	put(uint64(len(value)), countBits)
	for bits%8 != 0 {
		put(0, 1)
	}
	for i := 0; i < len(value); i++ {
		put(uint64(value[i]), 8)
	}
	put(0, refBits)
	for bits%8 != 0 {
		put(0, 1)
	}
	return out
}
func addUnknownAnnouncementField(src []byte, id uint64) []byte {
	count := binary.LittleEndian.Uint64(src[len(src)-8:])
	start := len(src) - 8 - int(count)*8
	if count != 2 || start < 2 || src[start-1] != 0 {
		panic("unexpected announcement framing")
	}
	out := append([]byte(nil), src[:start-1]...)
	out = append(out, 3, 6, 0x5a, 0)
	out = append(out, src[start:len(src)-8]...)
	var word [8]byte
	binary.LittleEndian.PutUint64(word[:], id)
	out = append(out, word[:]...)
	binary.LittleEndian.PutUint64(word[:], count+1)
	out = append(out, word[:]...)
	return out
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func squirrelMessageFiles(t *testing.T, u *ir.Unit, patch map[string][]byte) map[string][]byte {
	t.Helper()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range tables {
		if replacement, ok := patch[name]; ok {
			body = replacement
		}
		files[name] = body
	}
	return files
}

func runSquirrelMessages(t *testing.T, root string, units map[string]*ir.Unit, connections map[string]squirrelMessageConnection, rows []squirrelMessageRow, negative bool) []byte {
	t.Helper()
	dir := t.TempDir()
	patch := map[string][]byte{}
	if negative {
		runtime := squirreltable.Runtime()
		const name = "schema_table_message.nut"
		body := string(runtime[name])
		const anchor = "local count = (bytes[1]&0xff)+1;"
		if strings.Count(body, anchor) != 1 {
			t.Fatalf("negative control anchor occurs %d times, want exactly once", strings.Count(body, anchor))
		}
		patch[name] = []byte(strings.Replace(body, anchor, "local count = (bytes[1]&0xff);", 1))
	}
	inputs := map[string][]byte{}
	var script strings.Builder
	script.WriteString("local I=dofile(\"inputs.nut\",true);\n")
	script.WriteString("local same=function(a,b){if(a.len()!=b.len())return false;for(local i=0;i<a.len();i++)if(a[i]!=b[i])return false;return true;};\n")
	generated := map[string]bool{}
	for _, row := range rows {
		conn, ok := connections[row.connection]
		if !ok {
			t.Fatalf("message %s has no connection %s", row.name, row.connection)
		}
		u := units[conn.unit]
		if u == nil {
			t.Fatalf("connection %s has no unit %s", row.connection, conn.unit)
		}
		sub := filepath.Join(dir, conn.unit)
		if !generated[conn.unit] {
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, b := range squirrelMessageFiles(t, u, patch) {
				if strings.Contains(name, "/") {
					continue
				}
				writeFile(t, filepath.Join(sub, name), b)
			}
			generated[conn.unit] = true
		}
		inputs[row.name+"_ann"] = mustRead(t, filepath.Join(root, conn.file))
		inputs[row.name+"_batch"] = mustRead(t, filepath.Join(root, row.batch))
		module := squirreltable.ModuleName(u)
		fmt.Fprintf(&script, "{ local T=dofile(%q,true).require(%q); local v={}; local ar={};\n", conn.unit+"/schema_module.nut", module)
		fmt.Fprintf(&script, " if(!T.AnnounceRead(v,I[%q],ar)){print(%q+\":announce:\"+ar.reason+\"\\n\");} else {\n", row.name+"_ann", row.name)
		if st := u.Tables[row.root]; st == nil || !st.FixedDeclared {
			// a variable table carries no message verbs at all (§3.3)
			fmt.Fprintf(&script, " print(%q+((%q in T || %q in T || %q in T) ? \":present\\n\" : \":absent\\n\")); } }\n", row.name, row.root+"LoadMessages", row.root+"SaveMessages", row.root+"MeasureMessages")
			continue
		}
		batch := inputs[row.name+"_batch"]
		count := int(batch[1]) + 1
		constructors := make([]string, count)
		for i := range constructors {
			constructors[i] = "T." + row.root + "()"
		}
		fmt.Fprintf(&script, " local out=[%s]; local rr={}; local n=T.%sLoadMessages(out,v,I[%q],rr); if(n!=%d) print(%q+\":load:\"+n+\":\"+rr.reason+\":\"+rr.malformed+\"\\n\");\n", strings.Join(constructors, ","), row.root, row.name+"_batch", count, row.name)
		fmt.Fprintf(&script, " local mr={}; local size=T.%sMeasureMessages(out,mr); local b=blob(size); local sr={}; local got=T.%sSaveMessages(out,b,sr); if(got!=size || !same(b,I[%q])) print(%q+\":save:\"+got+\":\"+size+\"\\n\"); else print(%q+\":ok\\n\"); } }\n", row.root, row.root, row.name+"_batch", row.name, row.name)
	}
	for _, key := range []string{"backend_conn", "vocab_conn", "vocab9_conn", "graph_conn"} {
		conn, ok := connections[key]
		if !ok {
			continue
		}
		u := units[conn.unit]
		name := "parsed_" + conn.unit
		inputs[name+"_ann"] = addUnknownAnnouncementField(mustRead(t, filepath.Join(root, conn.file)), 0x0123456789abcdef)
		module := squirreltable.ModuleName(u)
		fmt.Fprintf(&script, `{ local T=dofile(%q,true).require(%q); local v={}; local ar={}; if(T.AnnounceRead(v,I[%q],ar) && ar.unknown==1) print(%q+":ok\n"); else print(%q+":fail:"+ar.reason+":"+ar.malformed+"\n"); }`+"\n", conn.unit+"/schema_module.nut", module, name+"_ann", name, name)
	}
	if conn, ok := connections["vocab_conn"]; ok {
		peer := mustRead(t, filepath.Join(root, conn.file))
		inputs["alternate_ann"] = peer
		inputs["alternate_unknown_ann"] = addUnknownAnnouncementField(peer, 0x0123456789abcdef)
		shapePeer, shapeCount := announcementWithPeerEntry(units[conn.unit], ir.TableVocabularyEntry{Id: 0x1234567890abcdef, Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: 5}})
		inputs["unknown_body_ann"] = addUnknownAnnouncementField(shapePeer, 0x1020304050607080)
		inputs["unknown_body_batch"] = messageBatchTwoUnknownString(ir.TableMessageRefBits(shapeCount), uint64(shapeCount), "ok", 5)
		script.WriteString(`{ local T=dofile("backenddemo/schema_module.nut",true).require("BackenddemoTables"); local v={}; local ar={}; if(T.AnnounceRead(v,I["alternate_ann"],ar)) print("alternate:ok:"+v.Count+"\n"); else print("alternate:fail:"+ar.reason+":"+ar.malformed+"\n"); }
`)
		script.WriteString(`{ local T=dofile("backenddemo/schema_module.nut",true).require("BackenddemoTables"); local v={}; local ar={}; if(T.AnnounceRead(v,I["alternate_unknown_ann"],ar) && ar.unknown==1) print("alternate_unknown:ok\n"); else print("alternate_unknown:fail:"+ar.unknown+":"+ar.reason+":"+ar.malformed+"\n"); }
`)
		script.WriteString(`{ local T=dofile("vocabdemo/schema_module.nut",true).require("VocabdemoTables"); local v={}; local ar={}; if(!T.AnnounceRead(v,I["alternate_unknown_ann"],ar) || ar.unknown!=1) print("alternate_batch:announce_fail\n"); else { local out=[T.Wide09()]; local rr={}; local n=T.Wide09LoadMessages(out,v,I["vocab_wide_batch"],rr); local m={}; local z=T.Wide09MeasureMessages(out,m); local b=blob(z); local sr={}; local w=T.Wide09SaveMessages(out,b,sr); if(n==1 && w==z && same(b,I["vocab_wide_batch"])) print("alternate_batch:ok\n"); else print("alternate_batch:fail:"+n+":"+rr.malformed+"\n"); } }
`)
		script.WriteString(`{ local T=dofile("vocabdemo/schema_module.nut",true).require("VocabdemoTables"); local v={}; local ar={}; if(!T.AnnounceRead(v,I["unknown_body_ann"],ar) || ar.unknown!=1) print("unknown_body:announce_fail\n"); else { local out=[T.Wide00(),T.Wide00()]; local rr={}; local n=T.Wide00LoadMessages(out,v,I["unknown_body_batch"],rr); if(n==2 && rr.unknown==1 && !rr.malformed) print("unknown_body:ok\n"); else print("unknown_body:fail:"+n+":"+rr.unknown+":"+rr.malformed+"\n"); } }
`)
		enumPeer, enumCount := announcementWithPeerEntry(units[conn.unit], ir.TableVocabularyEntry{Id: 0x2234567890abcdef, Kind: ir.TableKindEnum})
		inputs["bad_enum_ref_ann"] = enumPeer
		inputs["bad_enum_ref_batch"] = messageBatchWithInnerRef(ir.TableMessageRefBits(enumCount), uint64(enumCount), uint64(enumCount+1))
		script.WriteString(`{ local T=dofile("vocabdemo/schema_module.nut",true).require("VocabdemoTables"); local v={}; local ar={}; T.AnnounceRead(v,I["bad_enum_ref_ann"],ar); local out=[T.Wide00()]; local rr={}; local n=T.Wide00LoadMessages(out,v,I["bad_enum_ref_batch"],rr); if(n==0 && rr.malformed) print("bad_enum_ref:ok\n"); else print("bad_enum_ref:fail:"+n+":"+rr.malformed+"\n"); }
`)
	}
	if units["backenddemo"] != nil {
		inputs["backend_ann"] = mustRead(t, filepath.Join(root, connections["backend_conn"].file))
		inputs["damaged"] = mustRead(t, filepath.Join(root, "testdata/wire/tables/backend_batch_damaged_second_message.bin"))
		vocab := ir.TableVocabulary(units["backenddemo"])
		nodeSlot := 0
		for i, e := range vocab {
			if e.Id == ir.TableNodeWireId {
				nodeSlot = i + 1
			}
		}
		inputs["reserved_node_batch"] = messageBatchRefs(ir.TableMessageRefBits(len(vocab)), uint64(nodeSlot), 0)
		nestedAnn, nestedCount := announcementWithPeerEntry(units["backenddemo"], ir.TableVocabularyEntry{Id: 0x334567890abcdef1, Kind: ir.TableKindTable})
		inputs["reserved_nested_ann"] = nestedAnn
		inputs["reserved_nested_batch"] = messageBatchRefs(ir.TableMessageRefBits(nestedCount), uint64(nestedCount), uint64(nodeSlot), 0, 0)
		script.WriteString(`{local T=dofile("backenddemo/schema_module.nut",true).require("BackenddemoTables");local v={};local ar={};T.AnnounceRead(v,I["backend_ann"],ar);local out=[T.LoginRequest()];local rr={};local n=T.LoginRequestLoadMessages(out,v,I["reserved_node_batch"],rr);if(n==0&&rr.malformed)print("reserved_node:ok\n");else print("reserved_node:fail:"+n+":"+rr.malformed+"\n");}
`)
		script.WriteString(`{local T=dofile("backenddemo/schema_module.nut",true).require("BackenddemoTables");local v={};local ar={};T.AnnounceRead(v,I["reserved_nested_ann"],ar);local out=[T.LoginRequest()];local rr={};local n=T.LoginRequestLoadMessages(out,v,I["reserved_nested_batch"],rr);if(n==0&&rr.malformed)print("reserved_nested:ok\n");else print("reserved_nested:fail:"+n+":"+rr.malformed+"\n");}
`)
		login := units["backenddemo"].Tables["LoginRequest"]
		var playerID ir.TableVocabularyEntry
		for _, field := range login.Fields {
			if field.Name == "player_id" {
				playerID = ir.TableFieldEntry(field)
				break
			}
		}
		peerPlayerID := playerID
		peerPlayerID.Shape.Packing = ir.TableMessageRanged
		peerPlayerID.Shape.Bits = 64
		peerPlayerID.Shape.Base = big.NewInt(0)
		inputs["changed_shape_ann"] = announcementWithChangedEntry(units["backenddemo"], playerID, peerPlayerID)
		region := units["backenddemo"].Enums["Region"]
		variantID := ir.TableWireId(region.VariantWireName(0))
		variantEntry := ir.TableVocabularyEntry{Id: variantID}
		wrongEnumEntry := ir.TableVocabularyEntry{Id: variantID, Kind: ir.TableKindU64}
		inputs["wrong_enum_sort_ann"] = announcementWithChangedEntry(units["backenddemo"], variantEntry, wrongEnumEntry)
		variantSlot, regionSlot := 0, 0
		for i, e := range vocab {
			if e.Id == variantID {
				variantSlot = i + 1
			}
		}
		for _, field := range units["backenddemo"].Tables["LoginRequest"].Fields {
			if field.Name == "region" {
				id := ir.TableFieldWireId(field)
				for i, e := range vocab {
					if e.Id == id {
						regionSlot = i + 1
					}
				}
			}
		}
		inputs["wrong_enum_sort_batch"] = messageBatchEnumField(ir.TableMessageRefBits(len(vocab)), uint64(regionSlot), uint64(variantSlot))
		script.WriteString(`{ local T=dofile("backenddemo/schema_module.nut",true).require("BackenddemoTables"); local v={};local ar={};T.AnnounceRead(v,I["wrong_enum_sort_ann"],ar);local out=[T.LoginRequest()];local rr={};local n=T.LoginRequestLoadMessages(out,v,I["wrong_enum_sort_batch"],rr);if(n==0&&rr.malformed)print("wrong_enum_sort:ok\n");else print("wrong_enum_sort:fail:"+n+":"+rr.malformed+"\n"); }
`)
		purchase := units["backenddemo"].Tables["StorePurchase"]
		var sku ir.TableVocabularyEntry
		for _, field := range purchase.Fields {
			if field.Name == "sku" {
				sku = ir.TableFieldEntry(field)
				break
			}
		}
		peerSKU := sku
		peerSKU.Shape.Max = 40
		skuSlot := 0
		for i, e := range ir.TableVocabulary(units["backenddemo"]) {
			if e.Id == sku.Id {
				skuSlot = i + 1
			}
		}
		inputs["wide_string_ann"] = announcementWithChangedEntry(units["backenddemo"], sku, peerSKU)
		inputs["wide_string_batch"] = messageBatchWithUnknownString(ir.TableMessageRefBits(len(ir.TableVocabulary(units["backenddemo"]))), uint64(skuSlot), strings.Repeat("x", 33), 40)
		script.WriteString(`{ local T=dofile("backenddemo/schema_module.nut",true).require("BackenddemoTables"); local v={}; local ar={}; T.AnnounceRead(v,I["wide_string_ann"],ar); local out=[T.StorePurchase()]; local rr={}; local n=T.StorePurchaseLoadMessages(out,v,I["wide_string_batch"],rr); if(n==1 && rr.clamped==1 && !rr.malformed) print("wide_string_clamp:ok\n"); else print("wide_string_clamp:fail:"+n+":"+rr.clamped+":"+rr.malformed+"\n"); }
`)
		script.WriteString("{ local T=dofile(\"backenddemo/schema_module.nut\",true).require(\"BackenddemoTables\"); local v={}; local ar={}; if(!T.AnnounceRead(v,I[\"changed_shape_ann\"],ar)) print(\"changed_shape:announce_fail\\n\"); else { local out=[T.LoginRequest()]; local rr={}; local n=T.LoginRequestLoadMessages(out,v,I[\"login_full_batch\"],rr); local m={}; local z=T.LoginRequestMeasureMessages(out,m); local b=blob(z); local sr={}; local w=T.LoginRequestSaveMessages(out,b,sr); if(n==1 && rr.kindMismatch==0 && w==z && same(b,I[\"login_full_batch\"])) print(\"changed_shape:ok\\n\"); else print(\"changed_shape:fail:\"+n+\":\"+rr.kindMismatch+\":\"+rr.malformed+\"\\n\"); } }\n")
		script.WriteString(`{ local T=dofile("backenddemo/schema_module.nut",true).require("BackenddemoTables"); local v={}; local ar={}; T.AnnounceRead(v,I["backend_ann"],ar); local storage=[T.Envelope(),T.Envelope(),T.Envelope()]; local rr={}; local n=T.EnvelopeLoadMessages(storage,v,I["damaged"],rr); if(n==1 && rr.malformed) { print("damage:ok\n"); } else { print("damage:bad:"+n+"\n"); } }
`)
	}
	script.WriteString("print(\"done\\n\");\n")
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	return sqrun.Run(t, dir, "main.nut", inputs)
}

func loadMessageUnits(t *testing.T, root string, connections map[string]squirrelMessageConnection) map[string]*ir.Unit {
	t.Helper()
	paths := map[string][]string{}
	for _, u := range manifestUnits(t, filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt")) {
		paths[u.key] = u.paths
	}
	units := map[string]*ir.Unit{}
	for _, c := range connections {
		if units[c.unit] == nil {
			units[c.unit] = loadUnit(t, root, paths[c.unit])
		}
	}
	return units
}

func TestSquirrelMessageFormMatchesTheGoldens(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	connections, rows := readMessageRows(t, root)
	units := loadMessageUnits(t, root, connections)
	fixed := 0
	for _, row := range rows {
		if st := units[connections[row.connection].unit].Tables[row.root]; st != nil && st.FixedDeclared {
			fixed++
		}
	}
	if fixed != 12 {
		t.Fatalf("manifest has %d message rows over a fixed root, want 12", fixed)
	}
	out := string(runSquirrelMessages(t, root, units, connections, rows, false))
	for _, row := range rows {
		if st := units[connections[row.connection].unit].Tables[row.root]; st == nil || !st.FixedDeclared {
			if !strings.Contains(out, row.name+":absent") {
				t.Errorf("%s: the variable table %s carries message verbs: %s", row.name, row.root, out)
			}
			continue
		}
		if !strings.Contains(out, row.name+":ok") {
			t.Errorf("%s failed message round trip: %s", row.name, out)
		}
	}
	if !strings.Contains(out, "alternate:ok:") {
		t.Errorf("alternate peer announcement did not resolve: %s", out)
	}
	if !strings.Contains(out, "alternate_unknown:ok") {
		t.Errorf("unknown announcement field was not skipped and counted: %s", out)
	}
	for _, unit := range []string{"backenddemo", "vocabdemo", "vocab9demo", "graphdemo"} {
		if !strings.Contains(out, "parsed_"+unit+":ok") {
			t.Errorf("generic peer announcement parsing failed for %s: %s", unit, out)
		}
	}
	if !strings.Contains(out, "alternate_batch:ok") {
		t.Errorf("batch did not load through an alternate peer announcement: %s", out)
	}
	if !strings.Contains(out, "bad_enum_ref:ok") {
		t.Errorf("out-of-range inner enum reference was not terminal damage: %s", out)
	}
	if !strings.Contains(out, "unknown_body:ok") {
		t.Errorf("peer-only shaped body field was not skipped by its announced shape: %s", out)
	}
	if !strings.Contains(out, "wrong_enum_sort:ok") {
		t.Errorf("known enum accepted an announced payload kind: %s", out)
	}
	if !strings.Contains(out, "changed_shape:ok") {
		t.Errorf("known field was not decoded by its changed peer shape: %s", out)
	}
	if !strings.Contains(out, "wide_string_clamp:ok") {
		t.Errorf("widened string bound was not clamped and reported: %s", out)
	}
	if !strings.Contains(out, "reserved_node:ok") {
		t.Errorf("reserved node ID was accepted as a body field: %s", out)
	}
	if !strings.Contains(out, "reserved_nested:ok") {
		t.Errorf("reserved node ID was accepted in a nested peer body: %s", out)
	}
	if !strings.Contains(out, "damage:ok") {
		t.Errorf("damage in second body was not terminal for the batch: %s", out)
	}
	if !strings.Contains(out, "done") {
		t.Fatal("probe did not finish")
	}
	t.Logf("%d manifest message rows inspected: %d fixed-root byte comparisons", len(rows), fixed)
}

func TestSquirrelMessageFormNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]squirrelMessageConnection{"backend_conn": {unit: "backenddemo", file: "testdata/wire/tables/backend_conn.bin"}}
	units := loadMessageUnits(t, root, connections)
	rows := []squirrelMessageRow{{name: "login_full", connection: "backend_conn", root: "LoginRequest", batch: "testdata/wire/tables/login_full_message.bin"}}
	out := string(runSquirrelMessages(t, root, units, connections, rows, true))
	if !strings.Contains(out, "login_full:load:-1:") || !strings.Contains(out, "login_full:save:") {
		t.Fatalf("one-short batch-count negative control did not make load fail and byte comparison red: %s", out)
	}
}

func TestSquirrelMessageFormSetsPresentOptionalField(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string][]string{}
	for _, u := range manifestUnits(t, filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt")) {
		paths[u.key] = u.paths
	}
	u := loadUnit(t, root, paths["messagedemo"])
	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if strings.Contains(name, "/") {
			continue
		}
		writeFile(t, filepath.Join(dir, name), b)
	}
	armFound := false
	var voidEntry ir.TableVocabularyEntry
	union := u.Unions["ToolBody"]
	if union == nil {
		union = u.TableUnions["ToolBody"]
	}
	if union == nil {
		t.Fatal("ToolBody union missing")
	}
	for _, arm := range union.Variants {
		if arm.Void() && arm.Name == "ack" {
			voidEntry = ir.TableArmEntry(arm)
			armFound = true
		}
	}
	if !armFound {
		t.Fatal("ToolBody ack void arm missing")
	}
	wrongVoid := voidEntry
	wrongVoid.Kind = 0
	inputs := map[string][]byte{"wrong_union_sort_ann": announcementWithChangedEntry(u, voidEntry, wrongVoid)}
	module := squirreltable.ModuleName(u)
	script := fmt.Sprintf(`local T=dofile("schema_module.nut",true).require(%q); local v={};local ar={}; if(!T.AnnounceRead(v,T.TableMessageAnnouncement,ar)){print("announce:fail\n");return;} local x=T.InsertText();x.Modes=[];x.ModesPresent=true;local mr={};local z=T.InsertTextMeasureMessages([x],mr);local b=blob(z);local sr={};T.InsertTextSaveMessages([x],b,sr);local out=[T.InsertText()];local rr={};local n=T.InsertTextLoadMessages(out,v,b,rr);if(n==1&&out[0].ModesPresent)print("optional:ok\n");else print("optional:fail:"+n+":"+rr.malformed+"\n");`, module)
	script += `local I=dofile("inputs.nut",true);local T=dofile("schema_module.nut",true).require("` + module + `");local x=T.ToolMessage();local tag=0;foreach(a in T.ToolBodyDescriptor.arms)if(a.member=="Ack")tag=a.tag;x.Body.Type=tag;local mr={};local z=T.ToolMessageMeasureMessages([x],mr);local b=blob(z);local sr={};T.ToolMessageSaveMessages([x],b,sr);local v={};local ar={};T.AnnounceRead(v,I["wrong_union_sort_ann"],ar);local out=[T.ToolMessage()];local rr={};local n=T.ToolMessageLoadMessages(out,v,b,rr);if(n==0&&rr.malformed)print("wrong_union_sort:ok\n");else print("wrong_union_sort:fail:"+n+":"+rr.malformed+"\n");`
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	if !strings.Contains(out, "optional:ok") {
		t.Fatalf("present optional field did not round trip: %s", out)
	}
	if !strings.Contains(out, "wrong_union_sort:ok") {
		t.Fatalf("known void union arm accepted kind zero: %s", out)
	}
}

func TestSquirrelBytesMessagePairs(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string][]string{}
	for _, u := range manifestUnits(t, filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt")) {
		paths[u.key] = u.paths
	}
	u := loadUnit(t, root, paths["tabledemo"])
	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
	script := fmt.Sprintf(`local T=dofile("schema_module.nut",true).require(%q);local v={};local ar={};if(!T.AnnounceRead(v,T.TableMessageAnnouncement,ar)){print("bytes:announce_fail\n");return;}local x=T.ProfileConfig();local b=blob(2);b[0]=0x41;b[1]=0x42;x.Icon=b;local mr={};local z=T.ProfileConfigMeasureMessages([x],mr);local wire=blob(z);local sr={};local w=T.ProfileConfigSaveMessages([x],wire,sr);local out=[T.ProfileConfig()];local rr={};local n=T.ProfileConfigLoadMessages(out,v,wire,rr);if(n==1&&w==z&&out[0].Icon.len()==2&&out[0].Icon[0]==0x41&&out[0].Icon[1]==0x42&&!rr.malformed)print("bytes:ok\n");else print("bytes:fail:"+n+":"+rr.malformed+":"+rr.unknown+"\n");`, squirreltable.ModuleName(u))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	inputs := map[string][]byte{"ann": announcementWithEntries(u, ir.TableVocabulary(u))}
	out := string(sqrun.Run(t, dir, "main.nut", inputs))
	if !strings.Contains(out, "bytes:ok") {
		t.Fatalf("bytes message pair round trip failed: %s", out)
	}
}

func TestSquirrelMessageWidensAndSignExtends(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"test/tables/Bases.schema"})
	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
	entries := ir.TableVocabulary(u)
	changed := append([]ir.TableVocabularyEntry(nil), entries...)
	var smallSlot uint64
	for i, e := range entries {
		if e.Id == ir.TableWireId("small_a") {
			smallSlot = uint64(i + 1)
			changed[i] = ir.TableVocabularyEntry{Id: e.Id, Kind: ir.TableKindI8, Shape: ir.TableMessageShape{Packing: ir.TableMessageRaw}}
		}
	}
	if smallSlot == 0 {
		t.Fatal("small_a vocabulary field missing")
	}
	ann := announcementWithEntries(u, changed)
	refBits := ir.TableMessageRefBits(len(entries))
	batch := messageBatchScalar(refBits, smallSlot, 0xfe, 8)
	script := fmt.Sprintf(`local I=dofile("inputs.nut",true);local T=dofile("schema_module.nut",true).require(%q);local v={};local ar={};if(!T.AnnounceRead(v,I.peerann,ar)){print("widen:announce_fail\n");return;}local out=[T.Bases()];local rr={};local n=T.BasesLoadMessages(out,v,I.batch,rr);if(n==1&&out[0].SmallA==-2&&rr.widened==1&&rr.kindMismatch==0&&!rr.malformed)print("widen:ok\n");else print("widen:fail:"+n+":"+out[0].SmallA+":"+rr.widened+":"+rr.kindMismatch+":"+rr.malformed+"\n");`, squirreltable.ModuleName(u))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	out := string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"peerann": ann, "batch": batch}))
	if !strings.Contains(out, "widen:ok") {
		t.Fatalf("signed and float widening message read failed: %s", out)
	}
}

func TestSquirrelMessageUnsignedRangeClampAcrossSignBit(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"tables/examples/Ranges.schema"})
	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
	entries := ir.TableVocabulary(u)
	for _, tc := range []struct {
		name     string
		field    string
		peerKind uint8
		width    int
		value    uint64
	}{
		{name: "span_same_kind", field: "u64_span", peerKind: ir.TableKindU64, width: 64, value: 1},
		{name: "narrow_same_kind", field: "u64_low", peerKind: ir.TableKindU64, width: 64, value: 1},
		{name: "span_widened_u16", field: "u64_span", peerKind: ir.TableKindU16, width: 16, value: 1},
		{name: "span_widened_u32", field: "u64_span", peerKind: ir.TableKindU32, width: 32, value: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var slot uint64
			var fieldID uint64
			for i, e := range entries {
				if e.Id == ir.TableWireId(tc.field) {
					slot = uint64(i + 1)
					fieldID = e.Id
				}
			}
			if slot == 0 {
				t.Fatalf("%s vocabulary field missing", tc.field)
			}
			peer := ir.TableVocabularyEntry{Id: fieldID, Kind: tc.peerKind, Shape: ir.TableMessageShape{Packing: ir.TableMessageRaw}}
			ann := announcementWithChangedEntry(u, peer, peer)
			batch := messageBatchScalar(ir.TableMessageRefBits(len(entries)), slot, tc.value, tc.width)
			field := "U64Span"
			if tc.field == "u64_low" {
				field = "U64Low"
			}
			script := fmt.Sprintf(`local I=dofile("inputs.nut",true);local T=dofile("schema_module.nut",true).require(%q);local v={};local ar={};if(!T.AnnounceRead(v,I.peerann,ar)){print("u64range:announce_fail\n");return;}local out=[T.RangedUnsigned()];local rr={};local n=T.RangedUnsignedLoadMessages(out,v,I.batch,rr);if(n==1&&out[0].%s==1&&rr.clamped==0&&!rr.malformed)print("u64range:ok\n");else print("u64range:fail:"+n+":"+out[0].%s+":"+rr.clamped+":"+rr.malformed+"\n");`, squirreltable.ModuleName(u), field, field)
			writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
			out := string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"peerann": ann, "batch": batch}))
			if !strings.Contains(out, "u64range:ok") {
				t.Fatalf("unsigned range clamp across sign bit failed for %s: %s", tc.field, out)
			}
		})
	}
}

func messageBatchScalar(refBits int, slot uint64, value uint64, width int) []byte {
	out := []byte{2, 0}
	bit := 16
	put := func(x uint64, n int) {
		for i := range n {
			at := bit / 8
			if at == len(out) {
				out = append(out, 0)
			}
			if (x>>i)&1 != 0 {
				out[at] |= 1 << (bit % 8)
			}
			bit++
		}
	}
	put(slot, refBits)
	put(value, width)
	put(0, refBits)
	for bit%8 != 0 {
		put(0, 1)
	}
	return out
}

func TestSquirrelMessageWidenFloat32ToFloat64(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	var paths []string
	for _, unit := range units {
		if unit.key == "tabledemo" {
			paths = unit.paths
			break
		}
	}
	if paths == nil {
		t.Fatal("tabledemo manifest unit missing")
	}
	u := loadUnit(t, root, paths)
	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
	entries := ir.TableVocabulary(u)
	var target ir.TableVocabularyEntry
	var slot uint64
	for i, e := range entries {
		if e.Id == ir.TableWireId("precision") {
			target = e
			slot = uint64(i + 1)
			break
		}
	}
	if slot == 0 {
		t.Fatal("precision vocabulary entry missing")
	}
	peer := ir.TableVocabularyEntry{Id: target.Id, Kind: ir.TableKindF32, Shape: ir.TableMessageShape{Packing: ir.TableMessageRaw}}
	ann := announcementWithChangedEntry(u, target, peer)
	batch := messageBatchScalar(ir.TableMessageRefBits(len(entries)), slot, 0x3fa00000, 32)
	script := fmt.Sprintf(`local I=dofile("inputs.nut",true);local T=dofile("schema_module.nut",true).require(%q);local v={};local ar={};if(!T.AnnounceRead(v,I.peerann,ar)){print("widen-float:announce_fail\n");return;}local out=[T.ProfileConfig()];local rr={};local n=T.ProfileConfigLoadMessages(out,v,I.batch,rr);if(n==1&&out[0].Precision==1.25&&rr.widened==1&&rr.kindMismatch==0&&!rr.malformed)print("widen-float:ok\n");else print("widen-float:fail:"+n+":"+out[0].Precision+":"+rr.widened+":"+rr.kindMismatch+":"+rr.malformed+"\n");`, squirreltable.ModuleName(u))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	out := string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"peerann": ann, "batch": batch}))
	if !strings.Contains(out, "widen-float:ok") {
		t.Fatalf("float widening message read failed: %s", out)
	}
}

func TestSquirrelMessageWidenArrayElements(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"test/tables/Bases.schema"})
	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
	entries := ir.TableVocabulary(u)
	changed := append([]ir.TableVocabularyEntry(nil), entries...)
	var slot uint64
	for i, e := range entries {
		if e.Id == ir.TableWireId("few") {
			slot = uint64(i + 1)
			changed[i].Shape.Elem = ir.TableKindU16
			changed[i].Shape.Inner = &ir.TableMessageShape{Packing: ir.TableMessageRaw}
		}
	}
	if slot == 0 {
		t.Fatal("few vocabulary field missing")
	}
	ann := announcementWithEntries(u, changed)
	batch := messageBatchArrayElements(ir.TableMessageRefBits(len(entries)), slot, []uint64{5, 7})
	script := fmt.Sprintf(`local I=dofile("inputs.nut",true);local T=dofile("schema_module.nut",true).require(%q);local v={};local ar={};if(!T.AnnounceRead(v,I.peerann,ar)){print("widen-array:announce_fail\n");return;}local out=[T.Bases()];local rr={};local n=T.BasesLoadMessages(out,v,I.batch,rr);if(n==1&&out[0].Few.len()==2&&out[0].Few[0]==5&&out[0].Few[1]==7&&rr.widened==2&&rr.kindMismatch==0&&!rr.malformed)print("widen-array:ok\n");else print("widen-array:fail:"+n+":"+out[0].Few.len()+":"+rr.widened+":"+rr.kindMismatch+":"+rr.malformed+"\n");`, squirreltable.ModuleName(u))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
	out := string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"peerann": ann, "batch": batch}))
	if !strings.Contains(out, "widen-array:ok") {
		t.Fatalf("array element widening failed: %s", out)
	}
}

func messageBatchArrayElements(refBits int, slot uint64, values []uint64) []byte {
	out := []byte{2, 0}
	bit := 16
	put := func(x uint64, n int) {
		for i := range n {
			at := bit / 8
			if at == len(out) {
				out = append(out, 0)
			}
			if (x>>i)&1 != 0 {
				out[at] |= 1 << (bit % 8)
			}
			bit++
		}
	}
	put(slot, refBits)
	put(0, 2)
	for _, v := range values {
		put(v, 16)
	}
	put(0, refBits)
	for bit%8 != 0 {
		put(0, 1)
	}
	return out
}
