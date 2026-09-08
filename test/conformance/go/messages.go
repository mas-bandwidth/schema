package main

import (
	"backenddemo"
	"fmt"
	"os"
	"vocab9demo"
	"vocabdemo"
)

type messageCodec struct {
	unit, root string
	run        func([]byte, []byte, bool) ([]byte, report, bool)
}

func messageRow[T, R, V any](unit, root string, reset func(*T), fresh func() *V, announceMeasure func() int64, announce func([]byte) int64, announceRead func(*V, []byte, *R) bool, load func([]T, *V, []byte, *R) (int64, bool), measure func([]*T) int64, save func([]*T, []byte, *R) int64, snap func(*R) report) messageCodec {
	var own *V
	values := make([]T, 256)
	pointers := make([]*T, 256)
	for i := range values {
		pointers[i] = &values[i]
	}
	return messageCodec{unit, root, func(announcement, wire []byte, fuzz bool) ([]byte, report, bool) {
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
			count = 1
			rep.malformed = rep.malformed || !ok && !rep.refused
		} else if !ok {
			return nil, rep, false
		}
		if count < 1 || count > 256 {
			return nil, rep, false
		}
		n := measure(pointers[:count])
		if n < 0 {
			return nil, rep, false
		}
		out := make([]byte, n)
		var ignored R
		if save(pointers[:count], out, &ignored) != n {
			return nil, rep, false
		}
		return out, rep, ok
	}}
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
			if err := spillAbsent(out, f[1]); err != nil {
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
		v.Init(make([]backenddemo.TableMessageEntry, backenddemo.TableMessageEntriesHere))
		return v
	}, backenddemo.AnnounceMeasure, backenddemo.Announce, backenddemo.AnnounceRead, backenddemo.LoginRequestLoadMessages, backenddemo.LoginRequestMeasureMessages, backenddemo.LoginRequestSaveMessages, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused}
	}),
	messageRow("backenddemo", "MatchResult", backenddemo.MatchResultReset, func() *backenddemo.TableVocabulary {
		v := new(backenddemo.TableVocabulary)
		v.Init(make([]backenddemo.TableMessageEntry, backenddemo.TableMessageEntriesHere))
		return v
	}, backenddemo.AnnounceMeasure, backenddemo.Announce, backenddemo.AnnounceRead, backenddemo.MatchResultLoadMessages, backenddemo.MatchResultMeasureMessages, backenddemo.MatchResultSaveMessages, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused}
	}),
	messageRow("backenddemo", "StorePurchase", backenddemo.StorePurchaseReset, func() *backenddemo.TableVocabulary {
		v := new(backenddemo.TableVocabulary)
		v.Init(make([]backenddemo.TableMessageEntry, backenddemo.TableMessageEntriesHere))
		return v
	}, backenddemo.AnnounceMeasure, backenddemo.Announce, backenddemo.AnnounceRead, backenddemo.StorePurchaseLoadMessages, backenddemo.StorePurchaseMeasureMessages, backenddemo.StorePurchaseSaveMessages, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused}
	}),
	messageRow("backenddemo", "Envelope", backenddemo.EnvelopeReset, func() *backenddemo.TableVocabulary {
		v := new(backenddemo.TableVocabulary)
		v.Init(make([]backenddemo.TableMessageEntry, backenddemo.TableMessageEntriesHere))
		return v
	}, backenddemo.AnnounceMeasure, backenddemo.Announce, backenddemo.AnnounceRead, backenddemo.EnvelopeLoadMessages, backenddemo.EnvelopeMeasureMessages, backenddemo.EnvelopeSaveMessages, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused}
	}),
	messageRow("vocabdemo", "Wide00", vocabdemo.Wide00Reset, func() *vocabdemo.TableVocabulary {
		v := new(vocabdemo.TableVocabulary)
		v.Init(make([]vocabdemo.TableMessageEntry, vocabdemo.TableMessageEntriesHere))
		return v
	}, vocabdemo.AnnounceMeasure, vocabdemo.Announce, vocabdemo.AnnounceRead, vocabdemo.Wide00LoadMessages, vocabdemo.Wide00MeasureMessages, vocabdemo.Wide00SaveMessages, func(r *vocabdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocabdemo.TableOpenRefused}
	}),
	messageRow("vocabdemo", "Wide09", vocabdemo.Wide09Reset, func() *vocabdemo.TableVocabulary {
		v := new(vocabdemo.TableVocabulary)
		v.Init(make([]vocabdemo.TableMessageEntry, vocabdemo.TableMessageEntriesHere))
		return v
	}, vocabdemo.AnnounceMeasure, vocabdemo.Announce, vocabdemo.AnnounceRead, vocabdemo.Wide09LoadMessages, vocabdemo.Wide09MeasureMessages, vocabdemo.Wide09SaveMessages, func(r *vocabdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocabdemo.TableOpenRefused}
	}),
	messageRow("vocab9demo", "Wide00", vocab9demo.Wide00Reset, func() *vocab9demo.TableVocabulary {
		v := new(vocab9demo.TableVocabulary)
		v.Init(make([]vocab9demo.TableMessageEntry, vocab9demo.TableMessageEntriesHere))
		return v
	}, vocab9demo.AnnounceMeasure, vocab9demo.Announce, vocab9demo.AnnounceRead, vocab9demo.Wide00LoadMessages, vocab9demo.Wide00MeasureMessages, vocab9demo.Wide00SaveMessages, func(r *vocab9demo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocab9demo.TableOpenRefused}
	}),
	messageRow("vocab9demo", "Wide19", vocab9demo.Wide19Reset, func() *vocab9demo.TableVocabulary {
		v := new(vocab9demo.TableVocabulary)
		v.Init(make([]vocab9demo.TableMessageEntry, vocab9demo.TableMessageEntriesHere))
		return v
	}, vocab9demo.AnnounceMeasure, vocab9demo.Announce, vocab9demo.AnnounceRead, vocab9demo.Wide19LoadMessages, vocab9demo.Wide19MeasureMessages, vocab9demo.Wide19SaveMessages, func(r *vocab9demo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocab9demo.TableOpenRefused}
	}),
}
