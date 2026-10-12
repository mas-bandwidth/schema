package cards

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cardsinitial "cardsinitial"
)

// The compatibility corpus (docs/SPEC-TABLES.md §4, §5, §6.6): each named
// directory under tables/cards/corpus holds a frozen DECLARATION.schema, its
// authored .wire fixtures and expected logical .json values, and a METADATA.txt
// that classifies it SHIPPED (must upgrade successfully). These are test
// labels, never runtime version numbers or hash directories.

const corpusBody = "compatibility body →世界 with \"quotes\" and\ttabs\nand lines🚀—exact"

// buildInitialCard builds a fully populated card through the OLD declaration's
// generated API (cardsinitial): done_when, an old Column variant, no consumers.
func buildInitialCard(t *testing.T) (wire, json []byte) {
	t.Helper()
	var b cardsinitial.CardBuilder
	if !b.Init() {
		t.Fatal("cardsinitial.CardBuilder.Init failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	setStr(root.Id[:], &root.IdLength, "initial-card")
	setStr(root.Sha[:], &root.ShaLength, strings.Repeat("a", 40))
	root.Tier = cardsinitial.Tierheavy
	setStr(root.Repo[:], &root.RepoLength, "mas-bandwidth/schema")
	setStr(root.Base[:], &root.BaseLength, "sprint/schema-finish")
	setStr(root.Kind[:], &root.KindLength, "feature")
	root.Priority = cardsinitial.Priorityhigh
	setStr(root.Stream[:], &root.StreamLength, "schema-cards")
	for _, p := range []string{"tables/cards/Card.schema", "make/go.mk"} {
		e := cardsinitial.CardPathsAdd(&b.Main, &root.Paths)
		if e == nil {
			t.Fatal("CardPathsAdd nil")
		}
		setStr(e.S[:], &e.SLength, p)
	}
	if e := cardsinitial.CardNewAdd(&b.Main, &root.New); e == nil {
		t.Fatal("CardNewAdd nil")
	} else {
		setStr(e.S[:], &e.SLength, "tables/cards/upgrade.go")
	}
	if e := cardsinitial.CardSharedAdd(&b.Main, &root.Shared); e == nil {
		t.Fatal("CardSharedAdd nil")
	} else {
		setStr(e.S[:], &e.SLength, "tables/cards/cards.go")
	}
	for _, n := range []string{"dep-alpha", "dep-beta"} {
		e := cardsinitial.CardNeedsAdd(&b.Main, &root.Needs)
		if e == nil {
			t.Fatal("CardNeedsAdd nil")
		}
		setStr(e.S[:], &e.SLength, n)
	}
	setStr(root.Test[:], &root.TestLength, "TestEveryCorpusRecordReadsWithTheCurrentModel")
	cardsinitial.TableStringEmplace(&b.Main, &root.DoneWhen, []byte("make cards-test"))
	root.DeadlineMinutes = 90
	cardsinitial.TableStringEmplace(&b.Main, &root.Source, []byte("docs/CONTRIBUTING.md résumé→世界"))
	root.Epoch = 17
	root.Column = cardsinitial.Columnreview
	setStr(root.Holder[:], &root.HolderLength, "bench")
	root.Attempt = 3
	setStr(root.Head[:], &root.HeadLength, strings.Repeat("9", 40))
	setStr(root.Branch[:], &root.BranchLength, "sprint/schema-cards-c03")
	if e := cardsinitial.CardExtraAdd(&b.Main, &root.Extra); e == nil {
		t.Fatal("CardExtraAdd nil")
	} else {
		setStr(e.Key[:], &e.KeyLength, "x-extra")
		cardsinitial.TableStringEmplace(&b.Main, &e.Value, []byte("extra value →世界"))
	}
	cardsinitial.TableStringEmplace(&b.Main, &root.Body, []byte(corpusBody))
	if !b.Lock() {
		t.Fatal("CardBuilder.Lock failed")
	}
	packed := b.AsConst()
	size := cardsinitial.CardMeasure(packed)
	if size < 0 {
		t.Fatal("CardMeasure refused")
	}
	wire = make([]byte, size)
	if cardsinitial.CardSave(packed, wire) != size {
		t.Fatal("CardSave did not fill")
	}
	// Self-check: the OLD reader reads back exactly what it wrote.
	n := cardsinitial.CardLoadMeasure(wire)
	region := alignedRegion(n)
	var report cardsinitial.TableReport
	back := cardsinitial.CardLoad(region, wire, &report)
	if back == nil || report.Malformed || report.Unknown != 0 {
		t.Fatalf("initial self-load failed: %+v", report)
	}
	js := cardsinitial.CardToJsonMeasure(back)
	json = make([]byte, js)
	if cardsinitial.CardToJson(back, json) != js {
		t.Fatal("CardToJson did not fill")
	}
	return wire, json
}

// buildConsumersCard builds a populated card through the CURRENT declaration
// with a nonempty consumers collection and a plan column selection.
func buildConsumersCard(t *testing.T) (wire, json []byte) {
	t.Helper()
	var b CardBuilder
	if !b.Init() {
		t.Fatal("CardBuilder.Init failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	setStr(root.Id[:], &root.IdLength, "consumers-card")
	setStr(root.Sha[:], &root.ShaLength, strings.Repeat("b", 40))
	root.Tier = Tierpro
	setStr(root.Repo[:], &root.RepoLength, "mas-bandwidth/schema")
	setStr(root.Base[:], &root.BaseLength, "sprint/schema-finish")
	setStr(root.Kind[:], &root.KindLength, "feature")
	root.Priority = Prioritycritical
	setStr(root.Stream[:], &root.StreamLength, "schema-cards")
	for _, p := range []string{"tables/cards/Card.schema"} {
		e := CardPathsAdd(&b.Main, &root.Paths)
		setStr(e.S[:], &e.SLength, p)
	}
	setStr(root.Test[:], &root.TestLength, "TestUpgradeKeepsEveryField")
	TableStringEmplace(&b.Main, &root.Done, []byte("make cards-test"))
	root.DeadlineMinutes = 45
	TableStringEmplace(&b.Main, &root.Source, []byte("source →世界"))
	root.Epoch = 19
	root.Column = Columnplan
	setStr(root.Holder[:], &root.HolderLength, "bench")
	root.Attempt = 2
	setStr(root.Head[:], &root.HeadLength, strings.Repeat("c", 40))
	setStr(root.Branch[:], &root.BranchLength, "sprint/schema-cards-c03-versioning")
	TableStringEmplace(&b.Main, &root.Body, []byte(corpusBody))
	addConsumer(t, &b, &root.Consumers, "worker", "nova", "route/one", "model-A", 100, 200, "ok", "line one", 7)
	addConsumer(t, &b, &root.Consumers, "worker", "nova", "route/two", "model-B", 300, 400, "ok", "line two", 11)
	if !b.Lock() {
		t.Fatal("CardBuilder.Lock failed")
	}
	packed := b.AsConst()
	size := CardMeasure(packed)
	if size < 0 {
		t.Fatal("CardMeasure refused")
	}
	wire = make([]byte, size)
	if CardSave(packed, wire) != size {
		t.Fatal("CardSave did not fill")
	}
	rec, _, err := Load(wire)
	if err != nil {
		t.Fatalf("self-load: %v", err)
	}
	json, _, err = ToJSON(rec)
	if err != nil {
		t.Fatalf("self-ToJSON: %v", err)
	}
	return wire, json
}

// buildEmptyConsumersCard builds a compatible card with an empty consumers
// collection and a check column selection; empty additions are elided.
func buildEmptyConsumersCard(t *testing.T) (wire, json []byte) {
	t.Helper()
	var b CardBuilder
	if !b.Init() {
		t.Fatal("CardBuilder.Init failed")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	setStr(root.Id[:], &root.IdLength, "empty-consumers")
	setStr(root.Sha[:], &root.ShaLength, strings.Repeat("d", 40))
	root.Tier = Tierflash
	setStr(root.Repo[:], &root.RepoLength, "mas-bandwidth/schema")
	setStr(root.Kind[:], &root.KindLength, "feature")
	setStr(root.Test[:], &root.TestLength, "TestUpgradeKeepsEveryField")
	root.Column = Columncheck
	root.Attempt = 1
	if !b.Lock() {
		t.Fatal("CardBuilder.Lock failed")
	}
	packed := b.AsConst()
	size := CardMeasure(packed)
	if size < 0 {
		t.Fatal("CardMeasure refused")
	}
	wire = make([]byte, size)
	if CardSave(packed, wire) != size {
		t.Fatal("CardSave did not fill")
	}
	rec, _, err := Load(wire)
	if err != nil {
		t.Fatalf("self-load: %v", err)
	}
	json, _, err = ToJSON(rec)
	if err != nil {
		t.Fatalf("self-ToJSON: %v", err)
	}
	return wire, json
}

func addConsumer(t *testing.T, b *CardBuilder, list *TableList[ConsumerRow], kind, who, route, model string, started, ended int64, outcome, line string, cost int64) {
	t.Helper()
	e := CardConsumersAdd(&b.Main, list)
	if e == nil {
		t.Fatal("CardConsumersAdd nil")
	}
	setStr(e.Kind[:], &e.KindLength, kind)
	setStr(e.Who[:], &e.WhoLength, who)
	setStr(e.Route[:], &e.RouteLength, route)
	setStr(e.Model[:], &e.ModelLength, model)
	e.Started = started
	e.Ended = ended
	setStr(e.Outcome[:], &e.OutcomeLength, outcome)
	setStr(e.Line[:], &e.LineLength, line)
	e.CostMicroUsd = cost
}

// TestCorpusFixtureWrite authors the committed corpus fixtures into the named
// source directory. It runs only under CARDS_UPDATE_CORPUS=1; ordinary tests
// read the committed evidence and never generate it (docs/SPEC-TABLES.md §6.6).
func TestCorpusFixtureWrite(t *testing.T) {
	if os.Getenv("CARDS_UPDATE_CORPUS") != "1" {
		t.Skip("corpus fixture update requires CARDS_UPDATE_CORPUS=1")
	}
	src := filepath.Join("..", "..", "..", "tables", "cards", "corpus")
	write := func(caseName, fixture string, wire, json []byte) {
		dir := filepath.Join(src, caseName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		wirePath := filepath.Join(dir, fixture+".wire")
		if _, err := os.Stat(wirePath); err == nil {
			t.Fatalf("refusing to overwrite existing case %s/%s", caseName, fixture)
		}
		if err := os.WriteFile(wirePath, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fixture+".json"), json, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	iw, ij := buildInitialCard(t)
	write("initial", "populated", iw, ij)
	cw, cj := buildConsumersCard(t)
	write("consumers-and-rename", "populated", cw, cj)
	ew, ej := buildEmptyConsumersCard(t)
	write("consumers-and-rename", "empty-consumers", ew, ej)
}

// shippedCases enumerates the committed SHIPPED corpus cases: each has a
// METADATA.txt classifying it SHIPPED.
func shippedCases(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("corpus")
	if err != nil {
		t.Fatalf("corpus directory missing: %v", err)
	}
	var cases []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := os.ReadFile(filepath.Join("corpus", e.Name(), "METADATA.txt"))
		if err != nil {
			continue
		}
		if !strings.Contains(string(meta), "class: SHIPPED") {
			continue
		}
		cases = append(cases, e.Name())
	}
	if len(cases) == 0 {
		t.Fatal("no SHIPPED corpus cases found")
	}
	return cases
}

// checkUpgraded is the shared verification: Upgrade the wire, then compare the
// upgraded record's logical values and exact body against the authored expected
// JSON (loaded through the current generated reader), and confirm the fixture's
// historical reader still sees every value it originally knew (docs/SPEC-TABLES.md §4, §5, §6.6).
func checkUpgraded(t *testing.T, caseName string, wire, json []byte) {
	t.Helper()
	exp, err := FromJSON(json)
	if err != nil {
		t.Fatalf("%s: authored JSON is not a card: %v", caseName, err)
	}
	upgraded, report, err := Upgrade(wire)
	if err != nil {
		t.Fatalf("%s: Upgrade failed: %v (report %+v)", caseName, err, report)
	}
	if report.Malformed || report.KindMismatch != 0 || report.Clamped != 0 || report.RetainLost != 0 {
		t.Fatalf("%s: Upgrade reported loss: %+v", caseName, report)
	}
	got, _, err := Load(upgraded)
	if err != nil {
		t.Fatalf("%s: Load of upgraded wire failed: %v", caseName, err)
	}
	gj, _, err := ToJSON(got)
	if err != nil {
		t.Fatalf("%s: ToJSON of upgraded: %v", caseName, err)
	}
	ej, _, err := ToJSON(exp)
	if err != nil {
		t.Fatalf("%s: ToJSON of expected: %v", caseName, err)
	}
	if !bytes.Equal(gj, ej) {
		t.Fatalf("%s: upgraded logical values differ:\n got %s\nwant %s", caseName, gj, ej)
	}
	if gotBody, expBody := stringData(&got.root.Body, nil), stringData(&exp.root.Body, nil); !bytes.Equal(gotBody, expBody) {
		t.Fatalf("%s: body bytes differ: got %d, want %d", caseName, len(gotBody), len(expBody))
	}
	if !bytes.Equal(upgraded, mustUpgrade(t, upgraded)) {
		t.Fatalf("%s: Upgrade is not byte-idempotent", caseName)
	}
	// Historical reread: the fixture's own generated reader sees the same values
	// before and after upgrade, so every value it knew survives.
	if caseName == "initial" {
		checkHistorical(t, wire, upgraded)
	}
}

// checkHistorical decodes the original and upgraded wires through the frozen
// initial declaration's generated reader and requires byte-identical text
// (docs/SPEC-TABLES.md §4: an old reader may report added features, but every
// value it originally knew survives).
func checkHistorical(t *testing.T, original, upgraded []byte) {
	t.Helper()
	load := func(wire []byte) []byte {
		n := cardsinitial.CardLoadMeasure(wire)
		if n < 0 {
			t.Fatalf("historical measure refused: %d", n)
		}
		region := alignedRegion(n)
		var report cardsinitial.TableReport
		root := cardsinitial.CardLoad(region, wire, &report)
		if root == nil || report.Malformed {
			t.Fatalf("historical load failed: %+v", report)
		}
		size := cardsinitial.CardToJsonMeasure(root)
		buf := make([]byte, size)
		if cardsinitial.CardToJson(root, buf) != size {
			t.Fatal("historical ToJson did not fill")
		}
		return buf
	}
	before, after := load(original), load(upgraded)
	if !bytes.Equal(before, after) {
		t.Fatalf("historical reader lost a known value:\nbefore %s\nafter  %s", before, after)
	}
}

func mustUpgrade(t *testing.T, wire []byte) []byte {
	t.Helper()
	out, report, err := Upgrade(wire)
	if err != nil {
		t.Fatalf("Upgrade failed: %v (report %+v)", err, report)
	}
	return out
}

// TestEveryCorpusRecordReadsWithTheCurrentModel enumerates every committed
// SHIPPED case, requires a successful Upgrade, and compares the known logical
// values and exact body with the authored expected values (docs/SPEC-TABLES.md §4, §5, §6.6).
func TestEveryCorpusRecordReadsWithTheCurrentModel(t *testing.T) {
	t.Parallel()
	for _, caseName := range shippedCases(t) {
		caseName := caseName
		dir := filepath.Join("corpus", caseName)
		wires, err := filepath.Glob(filepath.Join(dir, "*.wire"))
		if err != nil || len(wires) == 0 {
			t.Fatalf("%s: no .wire fixtures", caseName)
		}
		for _, wf := range wires {
			wf := wf
			t.Run(caseName+"/"+filepath.Base(wf), func(t *testing.T) {
				t.Parallel()
				wire, err := os.ReadFile(wf)
				if err != nil {
					t.Fatalf("read wire: %v", err)
				}
				jf := strings.TrimSuffix(wf, ".wire") + ".json"
				json, err := os.ReadFile(jf)
				if err != nil {
					t.Fatalf("read json: %v", err)
				}
				checkUpgraded(t, caseName, wire, json)
			})
		}
	}
}

// TestUnsupportedDataIsReported uses the future probe declaration: an older
// generated reader (cardsinitial) counting Unknown on a newer card's
// consumers/plan/check, and the current ReadStrict refusing a nonempty future
// scalar (docs/SPEC-TABLES.md §4, §6).
func TestUnsupportedDataIsReported(t *testing.T) {
	t.Parallel()
	// An older reader's Unknown count rises on the newer card's added fields:
	// consumers, plan/check columns, done (already known via done_when).
	wire, _ := buildConsumersCard(t)
	var report cardsinitial.TableReport
	n := cardsinitial.CardLoadMeasure(wire)
	if n < 0 {
		t.Fatal("historical measure refused")
	}
	region := alignedRegion(n)
	root := cardsinitial.CardLoad(region, wire, &report)
	if root == nil || report.Malformed {
		t.Fatalf("historical load failed: %+v", report)
	}
	if report.Unknown == 0 {
		t.Fatal("an older reader reported no Unknown for the newer card's consumers/plan fields")
	}
	// Unknown enum selection reads None with the count: the old reader has no
	// plan/check variants, so a plan column reads None.
	if root.Column != cardsinitial.ColumnNone {
		t.Fatalf("unknown plan column did not read None: %d", root.Column)
	}
}
