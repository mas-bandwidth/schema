// `schema cook`, `schema cook-check` and `schema uncook` on the public driver
// (docs/SPEC-TABLES.md §7).
//
// COOKING IS FUNDAMENTALLY AN OPTIMIZATION: the wire stays the format of record
// and a cook is a build-locked accelerator beside it, produced only where load
// time asks for one. TOOLING BUILDS; THE GAME POINTS.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// CookOptions selects what a cook is produced for.
type CookOptions struct {
	// Big produces a BIG-ENDIAN cook. A cook is produced in the byte order of
	// the build it is cooked for, so the fixing happens where the target is
	// known — offline, once, on the writing side — and never on the reading
	// side, which is what makes `Open` a match and a point (docs/SPEC-TABLES.md §7).
	Big bool
	// NoAttribution leaves the node directory out, so a build that ships no
	// tooling carries just data. `schema cook-check` then refuses the file and
	// says which part is missing.
	NoAttribution bool
}

// CookReport is what a cook says about itself when it is written: the header
// facts a person needs to key a store by, and the shape of the region.
type CookReport struct {
	BuildVersion uint64
	ByteOrder    string
	Root         string
	Nodes        int
	DataBytes    int64
	AttribBytes  int64
	Pointers     int
}

// Cook converts one root table instance ON THE TOLERANT WIRE into the cooked
// form: the header, then `Lock`'s region written verbatim with the root at its
// base, then the node directory of §6.3 written BESIDE the data for the tool.
//
// The wire is the FORMAT OF RECORD and a cook is produced beside one, which is
// what makes the content-addressing pair well defined: the tuple a store is
// indexed by is (the hash of the WIRE FILE these bytes were produced from, this
// unit's BUILD VERSION), and the build version is the number in the returned
// report.
//
// **A COOK REFUSES A PARTIAL REGION.** A wire whose node table this unit cannot
// read whole would load into a region with a hole in it — a not-materialized
// sentinel — and a cooked file is an accelerator and cannot carry one, so the
// refusal is here rather than in the file. The wire report comes back beside the
// bytes so a caller can decide about the softer differences; anything that
// damaged the framing is an error and nothing is written.
func (c *Compiler) Cook(u *ir.Unit, root string, wire []byte, opts CookOptions) ([]byte, CookReport, TableReport, error) {
	var rep CookReport
	m := tabletext.NewModel(u)
	st := m.Lookup(root)
	if st == nil || !st.IsTable {
		return nil, rep, TableReport{}, fmt.Errorf("--root %s names no table in this unit", root)
	}
	inst := m.New(st)
	var wr tabletext.Report
	if _, err := tablewire.Decode(m, inst, wire, &wr); err != nil {
		return nil, rep, publicReport(wr), err
	}
	if wr.Malformed {
		return nil, rep, publicReport(wr), fmt.Errorf("the wire is malformed: a region loaded from it would carry a hole, and a cook cannot (docs/SPEC-TABLES.md §7)")
	}
	out, err := tablecook.Cook(m, inst, tablecook.Options{Big: opts.Big, NoAttribution: opts.NoAttribution})
	if err != nil {
		return nil, rep, publicReport(wr), err
	}
	// the report is taken off the file just written, so what a caller is told is
	// what a reader will find rather than what the writer believed
	check, err := tablecook.Check(m, out)
	if err != nil {
		if opts.NoAttribution {
			h, herr := tablecook.ReadHeader(out, ir.BuildVersion(u))
			if herr != nil {
				return nil, rep, publicReport(wr), herr
			}
			return out, CookReport{
				BuildVersion: h.BuildVersion,
				ByteOrder:    orderWord(opts.Big),
				Root:         root,
				DataBytes:    h.DataLength,
			}, publicReport(wr), nil
		}
		return nil, rep, publicReport(wr), err
	}
	return out, CookReport(check), publicReport(wr), nil
}

func orderWord(big bool) string {
	if big {
		return "big"
	}
	return "little"
}

// CookCheck is the OFFLINE VALIDATOR §7 assigns to the tool: the two-pass
// attribution scan — the directory linearly, then every node in directory
// order, following no reference and decoding no value — at O(R + P log N), with
// no allocation per node and no per-node state, terminating on every input.
//
// It is a person's decision to run it, not a parameter on a load: the runtime
// keeps one `Open` that matches the header and points.
func (c *Compiler) CookCheck(u *ir.Unit, root string, file []byte) (CookReport, error) {
	m := tabletext.NewModel(u)
	res, err := tablecook.Check(m, file)
	if err != nil {
		return CookReport{}, err
	}
	if root != "" && res.Root != root {
		return CookReport(res), fmt.Errorf("this cook's root is %s and --root names %s", res.Root, root)
	}
	return CookReport(res), nil
}

// CookSplitAttribution separates a cook's two parts: the file with the
// attribution removed and its length word zeroed, and the attribution on its
// own. It is header arithmetic and reads no declaration, because the
// attribution is separable and a caller may place the parts together or apart
// (docs/SPEC-TABLES.md §6.3).
func CookSplitAttribution(file []byte) (data, attribution []byte, err error) {
	return tablecook.SplitAttribution(file)
}

// CookJoinAttribution is the inverse, and it is exact: a split moves nothing
// but the length word, so rejoining reproduces the writer's own bytes.
func CookJoinAttribution(file, attribution []byte) ([]byte, error) {
	return tablecook.JoinAttribution(file, attribution)
}

// Uncook reads a cook back into the TOLERANT WIRE — the round trip that proves
// no fact was lost in producing one. It is a tool's path and never a runtime's:
// a runtime points at a cook where it lies.
func (c *Compiler) Uncook(u *ir.Unit, root string, file []byte) ([]byte, error) {
	m := tabletext.NewModel(u)
	st := m.Lookup(root)
	if st == nil || !st.IsTable {
		return nil, fmt.Errorf("--root %s names no table in this unit", root)
	}
	inst, err := tablecook.Uncook(m, st, file)
	if err != nil {
		return nil, err
	}
	return tablewire.Encode(m, inst)
}
