package tabledemo_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestGeneratedFixedOneVerdictEveryByte substitutes every value of every byte
// past the header of each fixed root's record, one at a time, and holds the
// generated fixed read to the engine's verdict on each (docs/SPEC-TABLES.md
// §4, §6.5; the slack rule of §3.4 included).
func TestGeneratedFixedOneVerdictEveryByte(t *testing.T) {
	t.Parallel()
	c := compiler.New()
	paths, _ := compiler.GatherPaths([]string{"../../../../tables/examples"})
	u, _ := c.Load(paths)
	m := tabletext.NewModel(u)
	for _, target := range fixedTargets {
		root := m.Lookup(target.name)
		if root == nil || !tablewire.FixedRoot(m, root) {
			continue
		}
		v := target.zero()
		one := target.slice(target.pair(v, v), 1)
		image := make([]byte, target.measure(1))
		target.save(one, image)
		type row struct {
			at         int
			vals       []string
			reason, js string
		}
		rows := map[string]*row{}
		for at := 16; at < len(image); at++ {
			for b := 0; b < 256; b++ {
				if byte(b) == image[at] {
					continue
				}
				in := append([]byte(nil), image...)
				in[at] = byte(b)
				vals := target.blank(1)
				var rep tabledemo.TableReport
				gok := target.load(vals, append([]byte(nil), in...), target.plan(), &rep) == 1
				inst := m.New(root)
				var erep tabletext.Report
				eok, eerr := tablewire.Decode(m, inst, in, &erep)
				if gok == eok {
					continue
				}
				js, _ := m.Write(inst)
				reason := fmt.Sprintf("gen=%v(%s) engine=%v(%v)", gok, rep.Reason, eok, eerr)
				key := fmt.Sprintf("@%d %s", at, reason)
				if rows[key] == nil {
					rows[key] = &row{at: at, reason: reason, js: strings.Join(strings.Fields(string(js)), "")}
				}
				rows[key].vals = append(rows[key].vals, fmt.Sprintf("%02x", b))
			}
		}
		keys := make([]string, 0, len(rows))
		for k := range rows {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return rows[keys[i]].at < rows[keys[j]].at })
		for i, k := range keys {
			if i > 12 {
				t.Errorf("%s: ... %d more", target.name, len(keys)-i)
				break
			}
			r := rows[k]
			vs := r.vals
			if len(vs) > 6 {
				vs = append(vs[:6], fmt.Sprintf("..(%d)", len(r.vals)))
			}
			t.Errorf("%s @%d byte(orig %02x)->%v: the verdicts differ, %s; value=%s", target.name, r.at, image[r.at], vs, r.reason, r.js)
		}
	}
}
