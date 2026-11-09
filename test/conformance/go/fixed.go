// THE FIXED ROOTS (docs/SPEC-TABLES.md §3.4, §6.5).
//
// A root declared `fixed table` is a form 3 file: the one read of it is the
// generated <Root>FixedLoad, which names a form 1 file previous_form, and its
// write is <Root>FixedSave. The corpus holds every fixed root's positive
// instances in form 3 (testdata/conformance/tables/wire/) and keeps the
// historical form 1 bytes as previous_form refusals, so the surfaces that ask
// for the read (wire, report, json-write, cook-write, the wire fuzz) and the
// write (wire, report, json-read, framing) take the fixed form here.
//
// The FRAMING vectors are the exception the harness states
// (test/conformance/harness/generate.go): they are form 1 bytes read through
// the VARIABLE form's entry point (<Root>Load) at a fixed root, and the value
// they leave is written back in the root's own form. `variableLoad` keeps
// that read for surfaceFraming alone.
package main

import (
	"reflect"
	"slices"
	"unsafe"

	"github.com/mas-bandwidth/schema/v2/tables/goruntime"

	"backenddemo"
	"listdemo"
	"messagedemo"
	"scalardemo"
	"tabledemo"
	"tbla1"
	"tbla2"
	"tblk1"
	"tblk2"
	"tblm1"
	"tblm2"
	"tblp1"
	"tblp3"
	"tblr1"
	"tblr2"
	tblscalars2 "tblscalars2"
	"tblv1"
	"tblv2"
	"vocab9demo"
	"vocabdemo"
	widedemo "widedemo"
)

// fixedPlanCapacity is the scratch a fixed read is handed for its plan; the
// largest corpus layout is far under it.
const fixedPlanCapacity = 1 << 14

// snapReport narrows any unit's TableReport: every unit's report carries the
// Unknown and Widened counters and the Refused verdict under the same names.
func snapReport(r any) report {
	v := reflect.ValueOf(r).Elem()
	return report{
		unknown: int32(v.FieldByName("Unknown").Int()),
		widened: int32(v.FieldByName("Widened").Int()),
		refused: r.(interface{ Refused() bool }).Refused(),
	}
}

func bigEndianHost() bool {
	v := uint16(1)
	return *(*byte)(unsafe.Pointer(&v)) == 0
}

// fixedForm rebinds a row's read and write to the fixed form. A root file
// holds one record, so the read is handed room for one and accepts only a file
// of exactly one.
func buildFieldSizes(t reflect.Type, base uintptr, out []uint8) {
	switch t.Kind() {
	case reflect.Struct:
		for f := range t.Fields() {
			buildFieldSizes(f.Type, base+f.Offset, out)
		}
	case reflect.Array:
		elem := t.Elem()
		stride := elem.Size()
		for i := range t.Len() {
			buildFieldSizes(elem, base+uintptr(i)*stride, out)
		}
	case reflect.Bool, reflect.Int8, reflect.Uint8,
		reflect.Int16, reflect.Uint16,
		reflect.Int32, reflect.Uint32, reflect.Float32,
		reflect.Int64, reflect.Uint64, reflect.Float64:
		out[base] = uint8(t.Size())
	}
}

func fixedForm[T any, R any, E any](
	c codec,
	load func([]T, []byte, []E, *R) int64,
	measure func(int64) int64,
	save func([]T, []byte) int64,
	leaves func([]goruntime.TableFixedEntry, [][]goruntime.TableFixedGuardLink, uint32, uint32) int,
) codec {
	plan := make([]E, fixedPlanCapacity)
	var rawEntries []goruntime.TableFixedEntry
	var rawLinks [][]goruntime.TableFixedGuardLink
	var leafCount int
	var idPlan goruntime.TableFixedPlan
	var fieldSizes []uint8
	if bigEndianHost() {
		rawEntries = make([]goruntime.TableFixedEntry, fixedPlanCapacity)
		rawLinks = make([][]goruntime.TableFixedGuardLink, fixedPlanCapacity)
		leafCount = leaves(rawEntries, rawLinks, 0, 0)
		idPlan = goruntime.TableFixedBuildPlan(leaves, leafCount)
		rt := reflect.TypeFor[T]()
		fieldSizes = make([]uint8, rt.Size())
		buildFieldSizes(rt, 0, fieldSizes)
	}
	c.variableLoad = c.load
	c.load = func(value any, wire []byte, rep *report) bool {
		var inner R
		if !bigEndianHost() || len(wire) < 1 || wire[0] != goruntime.TableFixedForm {
			n := load(unsafe.Slice(value.(*T), 1), wire, plan, &inner)
			*rep = snapReport(&inner)
			return n == 1 && !rep.refused
		}
		load(nil, wire, plan, &inner)
		if reflect.ValueOf(&inner).Elem().FieldByName("Reason").String() != goruntime.TableRefuseBatchTooLarge.Error() {
			*rep = snapReport(&inner)
			return false
		}
		if int64(len(wire)) != measure(1) {
			*rep = report{refused: true}
			return false
		}
		hash := goruntime.TableFixedGet64(wire[goruntime.TableFixedHashAt:])
		layoutBytes := goruntime.TableFixedGet32(wire[goruntime.TableFixedHeaderBytes:])
		at := wire[goruntime.TableFixedHeaderBytes+4+int(layoutBytes):]
		if goruntime.TableFixedGet64(at) != hash {
			*rep = report{refused: true}
			return false
		}
		src := at[8:]
		if why := goruntime.TableFixedTextRefusal(idPlan.Entries, idPlan.Count, src); why != "" {
			*rep = report{refused: true}
			return false
		}
		v := value.(*T)
		dst := goruntime.TableFixedOverlay(unsafe.Pointer(v), unsafe.Sizeof(*v))
		var gr goruntime.TableReport
		if why := goruntime.TableFixedRun(idPlan.Entries, idPlan.Count, src, dst, &gr); why != "" {
			*rep = report{refused: true}
			return false
		}
	leafLoop:
		for i := range leafCount {
			for _, l := range rawLinks[i] {
				if goruntime.TableFixedTagAt(src, l.Guard, l.ArgW) != l.Arg {
					continue leafLoop
				}
			}
			e := rawEntries[i]
			switch e.Op {
			case goruntime.TableFixedCopy, goruntime.TableFixedTag:
				if e.Size == 16 {
					slices.Reverse(dst[e.Dst : e.Dst+8])
					slices.Reverse(dst[e.Dst+8 : e.Dst+16])
					break
				}
				w := max(uint32(fieldSizes[e.Dst]), e.Size)
				if w > e.Size {
					clear(dst[e.Dst+e.Size : e.Dst+w])
				}
				if w == 2 || w == 4 || w == 8 {
					slices.Reverse(dst[e.Dst : e.Dst+w])
				}
			case goruntime.TableFixedCount:
				slices.Reverse(dst[e.Dst : e.Dst+4])
			case goruntime.TableFixedText:
				slices.Reverse(dst[e.Dst : e.Dst+4])
				if e.Meta == goruntime.TableFixedTextWide {
					for off := uint32(0); off < e.Size; off += 2 {
						slices.Reverse(dst[e.Aux+off : e.Aux+off+2])
					}
				}
			}
		}
		*rep = report{unknown: gr.Unknown, widened: gr.Widened}
		return true
	}
	c.measure = func(any) int64 { return measure(1) }
	c.measureReason = nil
	c.save = func(value any, buffer []byte) int64 {
		return save(unsafe.Slice(value.(*T), 1), buffer)
	}
	return c
}

// noFileForm rebinds a declared fixed root past §3.4's 65536-byte record
// bound, which has no file form: every file read refuses and nothing is
// saved, while variableLoad keeps the form-1 read for surfaceFraming alone.
func noFileForm(c codec) codec {
	c.variableLoad = c.load
	c.load = func(value any, wire []byte, rep *report) bool {
		*rep = report{refused: true}
		return false
	}
	c.measure = func(any) int64 { return -1 }
	c.measureReason = nil
	c.save = func(any, []byte) int64 { return -1 }
	return c
}

func init() {
	bind := func(unit, root string, rebind func(codec) codec) {
		c := findCodec(unit, root)
		if c == nil {
			panic("fixed root with no codec row: " + unit + " " + root)
		}
		*c = rebind(*c)
	}
	bind("tabledemo", "RootConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.RootConfigFixedLoad, tabledemo.RootConfigFixedMeasure, tabledemo.RootConfigFixedSave, tabledemo.RootConfigFixedLeaves)
	})
	bind("tabledemo", "ProfileConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.ProfileConfigFixedLoad, tabledemo.ProfileConfigFixedMeasure, tabledemo.ProfileConfigFixedSave, tabledemo.ProfileConfigFixedLeaves)
	})
	bind("tabledemo", "LoadoutConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.LoadoutConfigFixedLoad, tabledemo.LoadoutConfigFixedMeasure, tabledemo.LoadoutConfigFixedSave, tabledemo.LoadoutConfigFixedLeaves)
	})
	bind("tabledemo", "ArchiveConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.ArchiveConfigFixedLoad, tabledemo.ArchiveConfigFixedMeasure, tabledemo.ArchiveConfigFixedSave, tabledemo.ArchiveConfigFixedLeaves)
	})
	bind("tabledemo", "PackConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.PackConfigFixedLoad, tabledemo.PackConfigFixedMeasure, tabledemo.PackConfigFixedSave, tabledemo.PackConfigFixedLeaves)
	})
	bind("tabledemo", "KeyedConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.KeyedConfigFixedLoad, tabledemo.KeyedConfigFixedMeasure, tabledemo.KeyedConfigFixedSave, tabledemo.KeyedConfigFixedLeaves)
	})
	bind("tabledemo", "WideBlob", noFileForm)
	bind("tblv1", "Cfg", func(c codec) codec {
		return fixedForm(c, tblv1.CfgFixedLoad, tblv1.CfgFixedMeasure, tblv1.CfgFixedSave, tblv1.CfgFixedLeaves)
	})
	bind("tblv2", "Cfg", func(c codec) codec {
		return fixedForm(c, tblv2.CfgFixedLoad, tblv2.CfgFixedMeasure, tblv2.CfgFixedSave, tblv2.CfgFixedLeaves)
	})
	bind("tblp1", "Chain", func(c codec) codec {
		return fixedForm(c, tblp1.ChainFixedLoad, tblp1.ChainFixedMeasure, tblp1.ChainFixedSave, tblp1.ChainFixedLeaves)
	})
	bind("tblp3", "Chain", func(c codec) codec {
		return fixedForm(c, tblp3.ChainFixedLoad, tblp3.ChainFixedMeasure, tblp3.ChainFixedSave, tblp3.ChainFixedLeaves)
	})
	bind("tblr1", "Cfg", func(c codec) codec {
		return fixedForm(c, tblr1.CfgFixedLoad, tblr1.CfgFixedMeasure, tblr1.CfgFixedSave, tblr1.CfgFixedLeaves)
	})
	bind("tblr2", "Cfg", func(c codec) codec {
		return fixedForm(c, tblr2.CfgFixedLoad, tblr2.CfgFixedMeasure, tblr2.CfgFixedSave, tblr2.CfgFixedLeaves)
	})
	bind("tblm1", "Msg", func(c codec) codec {
		return fixedForm(c, tblm1.MsgFixedLoad, tblm1.MsgFixedMeasure, tblm1.MsgFixedSave, tblm1.MsgFixedLeaves)
	})
	bind("tblm2", "Msg", func(c codec) codec {
		return fixedForm(c, tblm2.MsgFixedLoad, tblm2.MsgFixedMeasure, tblm2.MsgFixedSave, tblm2.MsgFixedLeaves)
	})
	bind("tbla1", "Root", func(c codec) codec {
		return fixedForm(c, tbla1.RootFixedLoad, tbla1.RootFixedMeasure, tbla1.RootFixedSave, tbla1.RootFixedLeaves)
	})
	bind("tbla2", "Root", func(c codec) codec {
		return fixedForm(c, tbla2.RootFixedLoad, tbla2.RootFixedMeasure, tbla2.RootFixedSave, tbla2.RootFixedLeaves)
	})
	bind("tblk1", "Root", func(c codec) codec {
		return fixedForm(c, tblk1.RootFixedLoad, tblk1.RootFixedMeasure, tblk1.RootFixedSave, tblk1.RootFixedLeaves)
	})
	bind("tblk2", "Root", func(c codec) codec {
		return fixedForm(c, tblk2.RootFixedLoad, tblk2.RootFixedMeasure, tblk2.RootFixedSave, tblk2.RootFixedLeaves)
	})
	bind("listdemo", "Bounded", func(c codec) codec {
		return fixedForm(c, listdemo.BoundedFixedLoad, listdemo.BoundedFixedMeasure, listdemo.BoundedFixedSave, listdemo.BoundedFixedLeaves)
	})
	bind("scalars", "SimState", func(c codec) codec {
		return fixedForm(c, scalardemo.SimStateFixedLoad, scalardemo.SimStateFixedMeasure, scalardemo.SimStateFixedSave, scalardemo.SimStateFixedLeaves)
	})
	bind("tblscalars2", "SimState", func(c codec) codec {
		return fixedForm(c, tblscalars2.SimStateFixedLoad, tblscalars2.SimStateFixedMeasure, tblscalars2.SimStateFixedSave, tblscalars2.SimStateFixedLeaves)
	})
	bind("messagedemo", "ToolMessage", func(c codec) codec {
		return fixedForm(c, messagedemo.ToolMessageFixedLoad, messagedemo.ToolMessageFixedMeasure, messagedemo.ToolMessageFixedSave, messagedemo.ToolMessageFixedLeaves)
	})
	bind("backenddemo", "LoginRequest", func(c codec) codec {
		return fixedForm(c, backenddemo.LoginRequestFixedLoad, backenddemo.LoginRequestFixedMeasure, backenddemo.LoginRequestFixedSave, backenddemo.LoginRequestFixedLeaves)
	})
	bind("backenddemo", "MatchResult", func(c codec) codec {
		return fixedForm(c, backenddemo.MatchResultFixedLoad, backenddemo.MatchResultFixedMeasure, backenddemo.MatchResultFixedSave, backenddemo.MatchResultFixedLeaves)
	})
	bind("backenddemo", "StorePurchase", func(c codec) codec {
		return fixedForm(c, backenddemo.StorePurchaseFixedLoad, backenddemo.StorePurchaseFixedMeasure, backenddemo.StorePurchaseFixedSave, backenddemo.StorePurchaseFixedLeaves)
	})
	bind("backenddemo", "Envelope", func(c codec) codec {
		return fixedForm(c, backenddemo.EnvelopeFixedLoad, backenddemo.EnvelopeFixedMeasure, backenddemo.EnvelopeFixedSave, backenddemo.EnvelopeFixedLeaves)
	})
	bind("vocabdemo", "Wide00", func(c codec) codec {
		return fixedForm(c, vocabdemo.Wide00FixedLoad, vocabdemo.Wide00FixedMeasure, vocabdemo.Wide00FixedSave, vocabdemo.Wide00FixedLeaves)
	})
	bind("vocabdemo", "Wide09", func(c codec) codec {
		return fixedForm(c, vocabdemo.Wide09FixedLoad, vocabdemo.Wide09FixedMeasure, vocabdemo.Wide09FixedSave, vocabdemo.Wide09FixedLeaves)
	})
	bind("vocab9demo", "Wide00", func(c codec) codec {
		return fixedForm(c, vocab9demo.Wide00FixedLoad, vocab9demo.Wide00FixedMeasure, vocab9demo.Wide00FixedSave, vocab9demo.Wide00FixedLeaves)
	})
	bind("vocab9demo", "Wide19", func(c codec) codec {
		return fixedForm(c, vocab9demo.Wide19FixedLoad, vocab9demo.Wide19FixedMeasure, vocab9demo.Wide19FixedSave, vocab9demo.Wide19FixedLeaves)
	})
	bind("widedemo", "Caption", func(c codec) codec {
		return fixedForm(c, widedemo.CaptionFixedLoad, widedemo.CaptionFixedMeasure, widedemo.CaptionFixedSave, widedemo.CaptionFixedLeaves)
	})
	bind("widedemo", "Stamp", func(c codec) codec {
		return fixedForm(c, widedemo.StampFixedLoad, widedemo.StampFixedMeasure, widedemo.StampFixedSave, widedemo.StampFixedLeaves)
	})
}
