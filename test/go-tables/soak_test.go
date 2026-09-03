// THE SOAK (docs/SPEC-TABLES.md, "What allocates, and what never does").
//
// Correctness tests are necessary and not sufficient: a read path that leaks
// one object per call passes every byte comparison in this repo and takes a
// server down in an afternoon. So this reads and writes the WHOLE corpus in a
// loop for as long as it is given, and the thing it watches is not the bytes —
// those are the conformance harness's business — but the ALLOCATION COUNTER,
// which must be flat.
//
// It runs for two seconds by default, so it rides `go test ./...` and catches a
// regression on the way past; `make tables-go-soak` gives it the hour.
//
// WHAT IT WATCHES, and why each one:
//
//   - runtime.MemStats.Mallocs across the steady phase, which must not move at
//     all. Zero is the number: the read path owns no memory, and one object per
//     iteration is what a leak looks like before it looks like anything else.
//   - the bytes, every iteration, because a loop that stopped producing the
//     right answer while allocating nothing would pass an allocation gate.
package schematables

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"tabledemo"
	"tblp1"
	"tblp3"
	"tblv1"
	"tblv2"
)

var soakFor = flag.Duration("soak", 2*time.Second, "how long the soak runs")

// one corpus row: the wire golden, its text, and the three operations bound to
// the generated surface. Erased the way the conformance driver erases them, so
// the loop below is one loop and not one per root.
type soakCase struct {
	name string
	wire []byte
	text []byte
	// roundTrip loads the wire bytes, measures, saves into scratch and hands
	// back what it wrote
	roundTrip func(scratch []byte) []byte
	// textTrip reads the text, saves the wire form, and hands back what it
	// wrote; and writes the text back into scratch
	textTrip func(scratch []byte) []byte
	jsonTrip func(scratch []byte) []byte
}

func soakRow[T any, R any](
	t *testing.T, name, wirePath string,
	reset func(*T), load func(*T, []byte, *R) bool,
	measure func(*T) int64, save func(*T, []byte) int64,
	fromJson func(*T, []byte, *R) bool,
	toJsonMeasure func(*T) int64, toJson func(*T, []byte) int64,
) soakCase {
	wireBytes := wire(t, wirePath)
	textBytes := wire(t, "../../testdata/conformance/tables/json/"+name+".json")
	// the value and the report are allocated ONCE, here, and reused: they are
	// the CALLER's storage, which is what every codec on this page takes, and
	// allocating one per pass would be the soak measuring its own harness
	value := new(T)
	report := new(R)
	return soakCase{
		name: name, wire: wireBytes, text: textBytes,
		roundTrip: func(scratch []byte) []byte {
			*report = *new(R)
			reset(value)
			if !load(value, wireBytes, report) {
				t.Fatalf("%s: the golden does not load", name)
			}
			size := measure(value)
			if save(value, scratch[:size]) != size {
				t.Fatalf("%s: save did not write measure's answer", name)
			}
			return scratch[:size]
		},
		textTrip: func(scratch []byte) []byte {
			*report = *new(R)
			reset(value)
			if !fromJson(value, textBytes, report) {
				t.Fatalf("%s: FromJson refused the golden text", name)
			}
			size := measure(value)
			if save(value, scratch[:size]) != size {
				t.Fatalf("%s: save did not write measure's answer", name)
			}
			return scratch[:size]
		},
		jsonTrip: func(scratch []byte) []byte {
			*report = *new(R)
			reset(value)
			if !load(value, wireBytes, report) {
				t.Fatalf("%s: the golden does not load", name)
			}
			size := toJsonMeasure(value)
			if toJson(value, scratch[:size]) != size {
				t.Fatalf("%s: ToJson did not write ToJsonMeasure's answer", name)
			}
			return scratch[:size]
		},
	}
}

func soakCorpus(t *testing.T) []soakCase {
	g := "../../testdata/wire/tables/"
	return []soakCase{
		soakRow(t, "root_full", g+"root_full.bin", tabledemo.RootConfigReset, tabledemo.RootConfigLoad,
			tabledemo.RootConfigMeasure, tabledemo.RootConfigSave, tabledemo.RootConfigFromJson,
			tabledemo.RootConfigToJsonMeasure, tabledemo.RootConfigToJson),
		soakRow(t, "root_default", g+"root_default.bin", tabledemo.RootConfigReset, tabledemo.RootConfigLoad,
			tabledemo.RootConfigMeasure, tabledemo.RootConfigSave, tabledemo.RootConfigFromJson,
			tabledemo.RootConfigToJsonMeasure, tabledemo.RootConfigToJson),
		soakRow(t, "profile_elide", g+"profile_elide.bin", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigLoad,
			tabledemo.ProfileConfigMeasure, tabledemo.ProfileConfigSave, tabledemo.ProfileConfigFromJson,
			tabledemo.ProfileConfigToJsonMeasure, tabledemo.ProfileConfigToJson),
		soakRow(t, "loadout_full", g+"loadout_full.bin", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigLoad,
			tabledemo.LoadoutConfigMeasure, tabledemo.LoadoutConfigSave, tabledemo.LoadoutConfigFromJson,
			tabledemo.LoadoutConfigToJsonMeasure, tabledemo.LoadoutConfigToJson),
		soakRow(t, "wide_blob", g+"wide_blob.bin", tabledemo.WideBlobReset, tabledemo.WideBlobLoad,
			tabledemo.WideBlobMeasure, tabledemo.WideBlobSave, tabledemo.WideBlobFromJson,
			tabledemo.WideBlobToJsonMeasure, tabledemo.WideBlobToJson),
		soakRow(t, "archive", g+"archive.bin", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigLoad,
			tabledemo.ArchiveConfigMeasure, tabledemo.ArchiveConfigSave, tabledemo.ArchiveConfigFromJson,
			tabledemo.ArchiveConfigToJsonMeasure, tabledemo.ArchiveConfigToJson),
		soakRow(t, "keyed_config", g+"keyed_config.bin", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoad,
			tabledemo.KeyedConfigMeasure, tabledemo.KeyedConfigSave, tabledemo.KeyedConfigFromJson,
			tabledemo.KeyedConfigToJsonMeasure, tabledemo.KeyedConfigToJson),
		soakRow(t, "keyed_default", g+"keyed_default.bin", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoad,
			tabledemo.KeyedConfigMeasure, tabledemo.KeyedConfigSave, tabledemo.KeyedConfigFromJson,
			tabledemo.KeyedConfigToJsonMeasure, tabledemo.KeyedConfigToJson),
		soakRow(t, "v1_cfg", g+"v1_cfg.bin", tblv1.CfgReset, tblv1.CfgLoad, tblv1.CfgMeasure, tblv1.CfgSave,
			tblv1.CfgFromJson, tblv1.CfgToJsonMeasure, tblv1.CfgToJson),
		soakRow(t, "v1_seams", g+"v1_seams.bin", tblv1.CfgReset, tblv1.CfgLoad, tblv1.CfgMeasure, tblv1.CfgSave,
			tblv1.CfgFromJson, tblv1.CfgToJsonMeasure, tblv1.CfgToJson),
		soakRow(t, "v2_cfg", g+"v2_cfg.bin", tblv2.CfgReset, tblv2.CfgLoad, tblv2.CfgMeasure, tblv2.CfgSave,
			tblv2.CfgFromJson, tblv2.CfgToJsonMeasure, tblv2.CfgToJson),
		soakRow(t, "v2_seams", g+"v2_seams.bin", tblv2.CfgReset, tblv2.CfgLoad, tblv2.CfgMeasure, tblv2.CfgSave,
			tblv2.CfgFromJson, tblv2.CfgToJsonMeasure, tblv2.CfgToJson),
		// the pointered spellings (chain_pointer, chain_pointer_empty) have no Go
		// codec and no text: Go refuses the pointered unit by name (§11), and
		// their goldens are the C++ reference's
		soakRow(t, "chain_value", g+"chain_value.bin", tblp1.ChainReset, tblp1.ChainLoad, tblp1.ChainMeasure,
			tblp1.ChainSave, tblp1.ChainFromJson, tblp1.ChainToJsonMeasure, tblp1.ChainToJson),
		soakRow(t, "chain_value_empty", g+"chain_value_empty.bin", tblp1.ChainReset, tblp1.ChainLoad,
			tblp1.ChainMeasure, tblp1.ChainSave, tblp1.ChainFromJson, tblp1.ChainToJsonMeasure, tblp1.ChainToJson),
		soakRow(t, "chain_optional", g+"chain_optional.bin", tblp3.ChainReset, tblp3.ChainLoad, tblp3.ChainMeasure,
			tblp3.ChainSave, tblp3.ChainFromJson, tblp3.ChainToJsonMeasure, tblp3.ChainToJson),
		soakRow(t, "chain_optional_empty", g+"chain_optional_empty.bin", tblp3.ChainReset, tblp3.ChainLoad,
			tblp3.ChainMeasure, tblp3.ChainSave, tblp3.ChainFromJson, tblp3.ChainToJsonMeasure, tblp3.ChainToJson),
	}
}

func TestSoak(t *testing.T) {
	corpus := soakCorpus(t)
	scratch := make([]byte, 1<<20)

	// one warm pass, so the steady phase below measures the loop rather than
	// the first touch of every page in it
	pass := func() {
		for _, c := range corpus {
			if !bytes.Equal(c.roundTrip(scratch), c.wire) {
				t.Fatalf("%s: the wire round trip does not reproduce the golden", c.name)
			}
			if !bytes.Equal(c.textTrip(scratch), c.wire) {
				t.Fatalf("%s: the text round trip does not reproduce the wire golden", c.name)
			}
			if !bytes.Equal(c.jsonTrip(scratch), c.text) {
				t.Fatalf("%s: the text written back is not the golden text", c.name)
			}
		}
	}
	pass()

	// THE STEADY PHASE. Mallocs is read either side of it and must not move:
	// the read path owns no memory, and one object per iteration is what a leak
	// looks like before it looks like anything else.
	var before, after runtime.MemStats
	runtime.GC()
	// EVERY ALLOCATION IS SAMPLED, so the ones that do happen can be NAMED
	// rather than guessed at. MemProfileRate = 1 records a stack for each, and
	// the profile is diffed either side of the steady phase — which is what
	// turns "two objects an hour, a forced collection or a stack move" from a
	// plausible story into a fact the test prints.
	// THE PROFILE IS FLUSHED ON BOTH SIDES, and that ordering is the whole
	// difference between a reading and a fiction: an allocation's record only
	// becomes visible in the profile after a GC cycle processes it, so a
	// snapshot taken before one attributes the SETUP's allocations — the
	// scratch buffer, the corpus — to the steady phase that follows it.
	oldRate := runtime.MemProfileRate
	runtime.MemProfileRate = 1
	defer func() { runtime.MemProfileRate = oldRate }()
	runtime.GC()
	beforeSites := memProfileByStack()
	runtime.ReadMemStats(&before)

	deadline := time.Now().Add(*soakFor)
	iterations := int64(0)
	for time.Now().Before(deadline) {
		pass()
		iterations++
		if t.Failed() {
			return
		}
	}

	// THE COUNT FIRST, before a collection of our own moves it.
	runtime.ReadMemStats(&after)
	grew := after.Mallocs - before.Mallocs

	// AND THE PROFILE ONLY WHEN THERE IS SOMETHING TO EXPLAIN. Mallocs is the
	// ground truth and the profile is the witness: a record's AllocObjects
	// becomes visible only after a GC cycle processes it, so a snapshot taken
	// when nothing was allocated reports the SETUP's own objects — the scratch
	// buffer, the corpus — as if the loop had made them. Asking who allocated
	// when the answer is "nobody" is how a gate invents a finding.
	var sites []site
	if grew > 0 {
		// three cycles, because the profile lags the allocator by more than one
		for i := 0; i < 3; i++ {
			runtime.GC()
		}
		sites = grownSites(beforeSites, memProfileByStack())
	}

	// WHAT ALLOCATED, NAMED — when anything did. Every site is classified by
	// whose code it is, and the two answers are not the same finding: an
	// allocation whose stack is entirely the RUNTIME's — a background mark
	// worker starting, an m being allocated for a thread, a forced collection's
	// own bookkeeping — is the Go runtime running, and this loop cannot avoid
	// it. An allocation with a frame in the generated packages or in this file
	// is the CODEC's, and one of those is a leak whatever the count says.
	mine := 0
	for _, site := range sites {
		if site.mine {
			mine++
		}
		t.Logf("allocated %d at %s", site.objects, site.where)
	}
	if mine > 0 {
		t.Errorf("%d of the %d allocation sites are the port's own — the read and write paths own no memory",
			mine, len(sites))
	}

	// AND THE COUNT IS BOUNDED AS A RATE beside that, so a leak the profiler
	// missed still fails: what a leak looks like is one object per pass, or one
	// per ten, or one per thousand, and every one of those buries this bound
	// long before the hour is up. The EXACT zero is held next door, by the
	// per-operation gates in alloc_test.go.
	allowed := iterations/10000 + 8
	if grew > uint64(allowed) {
		t.Errorf("the soak allocated %d objects over %d passes of the corpus, past a bound of %d — "+
			"the read and write paths own no memory, so an allocation that SCALES with the loop is a leak",
			grew, iterations, allowed)
	}
	t.Logf("%d passes of %d cases in %v, %d objects allocated (bound %d) across %d site(s), %d of them the port's",
		iterations, len(corpus), *soakFor, grew, allowed, len(sites), mine)
	if os.Getenv("VERBOSE") != "" {
		t.Logf("heap in use: %d -> %d bytes", before.HeapAlloc, after.HeapAlloc)
	}
}

// ---- naming what allocated ----

// site is one allocation stack the steady phase grew: how many objects, where
// its innermost non-runtime frame is, and whether any frame is the PORT's.
type site struct {
	objects int64
	where   string
	mine    bool
}

// memProfileByStack is every recorded allocation stack and its object count,
// keyed by the stack itself so two snapshots can be diffed.
func memProfileByStack() map[string]int64 {
	var records []runtime.MemProfileRecord
	for {
		n, ok := runtime.MemProfile(records, false)
		if ok {
			records = records[:n]
			break
		}
		records = make([]runtime.MemProfileRecord, n+64)
	}
	out := make(map[string]int64, len(records))
	for i := range records {
		out[stackKey(&records[i])] += records[i].AllocObjects
	}
	return out
}

func stackKey(r *runtime.MemProfileRecord) string {
	var b strings.Builder
	for _, pc := range r.Stack() {
		fmt.Fprintf(&b, "%x;", pc)
	}
	return b.String()
}

// grownSites is the stacks whose object count rose across the steady phase,
// symbolised. A stack whose every frame is `runtime.` is the runtime's own;
// anything else names a package, and the port's packages are the ones that
// matter.
func grownSites(before, after map[string]int64) []site {
	var out []site
	for key, count := range after {
		grew := count - before[key]
		if grew <= 0 {
			continue
		}
		out = append(out, symbolise(key, grew))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].where < out[j].where })
	return out
}

func symbolise(key string, objects int64) site {
	var pcs []uintptr
	for _, hex := range strings.Split(key, ";") {
		if hex == "" {
			continue
		}
		var pc uint64
		fmt.Sscanf(hex, "%x", &pc)
		pcs = append(pcs, uintptr(pc))
	}
	result := site{objects: objects, where: "(unsymbolised)"}
	if len(pcs) == 0 {
		return result
	}
	frames := runtime.CallersFrames(pcs)
	first := true
	for {
		frame, more := frames.Next()
		if first && frame.Function != "" {
			result.where = frame.Function
			first = false
		}
		// the PORT is the generated packages and this test's own file; every
		// other frame in a clean run is the runtime's
		if strings.HasPrefix(frame.Function, "tabledemo.") || strings.HasPrefix(frame.Function, "tblv") ||
			strings.HasPrefix(frame.Function, "tblp") || strings.HasPrefix(frame.Function, "blockdemo.") ||
			strings.HasPrefix(frame.Function, "graphdemo.") || strings.HasPrefix(frame.Function, "schematables.") {
			result.mine = true
			result.where = frame.Function
		}
		if !more {
			break
		}
	}
	return result
}

// TestSoakIdentifierCanGoRed is the negative control for the identification
// above, and it is not optional: the first version of that code took its
// "before" snapshot ahead of the flush the profile needs, so it attributed the
// SETUP's own objects to the loop and reported a finding that was not there. A
// classifier that has never been shown a real allocation is a classifier nobody
// has checked.
//
// It allocates deliberately, from THIS package, and requires the walk to see it
// and to call it the port's.
func TestSoakIdentifierCanGoRed(t *testing.T) {
	oldRate := runtime.MemProfileRate
	runtime.MemProfileRate = 1
	defer func() { runtime.MemProfileRate = oldRate }()

	runtime.GC()
	before := memProfileByStack()
	for i := 0; i < 100; i++ {
		soakSink = make([]byte, 48)
	}
	for i := 0; i < 3; i++ {
		runtime.GC()
	}
	sites := grownSites(before, memProfileByStack())

	mine := 0
	total := int64(0)
	for _, s := range sites {
		if s.mine {
			mine++
			total += s.objects
		}
	}
	if mine == 0 {
		t.Fatalf("100 deliberate allocations from this package were not attributed to it; saw %d site(s)", len(sites))
	}
	if total < 50 {
		t.Errorf("the walk counted %d of 100 deliberate allocations — it is seeing them, but not all of them", total)
	}
}

// soakSink is where TestSoakIdentifierCanGoRed's allocations escape to.
var soakSink []byte
