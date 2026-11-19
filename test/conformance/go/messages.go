package main

import (
	"backenddemo"
	"fmt"
	"messagedemo"
	"os"
	"path/filepath"
	"scalardemo"
	"tabledemo"
	"tbla1"
	"tbla2"
	"tblfixedevo"
	"tblk1"
	"tblk2"
	"tblm1"
	"tblm2"
	tblscalars2 "tblscalars2"
	"tblv1"
	"tblv2"
	"tblvarevo1"
	"tblvarevo2"
	"tblvarevo3"
	"vocab9demo"
	"vocabdemo"
)

type messageCodec struct {
	unit, root  string
	run         func([]byte, []byte, bool) ([]byte, report, bool)
	loadMeasure func([]byte) int64
	// saveOne writes the one value through this root's SaveMessages and answers
	// the byte length, or -1 refused. The write-refusal surface takes it after
	// loading and mutating the row's value (docs/SPEC.md §5.1).
	saveOne func(value any) int64
}

func messageRow[T, R, V any](unit, root string, reset func(*T), fresh func() *V, announceMeasure func() int64, announce func([]byte) int64, announceRead func(*V, []byte, *R) bool, load func([]T, *V, []byte, *R) (int64, bool), measure func([]*T, *R) int64, save func([]*T, []byte, *R) int64, snap func(*R) report) messageCodec {
	var own *V
	values := make([]T, 256)
	pointers := make([]*T, 256)
	for i := range values {
		pointers[i] = &values[i]
	}
	return messageCodec{unit: unit, root: root, run: func(announcement, wire []byte, fuzz bool) ([]byte, report, bool) {
		var r R
		v := own
		if announcement != nil {
			v = fresh()
			if !announceRead(v, announcement, &r) {
				return nil, snap(&r), false
			}
		} else if v == nil {
			v = fresh()
			announcement = make([]byte, announceMeasure())
			announce(announcement)
			if !announceRead(v, announcement, &r) {
				return nil, snap(&r), false
			}
			own = v
		}
		for i := range values {
			reset(&values[i])
		}
		count, ok := load(values, v, wire, &r)
		rep := snap(&r)
		if fuzz {
			rep.refused = rep.refused || !ok
			if !ok {
				return nil, rep, false
			}
		} else if !ok {
			return nil, rep, false
		}
		if count < 1 || count > 256 {
			return nil, rep, false
		}
		var ignored R
		n := measure(pointers[:count], &ignored)
		if n < 0 {
			return nil, rep, false
		}
		out := make([]byte, n)
		if save(pointers[:count], out, &ignored) != n {
			return nil, rep, false
		}
		return out, rep, ok
	},
		saveOne: func(value any) int64 {
			v, ok := value.(*T)
			if !ok {
				return -1
			}
			var r R
			n := measure([]*T{v}, &r)
			if n < 0 {
				return -1
			}
			buffer := make([]byte, n)
			if save([]*T{v}, buffer, &r) != n {
				return -1
			}
			return n
		},
	}
}
func findMessageCodec(unit, root string) *messageCodec {
	for i := range messageCodecs {
		c := &messageCodecs[i]
		if c.unit == unit && c.root == root {
			return c
		}
	}
	return nil
}

// spillAbsentReason writes the absent marker with the reason it carries, so the
// matrix prints not just that the case is missing but why.
func spillAbsentReason(dir, name, reason string) error {
	return os.WriteFile(filepath.Join(dir, name+".absent"), []byte(reason), 0o644)
}
func surfaceMessage(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "message" {
			continue
		}
		var unit, path string
		for _, c := range lines {
			if c[0] == "connection" && c[1] == f[2] {
				unit, path = c[2], c[4]
				break
			}
		}
		codec := findMessageCodec(unit, f[3])
		if codec == nil {
			// A message row whose root is a variable table carries no message
			// verbs (docs/SPEC-TABLES.md §3.3: the message form is fixed tables
			// only), so the driver answers ABSENT and names the reason.
			if err := spillAbsentReason(out, f[1], "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)"); err != nil {
				return err
			}
			continue
		}
		announcement, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		wire, err := os.ReadFile(f[5])
		if err != nil {
			return err
		}
		answer, report, ok := codec.run(announcement, wire, false)
		if !ok {
			return fmt.Errorf("%s: message load/save %+v", f[1], report)
		}
		if err := spill(out, f[1], answer); err != nil {
			return err
		}
	}
	return nil
}

var messageCodecs = []messageCodec{
	messageRow("backenddemo", "LoginRequest", backenddemo.LoginRequestReset, func() *backenddemo.TableVocabulary {
		v := new(backenddemo.TableVocabulary)
		v.Init(make([]backenddemo.TableMessageEntry, max(4096, backenddemo.TableMessageEntriesHere)))
		return v
	}, backenddemo.AnnounceMeasure, backenddemo.Announce, backenddemo.AnnounceRead, backenddemo.LoginRequestLoadMessages, backenddemo.LoginRequestMeasureMessages, backenddemo.LoginRequestSaveMessages, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("backenddemo", "MatchResult", backenddemo.MatchResultReset, func() *backenddemo.TableVocabulary {
		v := new(backenddemo.TableVocabulary)
		v.Init(make([]backenddemo.TableMessageEntry, max(4096, backenddemo.TableMessageEntriesHere)))
		return v
	}, backenddemo.AnnounceMeasure, backenddemo.Announce, backenddemo.AnnounceRead, backenddemo.MatchResultLoadMessages, backenddemo.MatchResultMeasureMessages, backenddemo.MatchResultSaveMessages, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("backenddemo", "StorePurchase", backenddemo.StorePurchaseReset, func() *backenddemo.TableVocabulary {
		v := new(backenddemo.TableVocabulary)
		v.Init(make([]backenddemo.TableMessageEntry, max(4096, backenddemo.TableMessageEntriesHere)))
		return v
	}, backenddemo.AnnounceMeasure, backenddemo.Announce, backenddemo.AnnounceRead, backenddemo.StorePurchaseLoadMessages, backenddemo.StorePurchaseMeasureMessages, backenddemo.StorePurchaseSaveMessages, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("backenddemo", "Envelope", backenddemo.EnvelopeReset, func() *backenddemo.TableVocabulary {
		v := new(backenddemo.TableVocabulary)
		v.Init(make([]backenddemo.TableMessageEntry, max(4096, backenddemo.TableMessageEntriesHere)))
		return v
	}, backenddemo.AnnounceMeasure, backenddemo.Announce, backenddemo.AnnounceRead, backenddemo.EnvelopeLoadMessages, backenddemo.EnvelopeMeasureMessages, backenddemo.EnvelopeSaveMessages, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("vocabdemo", "Wide00", vocabdemo.Wide00Reset, func() *vocabdemo.TableVocabulary {
		v := new(vocabdemo.TableVocabulary)
		v.Init(make([]vocabdemo.TableMessageEntry, max(4096, vocabdemo.TableMessageEntriesHere)))
		return v
	}, vocabdemo.AnnounceMeasure, vocabdemo.Announce, vocabdemo.AnnounceRead, vocabdemo.Wide00LoadMessages, vocabdemo.Wide00MeasureMessages, vocabdemo.Wide00SaveMessages, func(r *vocabdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("vocabdemo", "Wide09", vocabdemo.Wide09Reset, func() *vocabdemo.TableVocabulary {
		v := new(vocabdemo.TableVocabulary)
		v.Init(make([]vocabdemo.TableMessageEntry, max(4096, vocabdemo.TableMessageEntriesHere)))
		return v
	}, vocabdemo.AnnounceMeasure, vocabdemo.Announce, vocabdemo.AnnounceRead, vocabdemo.Wide09LoadMessages, vocabdemo.Wide09MeasureMessages, vocabdemo.Wide09SaveMessages, func(r *vocabdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("vocab9demo", "Wide00", vocab9demo.Wide00Reset, func() *vocab9demo.TableVocabulary {
		v := new(vocab9demo.TableVocabulary)
		v.Init(make([]vocab9demo.TableMessageEntry, max(4096, vocab9demo.TableMessageEntriesHere)))
		return v
	}, vocab9demo.AnnounceMeasure, vocab9demo.Announce, vocab9demo.AnnounceRead, vocab9demo.Wide00LoadMessages, vocab9demo.Wide00MeasureMessages, vocab9demo.Wide00SaveMessages, func(r *vocab9demo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("vocab9demo", "Wide19", vocab9demo.Wide19Reset, func() *vocab9demo.TableVocabulary {
		v := new(vocab9demo.TableVocabulary)
		v.Init(make([]vocab9demo.TableMessageEntry, max(4096, vocab9demo.TableMessageEntriesHere)))
		return v
	}, vocab9demo.AnnounceMeasure, vocab9demo.Announce, vocab9demo.AnnounceRead, vocab9demo.Wide19LoadMessages, vocab9demo.Wide19MeasureMessages, vocab9demo.Wide19SaveMessages, func(r *vocab9demo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblvarevo1", "EvoMsg", tblvarevo1.EvoMsgReset, func() *tblvarevo1.TableVocabulary {
		v := new(tblvarevo1.TableVocabulary)
		v.Init(make([]tblvarevo1.TableMessageEntry, max(4096, tblvarevo1.TableMessageEntriesHere)))
		return v
	}, tblvarevo1.AnnounceMeasure, tblvarevo1.Announce, tblvarevo1.AnnounceRead, tblvarevo1.EvoMsgLoadMessages, tblvarevo1.EvoMsgMeasureMessages, tblvarevo1.EvoMsgSaveMessages, func(r *tblvarevo1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblvarevo3", "EvoMsg", tblvarevo3.EvoMsgReset, func() *tblvarevo3.TableVocabulary {
		v := new(tblvarevo3.TableVocabulary)
		v.Init(make([]tblvarevo3.TableMessageEntry, max(4096, tblvarevo3.TableMessageEntriesHere)))
		return v
	}, tblvarevo3.AnnounceMeasure, tblvarevo3.Announce, tblvarevo3.AnnounceRead, tblvarevo3.EvoMsgLoadMessages, tblvarevo3.EvoMsgMeasureMessages, tblvarevo3.EvoMsgSaveMessages, func(r *tblvarevo3.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblvarevo2", "EvoMsg", tblvarevo2.EvoMsgReset, func() *tblvarevo2.TableVocabulary {
		v := new(tblvarevo2.TableVocabulary)
		v.Init(make([]tblvarevo2.TableMessageEntry, max(4096, tblvarevo2.TableMessageEntriesHere)))
		return v
	}, tblvarevo2.AnnounceMeasure, tblvarevo2.Announce, tblvarevo2.AnnounceRead, tblvarevo2.EvoMsgLoadMessages, tblvarevo2.EvoMsgMeasureMessages, tblvarevo2.EvoMsgSaveMessages, func(r *tblvarevo2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("messagedemo", "ToolMessage", messagedemo.ToolMessageReset, func() *messagedemo.TableVocabulary {
		v := new(messagedemo.TableVocabulary)
		v.Init(make([]messagedemo.TableMessageEntry, max(4096, messagedemo.TableMessageEntriesHere)))
		return v
	}, messagedemo.AnnounceMeasure, messagedemo.Announce, messagedemo.AnnounceRead, messagedemo.ToolMessageLoadMessages, messagedemo.ToolMessageMeasureMessages, messagedemo.ToolMessageSaveMessages, func(r *messagedemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblfixedevo", "EvoRoot", tblfixedevo.EvoRootReset, func() *tblfixedevo.TableVocabulary {
		v := new(tblfixedevo.TableVocabulary)
		v.Init(make([]tblfixedevo.TableMessageEntry, max(4096, tblfixedevo.TableMessageEntriesHere)))
		return v
	}, tblfixedevo.AnnounceMeasure, tblfixedevo.Announce, tblfixedevo.AnnounceRead, tblfixedevo.EvoRootLoadMessages, tblfixedevo.EvoRootMeasureMessages, tblfixedevo.EvoRootSaveMessages, func(r *tblfixedevo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tabledemo", "RootConfig", tabledemo.RootConfigReset, func() *tabledemo.TableVocabulary {
		v := new(tabledemo.TableVocabulary)
		v.Init(make([]tabledemo.TableMessageEntry, max(4096, tabledemo.TableMessageEntriesHere)))
		return v
	}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.RootConfigLoadMessages, tabledemo.RootConfigMeasureMessages, tabledemo.RootConfigSaveMessages, func(r *tabledemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tabledemo", "ProfileConfig", tabledemo.ProfileConfigReset, func() *tabledemo.TableVocabulary {
		v := new(tabledemo.TableVocabulary)
		v.Init(make([]tabledemo.TableMessageEntry, max(4096, tabledemo.TableMessageEntriesHere)))
		return v
	}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.ProfileConfigLoadMessages, tabledemo.ProfileConfigMeasureMessages, tabledemo.ProfileConfigSaveMessages, func(r *tabledemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tabledemo", "KeyedConfig", tabledemo.KeyedConfigReset, func() *tabledemo.TableVocabulary {
		v := new(tabledemo.TableVocabulary)
		v.Init(make([]tabledemo.TableMessageEntry, max(4096, tabledemo.TableMessageEntriesHere)))
		return v
	}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.KeyedConfigLoadMessages, tabledemo.KeyedConfigMeasureMessages, tabledemo.KeyedConfigSaveMessages, func(r *tabledemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblv1", "Cfg", tblv1.CfgReset, func() *tblv1.TableVocabulary {
		v := new(tblv1.TableVocabulary)
		v.Init(make([]tblv1.TableMessageEntry, max(4096, tblv1.TableMessageEntriesHere)))
		return v
	}, tblv1.AnnounceMeasure, tblv1.Announce, tblv1.AnnounceRead, tblv1.CfgLoadMessages, tblv1.CfgMeasureMessages, tblv1.CfgSaveMessages, func(r *tblv1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblv2", "Cfg", tblv2.CfgReset, func() *tblv2.TableVocabulary {
		v := new(tblv2.TableVocabulary)
		v.Init(make([]tblv2.TableMessageEntry, max(4096, tblv2.TableMessageEntriesHere)))
		return v
	}, tblv2.AnnounceMeasure, tblv2.Announce, tblv2.AnnounceRead, tblv2.CfgLoadMessages, tblv2.CfgMeasureMessages, tblv2.CfgSaveMessages, func(r *tblv2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblm1", "Msg", tblm1.MsgReset, func() *tblm1.TableVocabulary {
		v := new(tblm1.TableVocabulary)
		v.Init(make([]tblm1.TableMessageEntry, max(4096, tblm1.TableMessageEntriesHere)))
		return v
	}, tblm1.AnnounceMeasure, tblm1.Announce, tblm1.AnnounceRead, tblm1.MsgLoadMessages, tblm1.MsgMeasureMessages, tblm1.MsgSaveMessages, func(r *tblm1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblm2", "Msg", tblm2.MsgReset, func() *tblm2.TableVocabulary {
		v := new(tblm2.TableVocabulary)
		v.Init(make([]tblm2.TableMessageEntry, max(4096, tblm2.TableMessageEntriesHere)))
		return v
	}, tblm2.AnnounceMeasure, tblm2.Announce, tblm2.AnnounceRead, tblm2.MsgLoadMessages, tblm2.MsgMeasureMessages, tblm2.MsgSaveMessages, func(r *tblm2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tbla1", "Root", tbla1.RootReset, func() *tbla1.TableVocabulary {
		v := new(tbla1.TableVocabulary)
		v.Init(make([]tbla1.TableMessageEntry, max(4096, tbla1.TableMessageEntriesHere)))
		return v
	}, tbla1.AnnounceMeasure, tbla1.Announce, tbla1.AnnounceRead, tbla1.RootLoadMessages, tbla1.RootMeasureMessages, tbla1.RootSaveMessages, func(r *tbla1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tbla2", "Root", tbla2.RootReset, func() *tbla2.TableVocabulary {
		v := new(tbla2.TableVocabulary)
		v.Init(make([]tbla2.TableMessageEntry, max(4096, tbla2.TableMessageEntriesHere)))
		return v
	}, tbla2.AnnounceMeasure, tbla2.Announce, tbla2.AnnounceRead, tbla2.RootLoadMessages, tbla2.RootMeasureMessages, tbla2.RootSaveMessages, func(r *tbla2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblk1", "Root", tblk1.RootReset, func() *tblk1.TableVocabulary {
		v := new(tblk1.TableVocabulary)
		v.Init(make([]tblk1.TableMessageEntry, max(4096, tblk1.TableMessageEntriesHere)))
		return v
	}, tblk1.AnnounceMeasure, tblk1.Announce, tblk1.AnnounceRead, tblk1.RootLoadMessages, tblk1.RootMeasureMessages, tblk1.RootSaveMessages, func(r *tblk1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblk2", "Root", tblk2.RootReset, func() *tblk2.TableVocabulary {
		v := new(tblk2.TableVocabulary)
		v.Init(make([]tblk2.TableMessageEntry, max(4096, tblk2.TableMessageEntriesHere)))
		return v
	}, tblk2.AnnounceMeasure, tblk2.Announce, tblk2.AnnounceRead, tblk2.RootLoadMessages, tblk2.RootMeasureMessages, tblk2.RootSaveMessages, func(r *tblk2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("scalars", "SimState", scalardemo.SimStateReset, func() *scalardemo.TableVocabulary {
		v := new(scalardemo.TableVocabulary)
		v.Init(make([]scalardemo.TableMessageEntry, max(4096, scalardemo.TableMessageEntriesHere)))
		return v
	}, scalardemo.AnnounceMeasure, scalardemo.Announce, scalardemo.AnnounceRead, scalardemo.SimStateLoadMessages, scalardemo.SimStateMeasureMessages, scalardemo.SimStateSaveMessages, func(r *scalardemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	messageRow("tblscalars2", "SimState", tblscalars2.SimStateReset, func() *tblscalars2.TableVocabulary {
		v := new(tblscalars2.TableVocabulary)
		v.Init(make([]tblscalars2.TableMessageEntry, max(4096, tblscalars2.TableMessageEntriesHere)))
		return v
	}, tblscalars2.AnnounceMeasure, tblscalars2.Announce, tblscalars2.AnnounceRead, tblscalars2.SimStateLoadMessages, tblscalars2.SimStateMeasureMessages, tblscalars2.SimStateSaveMessages, func(r *tblscalars2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
}
