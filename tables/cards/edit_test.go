package cards

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
)

type cardCfg struct {
	id       string
	body     string
	paths    []string
	new_     []string
	shared   []string
	needs    []string
	test     string
	done     string
	tier     Tier
	priority Priority
	column   Column
	holder   string
}

func buildCfg(t *testing.T, c cardCfg) *Record {
	t.Helper()
	var b CardBuilder
	if !b.Init() {
		t.Fatal("CardBuilder.Init failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	if c.id == "" {
		c.id = "x"
	}
	setStr(root.Id[:], &root.IdLength, c.id)
	root.Tier = c.tier
	root.Priority = c.priority
	root.Column = c.column
	setStr(root.Holder[:], &root.HolderLength, c.holder)
	if c.test == "" {
		c.test = "T"
	}
	setStr(root.Test[:], &root.TestLength, c.test)
	for _, p := range c.paths {
		e := CardPathsAdd(&b.Main, &root.Paths)
		if e == nil {
			t.Fatal("CardPathsAdd nil")
		}
		setStr(e.S[:], &e.SLength, p)
	}
	for _, p := range c.new_ {
		e := CardNewAdd(&b.Main, &root.New)
		if e == nil {
			t.Fatal("CardNewAdd nil")
		}
		setStr(e.S[:], &e.SLength, p)
	}
	for _, p := range c.shared {
		e := CardSharedAdd(&b.Main, &root.Shared)
		if e == nil {
			t.Fatal("CardSharedAdd nil")
		}
		setStr(e.S[:], &e.SLength, p)
	}
	for _, n := range c.needs {
		e := CardNeedsAdd(&b.Main, &root.Needs)
		if e == nil {
			t.Fatal("CardNeedsAdd nil")
		}
		setStr(e.S[:], &e.SLength, n)
	}
	if c.body != "" {
		TableStringEmplace(&b.Main, &root.Body, []byte(c.body))
	}
	if c.done != "" {
		TableStringEmplace(&b.Main, &root.Done, []byte(c.done))
	}
	if !b.Lock() {
		t.Fatal("CardBuilder.Lock failed")
	}
	packed := b.AsConst()
	size := CardMeasure(packed)
	if size < 0 {
		t.Fatal("CardMeasure refused")
	}
	wire := make([]byte, size)
	if CardSave(packed, wire) != size {
		t.Fatal("CardSave short")
	}
	rec, _, err := Load(wire)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return rec
}

func frameBytes(c cardCfg) []byte {
	var out []byte
	u64 := func(v uint64) {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], v)
		out = append(out, b[:]...)
	}
	text := func(s string) {
		u64(uint64(len(s)))
		out = append(out, s...)
	}
	list := func(items []string) {
		u64(uint64(len(items)))
		for _, s := range items {
			text(s)
		}
	}
	text(c.body)
	list(c.paths)
	list(c.new_)
	list(c.shared)
	text(c.test)
	text(c.done)
	return out
}

func TestTaskHashFramesLogicalFields(t *testing.T) {
	t.Parallel()
	base := cardCfg{body: "a", paths: []string{"bc"}, test: "T", done: "d"}
	rec := buildCfg(t, base)
	want := sha256.Sum256(frameBytes(base))
	got, err := TaskHash(rec)
	if err != nil {
		t.Fatalf("TaskHash: %v", err)
	}
	if got != want {
		t.Fatalf("frame pin mismatch:\n got %x\nwant %x", got, want)
	}
	included := []struct {
		name string
		cfg  cardCfg
	}{
		{"body", cardCfg{body: "ab", paths: []string{"bc"}, test: "T", done: "d"}},
		{"paths", cardCfg{body: "a", paths: []string{"b"}, test: "T", done: "d"}},
		{"new", cardCfg{body: "a", paths: []string{"bc"}, new_: []string{"n"}, test: "T", done: "d"}},
		{"shared", cardCfg{body: "a", paths: []string{"bc"}, shared: []string{"s"}, test: "T", done: "d"}},
		{"test", cardCfg{body: "a", paths: []string{"bc"}, test: "T2", done: "d"}},
		{"done", cardCfg{body: "a", paths: []string{"bc"}, test: "T", done: "d2"}},
		{"boundary", cardCfg{body: "ab", paths: []string{"c"}, test: "T", done: "d"}},
		{"empty entry", cardCfg{body: "a", paths: []string{"bc", ""}, test: "T", done: "d"}},
	}
	baseHash, _ := TaskHash(rec)
	for _, tc := range included {
		r := buildCfg(t, tc.cfg)
		h, err := TaskHash(r)
		if err != nil {
			t.Fatalf("%s: TaskHash: %v", tc.name, err)
		}
		if h == baseHash {
			t.Fatalf("%s change did not alter the task hash", tc.name)
		}
	}
	excluded := []struct {
		name string
		cfg  cardCfg
	}{
		{"needs", cardCfg{body: "a", paths: []string{"bc"}, test: "T", done: "d", needs: []string{"n"}}},
		{"tier", cardCfg{body: "a", paths: []string{"bc"}, test: "T", done: "d", tier: Tierheavy}},
		{"priority", cardCfg{body: "a", paths: []string{"bc"}, test: "T", done: "d", priority: Prioritycritical}},
		{"column", cardCfg{body: "a", paths: []string{"bc"}, test: "T", done: "d", column: Columnworking}},
		{"holder", cardCfg{body: "a", paths: []string{"bc"}, test: "T", done: "d", holder: "h"}},
		{"id", cardCfg{body: "a", paths: []string{"bc"}, test: "T", done: "d", id: "other"}},
	}
	for _, tc := range excluded {
		r := buildCfg(t, tc.cfg)
		h, err := TaskHash(r)
		if err != nil {
			t.Fatalf("%s: TaskHash: %v", tc.name, err)
		}
		if h != baseHash {
			t.Fatalf("%s change altered the task hash", tc.name)
		}
	}
}

func TestFieldEditLeavesTheTaskHashAlone(t *testing.T) {
	t.Parallel()
	rec := buildCfg(t, cardCfg{body: "body", paths: []string{"p1"}, test: "T", done: "d", needs: []string{"n1"}})
	origWire := rec.Wire()
	origHash, err := TaskHash(rec)
	if err != nil {
		t.Fatalf("TaskHash: %v", err)
	}
	if err := SetNeeds(rec, []string{"n1", "n2"}); err != nil {
		t.Fatalf("SetNeeds: %v", err)
	}
	if err := SetTier(rec, Tierfrontier); err != nil {
		t.Fatalf("SetTier: %v", err)
	}
	if err := SetPriority(rec, Priorityblocker); err != nil {
		t.Fatalf("SetPriority: %v", err)
	}
	if err := SetColumn(rec, Columnlanded); err != nil {
		t.Fatalf("SetColumn: %v", err)
	}
	if err := SetHolder(rec, "new-holder"); err != nil {
		t.Fatalf("SetHolder: %v", err)
	}
	if bytes.Equal(rec.Wire(), origWire) {
		t.Fatal("identity/machine edits did not change the wire")
	}
	hash, err := TaskHash(rec)
	if err != nil {
		t.Fatalf("TaskHash after edits: %v", err)
	}
	if hash != origHash {
		t.Fatal("identity/machine edits changed the task hash")
	}
	if got := stringData(&rec.root.Body, nil); !bytes.Equal(got, []byte("body")) {
		t.Fatalf("body bytes changed by an identity edit: %q", got)
	}
	if err := SetPaths(rec, []string{"p1", "p2"}); err != nil {
		t.Fatalf("SetPaths: %v", err)
	}
	if bytes.Equal(rec.Wire(), origWire) {
		t.Fatal("paths edit did not change the wire")
	}
	hash, err = TaskHash(rec)
	if err != nil {
		t.Fatalf("TaskHash after paths edit: %v", err)
	}
	if hash == origHash {
		t.Fatal("a task-bearing paths edit left the task hash unchanged")
	}
	if _, _, err := ToJSON(rec); err != nil {
		t.Fatalf("ToJSON after edits: %v", err)
	}
	if _, err := TaskHash(rec); err != nil {
		t.Fatalf("TaskHash after builder cleanup: %v", err)
	}
	if !bytes.Equal(rec.Wire(), rec.Wire()) {
		t.Fatal("Wire() unstable after edits")
	}
}

func TestFieldEditFailureLeavesOriginalWire(t *testing.T) {
	t.Parallel()
	rec := buildCfg(t, cardCfg{body: "body", paths: []string{"p1"}, test: "T", done: "d", needs: []string{"n1"}, holder: "h"})
	origWire := rec.Wire()

	if err := SetNeeds(rec, []string{"ok", "another"}); err != nil {
		t.Fatalf("SetNeeds(valid) failed: %v", err)
	}
	if bytes.Equal(rec.Wire(), origWire) {
		t.Fatal("a valid SetNeeds did not change the wire")
	}
	current := rec.Wire()

	if err := SetNeeds(rec, []string{"ok", "too-long-64-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" + "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}); err == nil {
		t.Fatal("SetNeeds accepted an over-bound element")
	}
	if !bytes.Equal(rec.Wire(), current) {
		t.Fatal("over-bound element SetNeeds published a wire")
	}

	badNeeds := make([]string, MaxListEntries+1)
	for i := range badNeeds {
		badNeeds[i] = "d"
	}
	if err := SetNeeds(rec, badNeeds); err == nil {
		t.Fatal("SetNeeds(513) accepted over-bound list")
	}
	if !bytes.Equal(rec.Wire(), current) {
		t.Fatal("over-bound SetNeeds published a wire")
	}

	if err := SetNeeds(rec, []string{"ok", "bad\x00nul"}); err == nil {
		t.Fatal("SetNeeds accepted a NUL element")
	}
	if !bytes.Equal(rec.Wire(), current) {
		t.Fatal("NUL SetNeeds published a wire")
	}

	if err := SetTier(rec, Tier(99)); err == nil {
		t.Fatal("SetTier accepted an undeclared variant")
	}
	if !bytes.Equal(rec.Wire(), current) {
		t.Fatal("invalid SetTier published a wire")
	}

	if err := SetHolder(rec, string(make([]byte, 33))); err == nil {
		t.Fatal("SetHolder accepted an over-bound holder")
	}
	if !bytes.Equal(rec.Wire(), current) {
		t.Fatal("over-bound SetHolder published a wire")
	}

	if err := SetPaths(rec, []string{"p1", "p2"}); err != nil {
		t.Fatalf("valid SetPaths after refusals failed: %v", err)
	}
	if bytes.Equal(rec.Wire(), origWire) {
		t.Fatal("valid edit after refusals did not land")
	}
}

func TestListBoundIsARefusal(t *testing.T) {
	t.Parallel()
	rec := buildCfg(t, cardCfg{test: "T"})

	ok := make([]string, MaxListEntries)
	for i := range ok {
		ok[i] = "p"
	}
	if err := SetPaths(rec, ok); err != nil {
		t.Fatalf("SetPaths(512) refused: %v", err)
	}
	rec2, _, err := Load(rec.Wire())
	if err != nil {
		t.Fatalf("reload of 512 paths: %v", err)
	}
	if rec2.root.Paths.Count != MaxListEntries {
		t.Fatalf("paths count after save/load = %d, want %d", rec2.root.Paths.Count, MaxListEntries)
	}

	origWire := rec.Wire()
	over := make([]string, MaxListEntries+1)
	for i := range over {
		over[i] = "p"
	}
	err = SetPaths(rec, over)
	if err == nil {
		t.Fatal("SetPaths(513) accepted")
	}
	if !strings.Contains(err.Error(), "paths") || !strings.Contains(err.Error(), strconv.Itoa(MaxListEntries)) {
		t.Fatalf("SetPaths(513) refusal does not name the field and bound: %v", err)
	}
	if !bytes.Equal(rec.Wire(), origWire) {
		t.Fatal("SetPaths(513) published a wire")
	}

	needRec := buildCfg(t, cardCfg{test: "T"})
	needOk := make([]string, MaxListEntries)
	for i := range needOk {
		needOk[i] = "d"
	}
	if err := SetNeeds(needRec, needOk); err != nil {
		t.Fatalf("SetNeeds(512) refused: %v", err)
	}
	needReload, _, err := Load(needRec.Wire())
	if err != nil {
		t.Fatalf("reload of 512 needs: %v", err)
	}
	if needReload.root.Needs.Count != MaxListEntries {
		t.Fatalf("needs count after save/load = %d, want %d", needReload.root.Needs.Count, MaxListEntries)
	}
	needOver := make([]string, MaxListEntries+1)
	for i := range needOver {
		needOver[i] = "d"
	}
	origNeed := needRec.Wire()
	needErr := SetNeeds(needRec, needOver)
	if needErr == nil {
		t.Fatal("SetNeeds(513) accepted")
	}
	if !strings.Contains(needErr.Error(), "needs") || !strings.Contains(needErr.Error(), strconv.Itoa(MaxListEntries)) {
		t.Fatalf("SetNeeds(513) refusal does not name the field and bound: %v", needErr)
	}
	if !bytes.Equal(needRec.Wire(), origNeed) {
		t.Fatal("SetNeeds(513) published a wire")
	}

	var sb strings.Builder
	sb.WriteString(`{"id":"x","test":"T","paths":[`)
	for i := 0; i < MaxListEntries+1; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"s":"p"}`)
	}
	sb.WriteString(`]}`)
	if _, err := FromJSON([]byte(sb.String())); err == nil {
		t.Fatal("FromJSON accepted 513 paths: the setter is not the sole bound")
	}
}
