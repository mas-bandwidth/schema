package tabledemo_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestGeneratedFixedAgreesWithEngine is the generated fixed read held to the
// engine's over every fixed root of the examples unit and 6000 mutants of two
// records each: one verdict on every input (docs/SPEC-TABLES.md §4, §6.5),
// slack included (§3.4, THE SLACK RULE: bytes past a count and under an absent
// optional are never a refusal); and where both accept, the same value, so the
// generated save and the engine's write the same bytes, and the generated text
// reads back in the engine to that value.
func TestGeneratedFixedAgreesWithEngine(t *testing.T) {
	t.Parallel()
	c := compiler.New()
	paths, _ := compiler.GatherPaths([]string{"../../../../tables/examples"})
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	var mu sync.Mutex
	hist := map[string]int{}
	ex := map[string][]string{}
	note := func(k, s string) {
		mu.Lock()
		defer mu.Unlock()
		hist[k]++
		if len(ex[k]) < 3 {
			ex[k] = append(ex[k], s)
		}
	}
	var wg sync.WaitGroup
	for ti, target := range fixedTargets {
		root := m.Lookup(target.name)
		if root == nil || !tablewire.FixedRoot(m, root) {
			t.Logf("%s: not an engine fixed root", target.name)
			continue
		}
		for _, v := range []any{target.zero(), target.away()} {
			one := target.pair(v, v)
			one = target.slice(one, 1)
			size := target.measure(1)
			image := make([]byte, size)
			if n := target.save(one, image); n != size {
				t.Fatalf("%s: save %d of %d", target.name, n, size)
			}
			wg.Add(1)
			go func(ti int, image []byte) {
				defer wg.Done()
				rng := rand.New(rand.NewSource(int64(ti + 99)))
				for i := range 6000 {
					b := image
					if i > 0 {
						b = mutate(rng, image)
					}
					vals := target.blank(1)
					var rep tabledemo.TableReport
					n := target.load(vals, append([]byte(nil), b...), target.plan(), &rep)
					gok := n == 1
					inst := m.New(root)
					var erep tabletext.Report
					eok, eerr := tablewire.Decode(m, inst, b, &erep)
					if gok != eok {
						note(fmt.Sprintf("%s verdict gen=%v(n=%d %s) engine=%v", target.name, gok, n, rep.Reason, eok), fmt.Sprintf("in=%s engine-err=%v", hex.EncodeToString(b), eerr))
						continue
					}
					if !gok {
						continue
					}
					ew, err := tablewire.EncodeFixed(m, inst)
					if err != nil {
						note(target.name+" engine-encode-fails", err.Error())
						continue
					}
					gw := make([]byte, target.measure(1))
					target.save(vals, gw)
					if !bytes.Equal(gw, ew) {
						note(target.name+" resave-differs", fmt.Sprintf("in=%s\n gen=%s\n eng=%s", hex.EncodeToString(b), hex.EncodeToString(gw), hex.EncodeToString(ew)))
					}
					texts, ok := target.json(vals)
					_, jerr := m.Write(inst)
					if !ok || jerr != nil {
						if ok != (jerr == nil) {
							note(fmt.Sprintf("%s json-write gen-ok=%v engine-err=%v", target.name, ok, jerr), hex.EncodeToString(b))
						}
						continue
					}
					i2 := m.New(root)
					var r2 tabletext.Report
					if !m.Read(i2, texts[0], &r2) {
						note(target.name+" engine-refuses-gen-json", fmt.Sprintf("js=%s %s", texts[0], r2.Reason))
						continue
					}
					w2, _ := tablewire.EncodeFixed(m, i2)
					if !bytes.Equal(w2, ew) {
						note(target.name+" gen-json-value-differs", fmt.Sprintf("in=%s js=%s", hex.EncodeToString(b), texts[0]))
					}
				}
			}(ti, image)
		}
	}
	wg.Wait()
	keys := make([]string, 0)
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Errorf("%s x%d", k, hist[k])
		for _, s := range ex[k] {
			t.Logf("    %s", s)
		}
	}
}

// TestGeneratedFixedSlackNegativeControl makes the generated pre-scan read the
// slack (every slack span dropped) and requires the differential red on a
// verdict the engine does not share.
func TestGeneratedFixedSlackNegativeControl(t *testing.T) {
	t.Parallel()
	out, failed := slowtest.RunSabotaged(t, "../../../..", "./internal/codegen/gotable/fuzzunit", "^TestGeneratedFixedAgreesWithEngine$",
		slowtest.Sabotage{File: "tables/goruntime/goruntime.go",
			From: "\tfor _, d := range dead {", To: "\tfor _, d := range dead[:0] {"})
	if !failed || !strings.Contains(out, "verdict gen=false") {
		t.Errorf("negative control: the generated read refuses slack and the differential did not go red on a verdict\n%s", out)
	}
}

// mutate returns a copy of image with one to three bytes past the five-byte
// file header rewritten: a bit flip, a boundary byte or a random byte, so the
// mutants reach the bool, present, count, length, tag and ordinal bytes and
// the slack behind them.
func mutate(rng *rand.Rand, image []byte) []byte {
	out := append([]byte(nil), image...)
	if len(out) <= 5 {
		return out
	}
	boundary := []byte{0, 1, 2, 0x7f, 0x80, 0xfe, 0xff}
	for n := 1 + rng.Intn(3); n > 0; n-- {
		at := 5 + rng.Intn(len(out)-5)
		switch rng.Intn(3) {
		case 0:
			out[at] ^= 1 << uint(rng.Intn(8))
		case 1:
			out[at] = boundary[rng.Intn(len(boundary))]
		default:
			out[at] = byte(rng.Intn(256))
		}
	}
	return out
}
