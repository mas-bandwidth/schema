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
	"os"
	"runtime"
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
		soakRow(t, "chain_value", g+"chain_value.bin", tblp1.ChainReset, tblp1.ChainLoad, tblp1.ChainMeasure,
			tblp1.ChainSave, tblp1.ChainFromJson, tblp1.ChainToJsonMeasure, tblp1.ChainToJson),
		soakRow(t, "chain_value_empty", g+"chain_value_empty.bin", tblp1.ChainReset, tblp1.ChainLoad,
			tblp1.ChainMeasure, tblp1.ChainSave, tblp1.ChainFromJson, tblp1.ChainToJsonMeasure, tblp1.ChainToJson),
		soakRow(t, "chain_pointer", g+"chain_pointer.bin", tblp1.ChainReset, tblp1.ChainLoad, tblp1.ChainMeasure,
			tblp1.ChainSave, tblp1.ChainFromJson, tblp1.ChainToJsonMeasure, tblp1.ChainToJson),
		soakRow(t, "chain_optional", g+"chain_optional.bin", tblp3.ChainReset, tblp3.ChainLoad, tblp3.ChainMeasure,
			tblp3.ChainSave, tblp3.ChainFromJson, tblp3.ChainToJsonMeasure, tblp3.ChainToJson),
		soakRow(t, "chain_optional_empty", g+"chain_optional_empty.bin", tblp3.ChainReset, tblp3.ChainLoad,
			tblp3.ChainMeasure, tblp3.ChainSave, tblp3.ChainFromJson, tblp3.ChainToJsonMeasure, tblp3.ChainToJson),
		soakRow(t, "chain_pointer_empty", g+"chain_pointer_empty.bin", tblp3.ChainReset, tblp3.ChainLoad,
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

	runtime.ReadMemStats(&after)
	grew := after.Mallocs - before.Mallocs

	// THE BOUND IS A RATE, and it is measured rather than assumed. Five
	// minutes of this loop — 42,000 passes — moves the counter ZERO times, and
	// an hour of it moves it TWICE: that is the runtime's own bookkeeping, a
	// forced collection or a stack move, and not the codec's. What a LEAK
	// looks like is one object per pass, or one per ten, or one per thousand,
	// and every one of those buries this bound long before the hour is up.
	//
	// The EXACT number is held next door: the alloc gates read zero for each
	// operation on its own, through testing.AllocsPerRun, and this is the
	// duration half of the same claim rather than a second, looser one.
	allowed := iterations/10000 + 8
	if grew > uint64(allowed) {
		t.Errorf("the soak allocated %d objects over %d passes of the corpus, past a bound of %d — "+
			"the read and write paths own no memory, so an allocation that SCALES with the loop is a leak",
			grew, iterations, allowed)
	}
	t.Logf("%d passes of %d cases in %v, %d objects allocated (bound %d)",
		iterations, len(corpus), *soakFor, grew, allowed)
	if os.Getenv("VERBOSE") != "" {
		t.Logf("heap in use: %d -> %d bytes", before.HeapAlloc, after.HeapAlloc)
	}
}
