package main

import (
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

// Fixed roots use the generated fixed codec for every binary leg, including
// the save after FromJSON and the load before ToJSON/report. The existing row
// retains its JSON/cook callbacks and its plain or regional value ownership.
// A conformance root holds one value; the native reader receives one slot.
func bindFixed[T, E, R any](unit, root string,
	load func([]T, []byte, []E, *R) int64,
	measure func(int64) int64,
	save func([]T, []byte) (int64, error),
	planMax int32,
	snap func(*R) report,
) {
	c := findCodec(unit, root)
	if c == nil {
		panic("fixed codec has no value row: " + unit + "." + root)
	}
	value := func(v any) *T {
		switch v := v.(type) {
		case *T:
			return v
		case *regionValue[T]:
			if v.root == nil {
				v.root = new(T)
			}
			return v.root
		default:
			panic("fixed codec has the wrong value storage: " + unit + "." + root)
		}
	}
	c.loadMeasure = nil // fixed storage is the typed row, not a variable region
	c.load = func(v any, wire []byte, rep *report) bool {
		var rows [1]T
		var inner R
		n := load(rows[:], wire, make([]E, planMax), &inner)
		*rep = snap(&inner)
		if n != 1 || rep.refused {
			rep.refused = true
			return false
		}
		*value(v) = rows[0]
		return true
	}
	c.measure = func(any) int64 { return measure(1) }
	c.measureReason = nil
	c.save = func(v any, buffer []byte) int64 {
		n, err := save([]T{*value(v)}, buffer)
		if err != nil {
			return -1
		}
		return n
	}
}

func init() {
	bindFixed("backenddemo", "LoginRequest", backenddemo.LoginRequestFixedLoad, backenddemo.LoginRequestFixedMeasure, backenddemo.LoginRequestFixedSaveReason, backenddemo.LoginRequestFixedPlanMax, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("backenddemo", "MatchResult", backenddemo.MatchResultFixedLoad, backenddemo.MatchResultFixedMeasure, backenddemo.MatchResultFixedSaveReason, backenddemo.MatchResultFixedPlanMax, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("backenddemo", "StorePurchase", backenddemo.StorePurchaseFixedLoad, backenddemo.StorePurchaseFixedMeasure, backenddemo.StorePurchaseFixedSaveReason, backenddemo.StorePurchaseFixedPlanMax, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("backenddemo", "Envelope", backenddemo.EnvelopeFixedLoad, backenddemo.EnvelopeFixedMeasure, backenddemo.EnvelopeFixedSaveReason, backenddemo.EnvelopeFixedPlanMax, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("vocabdemo", "Wide00", vocabdemo.Wide00FixedLoad, vocabdemo.Wide00FixedMeasure, vocabdemo.Wide00FixedSaveReason, vocabdemo.Wide00FixedPlanMax, func(r *vocabdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("vocabdemo", "Wide09", vocabdemo.Wide09FixedLoad, vocabdemo.Wide09FixedMeasure, vocabdemo.Wide09FixedSaveReason, vocabdemo.Wide09FixedPlanMax, func(r *vocabdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("vocab9demo", "Wide00", vocab9demo.Wide00FixedLoad, vocab9demo.Wide00FixedMeasure, vocab9demo.Wide00FixedSaveReason, vocab9demo.Wide00FixedPlanMax, func(r *vocab9demo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("vocab9demo", "Wide19", vocab9demo.Wide19FixedLoad, vocab9demo.Wide19FixedMeasure, vocab9demo.Wide19FixedSaveReason, vocab9demo.Wide19FixedPlanMax, func(r *vocab9demo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("messagedemo", "ToolMessage", messagedemo.ToolMessageFixedLoad, messagedemo.ToolMessageFixedMeasure, messagedemo.ToolMessageFixedSaveReason, messagedemo.ToolMessageFixedPlanMax, func(r *messagedemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("widedemo", "Caption", widedemo.CaptionFixedLoad, widedemo.CaptionFixedMeasure, widedemo.CaptionFixedSaveReason, widedemo.CaptionFixedPlanMax, func(r *widedemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("widedemo", "Stamp", widedemo.StampFixedLoad, widedemo.StampFixedMeasure, widedemo.StampFixedSaveReason, widedemo.StampFixedPlanMax, func(r *widedemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("scalars", "SimState", scalardemo.SimStateFixedLoad, scalardemo.SimStateFixedMeasure, scalardemo.SimStateFixedSaveReason, scalardemo.SimStateFixedPlanMax, func(r *scalardemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tblscalars2", "SimState", tblscalars2.SimStateFixedLoad, tblscalars2.SimStateFixedMeasure, tblscalars2.SimStateFixedSaveReason, tblscalars2.SimStateFixedPlanMax, func(r *tblscalars2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tblm1", "Msg", tblm1.MsgFixedLoad, tblm1.MsgFixedMeasure, tblm1.MsgFixedSaveReason, tblm1.MsgFixedPlanMax, func(r *tblm1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tblm2", "Msg", tblm2.MsgFixedLoad, tblm2.MsgFixedMeasure, tblm2.MsgFixedSaveReason, tblm2.MsgFixedPlanMax, func(r *tblm2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tbla1", "Root", tbla1.RootFixedLoad, tbla1.RootFixedMeasure, tbla1.RootFixedSaveReason, tbla1.RootFixedPlanMax, func(r *tbla1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tbla2", "Root", tbla2.RootFixedLoad, tbla2.RootFixedMeasure, tbla2.RootFixedSaveReason, tbla2.RootFixedPlanMax, func(r *tbla2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tblk1", "Root", tblk1.RootFixedLoad, tblk1.RootFixedMeasure, tblk1.RootFixedSaveReason, tblk1.RootFixedPlanMax, func(r *tblk1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tblk2", "Root", tblk2.RootFixedLoad, tblk2.RootFixedMeasure, tblk2.RootFixedSaveReason, tblk2.RootFixedPlanMax, func(r *tblk2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("listdemo", "Bounded", listdemo.BoundedFixedLoad, listdemo.BoundedFixedMeasure, listdemo.BoundedFixedSaveReason, listdemo.BoundedFixedPlanMax, func(r *listdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tblr1", "Cfg", tblr1.CfgFixedLoad, tblr1.CfgFixedMeasure, tblr1.CfgFixedSaveReason, tblr1.CfgFixedPlanMax, func(r *tblr1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tblr2", "Cfg", tblr2.CfgFixedLoad, tblr2.CfgFixedMeasure, tblr2.CfgFixedSaveReason, tblr2.CfgFixedPlanMax, func(r *tblr2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	})
	bindFixed("tabledemo", "RootConfig", tabledemo.RootConfigFixedLoad, tabledemo.RootConfigFixedMeasure, tabledemo.RootConfigFixedSaveReason, tabledemo.RootConfigFixedPlanMax, snapDemo)
	bindFixed("tabledemo", "ProfileConfig", tabledemo.ProfileConfigFixedLoad, tabledemo.ProfileConfigFixedMeasure, tabledemo.ProfileConfigFixedSaveReason, tabledemo.ProfileConfigFixedPlanMax, snapDemo)
	bindFixed("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigFixedLoad, tabledemo.LoadoutConfigFixedMeasure, tabledemo.LoadoutConfigFixedSaveReason, tabledemo.LoadoutConfigFixedPlanMax, snapDemo)
	bindFixed("tabledemo", "ArchiveConfig", tabledemo.ArchiveConfigFixedLoad, tabledemo.ArchiveConfigFixedMeasure, tabledemo.ArchiveConfigFixedSaveReason, tabledemo.ArchiveConfigFixedPlanMax, snapDemo)
	bindFixed("tabledemo", "PackConfig", tabledemo.PackConfigFixedLoad, tabledemo.PackConfigFixedMeasure, tabledemo.PackConfigFixedSaveReason, tabledemo.PackConfigFixedPlanMax, snapDemo)
	bindFixed("tabledemo", "KeyedConfig", tabledemo.KeyedConfigFixedLoad, tabledemo.KeyedConfigFixedMeasure, tabledemo.KeyedConfigFixedSaveReason, tabledemo.KeyedConfigFixedPlanMax, snapDemo)
	bindFixed("tblv1", "Cfg", tblv1.CfgFixedLoad, tblv1.CfgFixedMeasure, tblv1.CfgFixedSaveReason, tblv1.CfgFixedPlanMax, snapV1)
	bindFixed("tblv2", "Cfg", tblv2.CfgFixedLoad, tblv2.CfgFixedMeasure, tblv2.CfgFixedSaveReason, tblv2.CfgFixedPlanMax, snapV2)
	bindFixed("tblp1", "Chain", tblp1.ChainFixedLoad, tblp1.ChainFixedMeasure, tblp1.ChainFixedSaveReason, tblp1.ChainFixedPlanMax, snapP1)
	bindFixed("tblp3", "Chain", tblp3.ChainFixedLoad, tblp3.ChainFixedMeasure, tblp3.ChainFixedSaveReason, tblp3.ChainFixedPlanMax, snapP3)
}
