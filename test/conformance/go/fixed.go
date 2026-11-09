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
	"unsafe"

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

// fixedForm rebinds a row's read and write to the fixed form. A root file
// holds one record, so the read is handed room for one and accepts only a file
// of exactly one.
func fixedForm[T any, R any, E any](
	c codec,
	load func([]T, []byte, []E, *R) int64,
	measure func(int64) int64,
	save func([]T, []byte) int64,
) codec {
	plan := make([]E, fixedPlanCapacity)
	c.variableLoad = c.load
	c.load = func(value any, wire []byte, rep *report) bool {
		var inner R
		n := load(unsafe.Slice(value.(*T), 1), wire, plan, &inner)
		*rep = snapReport(&inner)
		return n == 1 && !rep.refused
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
		return fixedForm(c, tabledemo.RootConfigFixedLoad, tabledemo.RootConfigFixedMeasure, tabledemo.RootConfigFixedSave)
	})
	bind("tabledemo", "ProfileConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.ProfileConfigFixedLoad, tabledemo.ProfileConfigFixedMeasure, tabledemo.ProfileConfigFixedSave)
	})
	bind("tabledemo", "LoadoutConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.LoadoutConfigFixedLoad, tabledemo.LoadoutConfigFixedMeasure, tabledemo.LoadoutConfigFixedSave)
	})
	bind("tabledemo", "ArchiveConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.ArchiveConfigFixedLoad, tabledemo.ArchiveConfigFixedMeasure, tabledemo.ArchiveConfigFixedSave)
	})
	bind("tabledemo", "PackConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.PackConfigFixedLoad, tabledemo.PackConfigFixedMeasure, tabledemo.PackConfigFixedSave)
	})
	bind("tabledemo", "KeyedConfig", func(c codec) codec {
		return fixedForm(c, tabledemo.KeyedConfigFixedLoad, tabledemo.KeyedConfigFixedMeasure, tabledemo.KeyedConfigFixedSave)
	})
	bind("tabledemo", "WideBlob", noFileForm)
	bind("tblv1", "Cfg", func(c codec) codec {
		return fixedForm(c, tblv1.CfgFixedLoad, tblv1.CfgFixedMeasure, tblv1.CfgFixedSave)
	})
	bind("tblv2", "Cfg", func(c codec) codec {
		return fixedForm(c, tblv2.CfgFixedLoad, tblv2.CfgFixedMeasure, tblv2.CfgFixedSave)
	})
	bind("tblp1", "Chain", func(c codec) codec {
		return fixedForm(c, tblp1.ChainFixedLoad, tblp1.ChainFixedMeasure, tblp1.ChainFixedSave)
	})
	bind("tblp3", "Chain", func(c codec) codec {
		return fixedForm(c, tblp3.ChainFixedLoad, tblp3.ChainFixedMeasure, tblp3.ChainFixedSave)
	})
	bind("tblr1", "Cfg", func(c codec) codec {
		return fixedForm(c, tblr1.CfgFixedLoad, tblr1.CfgFixedMeasure, tblr1.CfgFixedSave)
	})
	bind("tblr2", "Cfg", func(c codec) codec {
		return fixedForm(c, tblr2.CfgFixedLoad, tblr2.CfgFixedMeasure, tblr2.CfgFixedSave)
	})
	bind("tblm1", "Msg", func(c codec) codec {
		return fixedForm(c, tblm1.MsgFixedLoad, tblm1.MsgFixedMeasure, tblm1.MsgFixedSave)
	})
	bind("tblm2", "Msg", func(c codec) codec {
		return fixedForm(c, tblm2.MsgFixedLoad, tblm2.MsgFixedMeasure, tblm2.MsgFixedSave)
	})
	bind("tbla1", "Root", func(c codec) codec {
		return fixedForm(c, tbla1.RootFixedLoad, tbla1.RootFixedMeasure, tbla1.RootFixedSave)
	})
	bind("tbla2", "Root", func(c codec) codec {
		return fixedForm(c, tbla2.RootFixedLoad, tbla2.RootFixedMeasure, tbla2.RootFixedSave)
	})
	bind("tblk1", "Root", func(c codec) codec {
		return fixedForm(c, tblk1.RootFixedLoad, tblk1.RootFixedMeasure, tblk1.RootFixedSave)
	})
	bind("tblk2", "Root", func(c codec) codec {
		return fixedForm(c, tblk2.RootFixedLoad, tblk2.RootFixedMeasure, tblk2.RootFixedSave)
	})
	bind("listdemo", "Bounded", func(c codec) codec {
		return fixedForm(c, listdemo.BoundedFixedLoad, listdemo.BoundedFixedMeasure, listdemo.BoundedFixedSave)
	})
	bind("scalars", "SimState", func(c codec) codec {
		return fixedForm(c, scalardemo.SimStateFixedLoad, scalardemo.SimStateFixedMeasure, scalardemo.SimStateFixedSave)
	})
	bind("tblscalars2", "SimState", func(c codec) codec {
		return fixedForm(c, tblscalars2.SimStateFixedLoad, tblscalars2.SimStateFixedMeasure, tblscalars2.SimStateFixedSave)
	})
	bind("messagedemo", "ToolMessage", func(c codec) codec {
		return fixedForm(c, messagedemo.ToolMessageFixedLoad, messagedemo.ToolMessageFixedMeasure, messagedemo.ToolMessageFixedSave)
	})
	bind("backenddemo", "LoginRequest", func(c codec) codec {
		return fixedForm(c, backenddemo.LoginRequestFixedLoad, backenddemo.LoginRequestFixedMeasure, backenddemo.LoginRequestFixedSave)
	})
	bind("backenddemo", "MatchResult", func(c codec) codec {
		return fixedForm(c, backenddemo.MatchResultFixedLoad, backenddemo.MatchResultFixedMeasure, backenddemo.MatchResultFixedSave)
	})
	bind("backenddemo", "StorePurchase", func(c codec) codec {
		return fixedForm(c, backenddemo.StorePurchaseFixedLoad, backenddemo.StorePurchaseFixedMeasure, backenddemo.StorePurchaseFixedSave)
	})
	bind("backenddemo", "Envelope", func(c codec) codec {
		return fixedForm(c, backenddemo.EnvelopeFixedLoad, backenddemo.EnvelopeFixedMeasure, backenddemo.EnvelopeFixedSave)
	})
	bind("vocabdemo", "Wide00", func(c codec) codec {
		return fixedForm(c, vocabdemo.Wide00FixedLoad, vocabdemo.Wide00FixedMeasure, vocabdemo.Wide00FixedSave)
	})
	bind("vocabdemo", "Wide09", func(c codec) codec {
		return fixedForm(c, vocabdemo.Wide09FixedLoad, vocabdemo.Wide09FixedMeasure, vocabdemo.Wide09FixedSave)
	})
	bind("vocab9demo", "Wide00", func(c codec) codec {
		return fixedForm(c, vocab9demo.Wide00FixedLoad, vocab9demo.Wide00FixedMeasure, vocab9demo.Wide00FixedSave)
	})
	bind("vocab9demo", "Wide19", func(c codec) codec {
		return fixedForm(c, vocab9demo.Wide19FixedLoad, vocab9demo.Wide19FixedMeasure, vocab9demo.Wide19FixedSave)
	})
	bind("widedemo", "Caption", func(c codec) codec {
		return fixedForm(c, widedemo.CaptionFixedLoad, widedemo.CaptionFixedMeasure, widedemo.CaptionFixedSave)
	})
	bind("widedemo", "Stamp", func(c codec) codec {
		return fixedForm(c, widedemo.StampFixedLoad, widedemo.StampFixedMeasure, widedemo.StampFixedSave)
	})
}
