// THE CODEC TABLE: one row per (unit, root) the corpus names.
//
// Nothing here is an expectation — the rows only say which generated functions
// answer for which manifest key. The expectations all live in the DATA.
package main

import (
	"tblg1"
	"tblp2"
	"tblw1"
	"tblw2"

	"backenddemo"
	"blobdemo"
	"graphdemo"
	"listdemo"
	"mapdemo"
	"messagedemo"
	"scalardemo"
	"streamdemo"
	tblscalars2 "tblscalars2"
	"vocab9demo"
	"vocabdemo"

	"tbla1"
	"tbla2"
	"tblk1"
	"tblk2"
	"tblm1"
	"tblm2"
	"tblr1"
	"tblr2"

	"tabledemo"
	"tblp1"
	"tblp3"
	"tblv1"
	"tblv2"
)

// each unit's TableReport is its OWN type, so each gets a narrowing of four
// lines. A row that stopped copying a counter would be caught by the first
// case that counts it.
func snapDemo(r *tabledemo.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused || r.NonFiniteFloat != ""}
}

func snapV1(r *tblv1.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblv1.TableOpenRefused || r.NonFiniteFloat != ""}
}

func snapV2(r *tblv2.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblv2.TableOpenRefused || r.NonFiniteFloat != ""}
}

func snapP1(r *tblp1.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp1.TableOpenRefused || r.NonFiniteFloat != ""}
}

func snapP3(r *tblp3.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp3.TableOpenRefused || r.NonFiniteFloat != ""}
}

var codecTable = []codec{
	regionRow("tblp2", "Chain", tblp2.ChainLoadMeasure, tblp2.ChainLoadTolerant, tblp2.ChainMeasure, tblp2.ChainSave, func(text []byte, r *tblp2.TableReport) (*tblp2.Chain, []byte, bool) {
		var b tblp2.ChainBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblp2.ChainFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblp2.ChainToJSONMeasure, tblp2.ChainToJSON, tblp2.ChainCookMeasure, tblp2.ChainCookFrom, func(r *tblp2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp2.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("tblw1", "Fleet", tblw1.FleetLoadMeasure, tblw1.FleetLoadTolerant, tblw1.FleetMeasure, tblw1.FleetSave, func(text []byte, r *tblw1.TableReport) (*tblw1.Fleet, []byte, bool) {
		var b tblw1.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblw1.FleetFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblw1.FleetToJSONMeasure, tblw1.FleetToJSON, tblw1.FleetCookMeasure, tblw1.FleetCookFrom, func(r *tblw1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw1.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("tblw2", "Fleet", tblw2.FleetLoadMeasure, tblw2.FleetLoadTolerant, tblw2.FleetMeasure, tblw2.FleetSave, func(text []byte, r *tblw2.TableReport) (*tblw2.Fleet, []byte, bool) {
		var b tblw2.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblw2.FleetFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblw2.FleetToJSONMeasure, tblw2.FleetToJSON, tblw2.FleetCookMeasure, tblw2.FleetCookFrom, func(r *tblw2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw2.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("tblg1", "Guarded", tblg1.GuardedLoadMeasure, tblg1.GuardedLoadTolerant, tblg1.GuardedMeasure, tblg1.GuardedSave, func(text []byte, r *tblg1.TableReport) (*tblg1.Guarded, []byte, bool) {
		var b tblg1.GuardedBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblg1.GuardedFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblg1.GuardedToJSONMeasure, tblg1.GuardedToJSON, tblg1.GuardedCookMeasure, tblg1.GuardedCookFrom, func(r *tblg1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblg1.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	row("backenddemo", "LoginRequest", backenddemo.LoginRequestReset, backenddemo.LoginRequestLoadTolerant, backenddemo.LoginRequestMeasure, backenddemo.LoginRequestSave, backenddemo.LoginRequestFromJSONTolerant, backenddemo.LoginRequestToJSONMeasure, backenddemo.LoginRequestToJSON, backenddemo.LoginRequestCookMeasure, backenddemo.LoginRequestCookFrom, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("backenddemo", "MatchResult", backenddemo.MatchResultReset, backenddemo.MatchResultLoadTolerant, backenddemo.MatchResultMeasure, backenddemo.MatchResultSave, backenddemo.MatchResultFromJSONTolerant, backenddemo.MatchResultToJSONMeasure, backenddemo.MatchResultToJSON, backenddemo.MatchResultCookMeasure, backenddemo.MatchResultCookFrom, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("backenddemo", "StorePurchase", backenddemo.StorePurchaseReset, backenddemo.StorePurchaseLoadTolerant, backenddemo.StorePurchaseMeasure, backenddemo.StorePurchaseSave, backenddemo.StorePurchaseFromJSONTolerant, backenddemo.StorePurchaseToJSONMeasure, backenddemo.StorePurchaseToJSON, backenddemo.StorePurchaseCookMeasure, backenddemo.StorePurchaseCookFrom, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("backenddemo", "Envelope", backenddemo.EnvelopeReset, backenddemo.EnvelopeLoadTolerant, backenddemo.EnvelopeMeasure, backenddemo.EnvelopeSave, backenddemo.EnvelopeFromJSONTolerant, backenddemo.EnvelopeToJSONMeasure, backenddemo.EnvelopeToJSON, backenddemo.EnvelopeCookMeasure, backenddemo.EnvelopeCookFrom, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("vocabdemo", "Wide00", vocabdemo.Wide00Reset, vocabdemo.Wide00LoadTolerant, vocabdemo.Wide00Measure, vocabdemo.Wide00Save, vocabdemo.Wide00FromJSONTolerant, vocabdemo.Wide00ToJSONMeasure, vocabdemo.Wide00ToJSON, vocabdemo.Wide00CookMeasure, vocabdemo.Wide00CookFrom, func(r *vocabdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocabdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("vocabdemo", "Wide09", vocabdemo.Wide09Reset, vocabdemo.Wide09LoadTolerant, vocabdemo.Wide09Measure, vocabdemo.Wide09Save, vocabdemo.Wide09FromJSONTolerant, vocabdemo.Wide09ToJSONMeasure, vocabdemo.Wide09ToJSON, vocabdemo.Wide09CookMeasure, vocabdemo.Wide09CookFrom, func(r *vocabdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocabdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("vocab9demo", "Wide00", vocab9demo.Wide00Reset, vocab9demo.Wide00LoadTolerant, vocab9demo.Wide00Measure, vocab9demo.Wide00Save, vocab9demo.Wide00FromJSONTolerant, vocab9demo.Wide00ToJSONMeasure, vocab9demo.Wide00ToJSON, vocab9demo.Wide00CookMeasure, vocab9demo.Wide00CookFrom, func(r *vocab9demo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocab9demo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("vocab9demo", "Wide19", vocab9demo.Wide19Reset, vocab9demo.Wide19LoadTolerant, vocab9demo.Wide19Measure, vocab9demo.Wide19Save, vocab9demo.Wide19FromJSONTolerant, vocab9demo.Wide19ToJSONMeasure, vocab9demo.Wide19ToJSON, vocab9demo.Wide19CookMeasure, vocab9demo.Wide19CookFrom, func(r *vocab9demo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocab9demo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Cells", mapdemo.CellsLoadMeasure, mapdemo.CellsLoadTolerant, mapdemo.CellsMeasure, mapdemo.CellsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Cells, []byte, bool) {
		var b mapdemo.CellsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.CellsFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.CellsToJSONMeasure, mapdemo.CellsToJSON, mapdemo.CellsCookMeasure, mapdemo.CellsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Chunks", mapdemo.ChunksLoadMeasure, mapdemo.ChunksLoadTolerant, mapdemo.ChunksMeasure, mapdemo.ChunksSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Chunks, []byte, bool) {
		var b mapdemo.ChunksBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.ChunksFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.ChunksToJSONMeasure, mapdemo.ChunksToJSON, mapdemo.ChunksCookMeasure, mapdemo.ChunksCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Crews", mapdemo.CrewsLoadMeasure, mapdemo.CrewsLoadTolerant, mapdemo.CrewsMeasure, mapdemo.CrewsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Crews, []byte, bool) {
		var b mapdemo.CrewsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.CrewsFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.CrewsToJSONMeasure, mapdemo.CrewsToJSON, mapdemo.CrewsCookMeasure, mapdemo.CrewsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Depth", mapdemo.DepthLoadMeasure, mapdemo.DepthLoadTolerant, mapdemo.DepthMeasure, mapdemo.DepthSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Depth, []byte, bool) {
		var b mapdemo.DepthBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.DepthFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.DepthToJSONMeasure, mapdemo.DepthToJSON, mapdemo.DepthCookMeasure, mapdemo.DepthCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Docs", mapdemo.DocsLoadMeasure, mapdemo.DocsLoadTolerant, mapdemo.DocsMeasure, mapdemo.DocsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Docs, []byte, bool) {
		var b mapdemo.DocsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.DocsFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.DocsToJSONMeasure, mapdemo.DocsToJSON, mapdemo.DocsCookMeasure, mapdemo.DocsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}, mapdemo.DocsMeasureReason),
	regionRow("mapdemo", "Fleet", mapdemo.FleetLoadMeasure, mapdemo.FleetLoadTolerant, mapdemo.FleetMeasure, mapdemo.FleetSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Fleet, []byte, bool) {
		var b mapdemo.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.FleetFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.FleetToJSONMeasure, mapdemo.FleetToJSON, mapdemo.FleetCookMeasure, mapdemo.FleetCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Pairs", mapdemo.PairsLoadMeasure, mapdemo.PairsLoadTolerant, mapdemo.PairsMeasure, mapdemo.PairsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Pairs, []byte, bool) {
		var b mapdemo.PairsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.PairsFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.PairsToJSONMeasure, mapdemo.PairsToJSON, mapdemo.PairsCookMeasure, mapdemo.PairsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Slots", mapdemo.SlotsLoadMeasure, mapdemo.SlotsLoadTolerant, mapdemo.SlotsMeasure, mapdemo.SlotsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Slots, []byte, bool) {
		var b mapdemo.SlotsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.SlotsFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.SlotsToJSONMeasure, mapdemo.SlotsToJSON, mapdemo.SlotsCookMeasure, mapdemo.SlotsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Spans", mapdemo.SpansLoadMeasure, mapdemo.SpansLoadTolerant, mapdemo.SpansMeasure, mapdemo.SpansSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Spans, []byte, bool) {
		var b mapdemo.SpansBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.SpansFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.SpansToJSONMeasure, mapdemo.SpansToJSON, mapdemo.SpansCookMeasure, mapdemo.SpansCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Text", mapdemo.TextLoadMeasure, mapdemo.TextLoadTolerant, mapdemo.TextMeasure, mapdemo.TextSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Text, []byte, bool) {
		var b mapdemo.TextBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.TextFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.TextToJSONMeasure, mapdemo.TextToJSON, mapdemo.TextCookMeasure, mapdemo.TextCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Trails", mapdemo.TrailsLoadMeasure, mapdemo.TrailsLoadTolerant, mapdemo.TrailsMeasure, mapdemo.TrailsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Trails, []byte, bool) {
		var b mapdemo.TrailsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.TrailsFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.TrailsToJSONMeasure, mapdemo.TrailsToJSON, mapdemo.TrailsCookMeasure, mapdemo.TrailsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Runs", mapdemo.RunsLoadMeasure, mapdemo.RunsLoadTolerant, mapdemo.RunsMeasure, mapdemo.RunsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Runs, []byte, bool) {
		var b mapdemo.RunsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.RunsFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.RunsToJSONMeasure, mapdemo.RunsToJSON, mapdemo.RunsCookMeasure, mapdemo.RunsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "Row", mapdemo.RowLoadMeasure, mapdemo.RowLoadTolerant, mapdemo.RowMeasure, mapdemo.RowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Row, []byte, bool) {
		var b mapdemo.RowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.RowFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.RowToJSONMeasure, mapdemo.RowToJSON, mapdemo.RowCookMeasure, mapdemo.RowCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "WideRow", mapdemo.WideRowLoadMeasure, mapdemo.WideRowLoadTolerant, mapdemo.WideRowMeasure, mapdemo.WideRowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.WideRow, []byte, bool) {
		var b mapdemo.WideRowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.WideRowFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.WideRowToJSONMeasure, mapdemo.WideRowToJSON, mapdemo.WideRowCookMeasure, mapdemo.WideRowCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("mapdemo", "EdgeRow", mapdemo.EdgeRowLoadMeasure, mapdemo.EdgeRowLoadTolerant, mapdemo.EdgeRowMeasure, mapdemo.EdgeRowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.EdgeRow, []byte, bool) {
		var b mapdemo.EdgeRowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.EdgeRowFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.EdgeRowToJSONMeasure, mapdemo.EdgeRowToJSON, mapdemo.EdgeRowCookMeasure, mapdemo.EdgeRowCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("listdemo", "Sheet", listdemo.SheetLoadMeasure, listdemo.SheetLoadTolerant, listdemo.SheetMeasure, listdemo.SheetSave, func(text []byte, r *listdemo.TableReport) (*listdemo.Sheet, []byte, bool) {
		var b listdemo.SheetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := listdemo.SheetFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, listdemo.SheetToJSONMeasure, listdemo.SheetToJSON, listdemo.SheetCookMeasure, listdemo.SheetCookFrom, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("listdemo", "Army", listdemo.ArmyLoadMeasure, listdemo.ArmyLoadTolerant, listdemo.ArmyMeasure, listdemo.ArmySave, func(text []byte, r *listdemo.TableReport) (*listdemo.Army, []byte, bool) {
		var b listdemo.ArmyBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := listdemo.ArmyFromJSONTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, listdemo.ArmyToJSONMeasure, listdemo.ArmyToJSON, listdemo.ArmyCookMeasure, listdemo.ArmyCookFrom, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	regionRow("listdemo", "Save", listdemo.SaveLoadMeasure, listdemo.SaveLoadTolerant, listdemo.SaveMeasure, listdemo.SaveSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Save, []byte, bool) {
			var b listdemo.SaveBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.SaveFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.SaveToJSONMeasure, listdemo.SaveToJSON, listdemo.SaveCookMeasure, listdemo.SaveCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),
	regionRow("listdemo", "Mixed", listdemo.MixedLoadMeasure, listdemo.MixedLoadTolerant, listdemo.MixedMeasure, listdemo.MixedSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Mixed, []byte, bool) {
			var b listdemo.MixedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.MixedFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.MixedToJSONMeasure, listdemo.MixedToJSON, listdemo.MixedCookMeasure, listdemo.MixedCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),
	regionRow("listdemo", "Bytes", listdemo.BytesLoadMeasure, listdemo.BytesLoadTolerant, listdemo.BytesMeasure, listdemo.BytesSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Bytes, []byte, bool) {
			var b listdemo.BytesBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.BytesFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.BytesToJSONMeasure, listdemo.BytesToJSON, listdemo.BytesCookMeasure, listdemo.BytesCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),
	regionRow("listdemo", "Ints", listdemo.IntsLoadMeasure, listdemo.IntsLoadTolerant, listdemo.IntsMeasure, listdemo.IntsSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Ints, []byte, bool) {
			var b listdemo.IntsBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.IntsFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.IntsToJSONMeasure, listdemo.IntsToJSON, listdemo.IntsCookMeasure, listdemo.IntsCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),
	regionRow("listdemo", "Floats", listdemo.FloatsLoadMeasure, listdemo.FloatsLoadTolerant, listdemo.FloatsMeasure, listdemo.FloatsSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Floats, []byte, bool) {
			var b listdemo.FloatsBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.FloatsFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.FloatsToJSONMeasure, listdemo.FloatsToJSON, listdemo.FloatsCookMeasure, listdemo.FloatsCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),
	regionRow("listdemo", "Album", listdemo.AlbumLoadMeasure, listdemo.AlbumLoadTolerant, listdemo.AlbumMeasure, listdemo.AlbumSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Album, []byte, bool) {
			var b listdemo.AlbumBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.AlbumFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.AlbumToJSONMeasure, listdemo.AlbumToJSON, listdemo.AlbumCookMeasure, listdemo.AlbumCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),
	regionRow("listdemo", "Unbounded", listdemo.UnboundedLoadMeasure, listdemo.UnboundedLoadTolerant, listdemo.UnboundedMeasure, listdemo.UnboundedSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Unbounded, []byte, bool) {
			var b listdemo.UnboundedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.UnboundedFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.UnboundedToJSONMeasure, listdemo.UnboundedToJSON, listdemo.UnboundedCookMeasure, listdemo.UnboundedCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),

	regionRow("streamdemo", "Feed", streamdemo.FeedLoadMeasure, streamdemo.FeedLoadTolerant, streamdemo.FeedMeasure, streamdemo.FeedSave,
		func(text []byte, r *streamdemo.TableReport) (*streamdemo.Feed, []byte, bool) {
			var b streamdemo.FeedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := streamdemo.FeedFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		streamdemo.FeedToJSONMeasure, streamdemo.FeedToJSON, streamdemo.FeedCookMeasure, streamdemo.FeedCookFrom, func(r *streamdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),

	regionRow("streamdemo", "Chunk", streamdemo.ChunkLoadMeasure, streamdemo.ChunkLoadTolerant, streamdemo.ChunkMeasure, streamdemo.ChunkSave,
		func(text []byte, r *streamdemo.TableReport) (*streamdemo.Chunk, []byte, bool) {
			var b streamdemo.ChunkBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := streamdemo.ChunkFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		streamdemo.ChunkToJSONMeasure, streamdemo.ChunkToJSON, streamdemo.ChunkCookMeasure, streamdemo.ChunkCookFrom, func(r *streamdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),

	regionRow("blobdemo", "Catalog", blobdemo.CatalogLoadMeasure, blobdemo.CatalogLoadTolerant, blobdemo.CatalogMeasure, blobdemo.CatalogSave,
		func(text []byte, r *blobdemo.TableReport) (*blobdemo.Catalog, []byte, bool) {
			var b blobdemo.CatalogBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := blobdemo.CatalogFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		blobdemo.CatalogToJSONMeasure, blobdemo.CatalogToJSON, blobdemo.CatalogCookMeasure, blobdemo.CatalogCookFrom, func(r *blobdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}, blobdemo.CatalogMeasureReason),

	regionRow("blobdemo", "Asset", blobdemo.AssetLoadMeasure, blobdemo.AssetLoadTolerant, blobdemo.AssetMeasure, blobdemo.AssetSave,
		func(text []byte, r *blobdemo.TableReport) (*blobdemo.Asset, []byte, bool) {
			var b blobdemo.AssetBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := blobdemo.AssetFromJSONTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		blobdemo.AssetToJSONMeasure, blobdemo.AssetToJSON, blobdemo.AssetCookMeasure, blobdemo.AssetCookFrom, func(r *blobdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused || r.NonFiniteFloat != ""}
		}),

	regionRow("graphdemo", "Scene", graphdemo.SceneLoadMeasure, graphdemo.SceneLoadTolerant, graphdemo.SceneMeasure, graphdemo.SceneSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Scene, []byte, bool) {
		var b graphdemo.SceneBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.SceneFromJSONTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.SceneToJSONMeasure, graphdemo.SceneToJSON, graphdemo.SceneCookMeasure, graphdemo.SceneCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	regionRow("graphdemo", "ListNode", graphdemo.ListNodeLoadMeasure, graphdemo.ListNodeLoadTolerant, graphdemo.ListNodeMeasure, graphdemo.ListNodeSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.ListNode, []byte, bool) {
		var b graphdemo.ListNodeBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.ListNodeFromJSONTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.ListNodeToJSONMeasure, graphdemo.ListNodeToJSON, graphdemo.ListNodeCookMeasure, graphdemo.ListNodeCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	regionRow("graphdemo", "TreeNode", graphdemo.TreeNodeLoadMeasure, graphdemo.TreeNodeLoadTolerant, graphdemo.TreeNodeMeasure, graphdemo.TreeNodeSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.TreeNode, []byte, bool) {
		var b graphdemo.TreeNodeBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.TreeNodeFromJSONTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.TreeNodeToJSONMeasure, graphdemo.TreeNodeToJSON, graphdemo.TreeNodeCookMeasure, graphdemo.TreeNodeCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	regionRow("graphdemo", "Layer", graphdemo.LayerLoadMeasure, graphdemo.LayerLoadTolerant, graphdemo.LayerMeasure, graphdemo.LayerSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Layer, []byte, bool) {
		var b graphdemo.LayerBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.LayerFromJSONTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.LayerToJSONMeasure, graphdemo.LayerToJSON, graphdemo.LayerCookMeasure, graphdemo.LayerCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	regionRow("graphdemo", "Depot", graphdemo.DepotLoadMeasure, graphdemo.DepotLoadTolerant, graphdemo.DepotMeasure, graphdemo.DepotSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Depot, []byte, bool) {
		var b graphdemo.DepotBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.DepotFromJSONTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.DepotToJSONMeasure, graphdemo.DepotToJSON, graphdemo.DepotCookMeasure, graphdemo.DepotCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	regionRow("graphdemo", "Album", graphdemo.AlbumLoadMeasure, graphdemo.AlbumLoadTolerant, graphdemo.AlbumMeasure, graphdemo.AlbumSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Album, []byte, bool) {
		var b graphdemo.AlbumBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.AlbumFromJSONTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.AlbumToJSONMeasure, graphdemo.AlbumToJSON, graphdemo.AlbumCookMeasure, graphdemo.AlbumCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	regionRow("graphdemo", "Marker", graphdemo.MarkerLoadMeasure, graphdemo.MarkerLoadTolerant, graphdemo.MarkerMeasure, graphdemo.MarkerSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Marker, []byte, bool) {
		var b graphdemo.MarkerBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.MarkerFromJSONTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.MarkerToJSONMeasure, graphdemo.MarkerToJSON, graphdemo.MarkerCookMeasure, graphdemo.MarkerCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	row("messagedemo", "ToolMessage", messagedemo.ToolMessageReset, messagedemo.ToolMessageLoadTolerant, messagedemo.ToolMessageMeasure, messagedemo.ToolMessageSave, messagedemo.ToolMessageFromJSONTolerant, messagedemo.ToolMessageToJSONMeasure, messagedemo.ToolMessageToJSON, messagedemo.ToolMessageCookMeasure, messagedemo.ToolMessageCookFrom, func(r *messagedemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == messagedemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	row("scalars", "SimState", scalardemo.SimStateReset, scalardemo.SimStateLoadTolerant, scalardemo.SimStateMeasure, scalardemo.SimStateSave, scalardemo.SimStateFromJSONTolerant, scalardemo.SimStateToJSONMeasure, scalardemo.SimStateToJSON, scalardemo.SimStateCookMeasure, scalardemo.SimStateCookFrom, func(r *scalardemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == scalardemo.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("tblscalars2", "SimState", tblscalars2.SimStateReset, tblscalars2.SimStateLoadTolerant, tblscalars2.SimStateMeasure, tblscalars2.SimStateSave, tblscalars2.SimStateFromJSONTolerant, tblscalars2.SimStateToJSONMeasure, tblscalars2.SimStateToJSON, tblscalars2.SimStateCookMeasure, tblscalars2.SimStateCookFrom, func(r *tblscalars2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblscalars2.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	row("tblm1", "Msg", tblm1.MsgReset, tblm1.MsgLoadTolerant, tblm1.MsgMeasure, tblm1.MsgSave, tblm1.MsgFromJSONTolerant, tblm1.MsgToJSONMeasure, tblm1.MsgToJSON, tblm1.MsgCookMeasure, tblm1.MsgCookFrom, func(r *tblm1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblm1.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("tblm2", "Msg", tblm2.MsgReset, tblm2.MsgLoadTolerant, tblm2.MsgMeasure, tblm2.MsgSave, tblm2.MsgFromJSONTolerant, tblm2.MsgToJSONMeasure, tblm2.MsgToJSON, tblm2.MsgCookMeasure, tblm2.MsgCookFrom, func(r *tblm2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblm2.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("tbla1", "Root", tbla1.RootReset, tbla1.RootLoadTolerant, tbla1.RootMeasure, tbla1.RootSave, tbla1.RootFromJSONTolerant, tbla1.RootToJSONMeasure, tbla1.RootToJSON, tbla1.RootCookMeasure, tbla1.RootCookFrom, func(r *tbla1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tbla1.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("tbla2", "Root", tbla2.RootReset, tbla2.RootLoadTolerant, tbla2.RootMeasure, tbla2.RootSave, tbla2.RootFromJSONTolerant, tbla2.RootToJSONMeasure, tbla2.RootToJSON, tbla2.RootCookMeasure, tbla2.RootCookFrom, func(r *tbla2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tbla2.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("tblk1", "Root", tblk1.RootReset, tblk1.RootLoadTolerant, tblk1.RootMeasure, tblk1.RootSave, tblk1.RootFromJSONTolerant, tblk1.RootToJSONMeasure, tblk1.RootToJSON, tblk1.RootCookMeasure, tblk1.RootCookFrom, func(r *tblk1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblk1.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("tblk2", "Root", tblk2.RootReset, tblk2.RootLoadTolerant, tblk2.RootMeasure, tblk2.RootSave, tblk2.RootFromJSONTolerant, tblk2.RootToJSONMeasure, tblk2.RootToJSON, tblk2.RootCookMeasure, tblk2.RootCookFrom, func(r *tblk2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblk2.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("tblr1", "Cfg", tblr1.CfgReset, tblr1.CfgLoadTolerant, tblr1.CfgMeasure, tblr1.CfgSave, tblr1.CfgFromJSONTolerant, tblr1.CfgToJSONMeasure, tblr1.CfgToJSON, tblr1.CfgCookMeasure, tblr1.CfgCookFrom, func(r *tblr1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblr1.TableOpenRefused || r.NonFiniteFloat != ""}
	}),
	row("tblr2", "Cfg", tblr2.CfgReset, tblr2.CfgLoadTolerant, tblr2.CfgMeasure, tblr2.CfgSave, tblr2.CfgFromJSONTolerant, tblr2.CfgToJSONMeasure, tblr2.CfgToJSON, tblr2.CfgCookMeasure, tblr2.CfgCookFrom, func(r *tblr2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblr2.TableOpenRefused || r.NonFiniteFloat != ""}
	}),

	row("tabledemo", "RootConfig", tabledemo.RootConfigReset, tabledemo.RootConfigLoadTolerant,
		tabledemo.RootConfigMeasure, tabledemo.RootConfigSave,
		tabledemo.RootConfigFromJSONTolerant, tabledemo.RootConfigToJSONMeasure, tabledemo.RootConfigToJSON, tabledemo.RootConfigCookMeasure, tabledemo.RootConfigCookFrom, snapDemo),
	row("tabledemo", "ProfileConfig", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigLoadTolerant,
		tabledemo.ProfileConfigMeasure, tabledemo.ProfileConfigSave,
		tabledemo.ProfileConfigFromJSONTolerant, tabledemo.ProfileConfigToJSONMeasure, tabledemo.ProfileConfigToJSON, tabledemo.ProfileConfigCookMeasure, tabledemo.ProfileConfigCookFrom, snapDemo),
	row("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigLoadTolerant,
		tabledemo.LoadoutConfigMeasure, tabledemo.LoadoutConfigSave,
		tabledemo.LoadoutConfigFromJSONTolerant, tabledemo.LoadoutConfigToJSONMeasure, tabledemo.LoadoutConfigToJSON, tabledemo.LoadoutConfigCookMeasure, tabledemo.LoadoutConfigCookFrom, snapDemo),
	row("tabledemo", "WideBlob", tabledemo.WideBlobReset, tabledemo.WideBlobLoadTolerant,
		tabledemo.WideBlobMeasure, tabledemo.WideBlobSave,
		tabledemo.WideBlobFromJSONTolerant, tabledemo.WideBlobToJSONMeasure, tabledemo.WideBlobToJSON, tabledemo.WideBlobCookMeasure, tabledemo.WideBlobCookFrom, snapDemo),
	row("tabledemo", "ArchiveConfig", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigLoadTolerant,
		tabledemo.ArchiveConfigMeasure, tabledemo.ArchiveConfigSave,
		tabledemo.ArchiveConfigFromJSONTolerant, tabledemo.ArchiveConfigToJSONMeasure, tabledemo.ArchiveConfigToJSON, tabledemo.ArchiveConfigCookMeasure, tabledemo.ArchiveConfigCookFrom, snapDemo),
	row("tabledemo", "PackConfig", tabledemo.PackConfigReset, tabledemo.PackConfigLoadTolerant,
		tabledemo.PackConfigMeasure, tabledemo.PackConfigSave,
		tabledemo.PackConfigFromJSONTolerant, tabledemo.PackConfigToJSONMeasure, tabledemo.PackConfigToJSON, tabledemo.PackConfigCookMeasure, tabledemo.PackConfigCookFrom, snapDemo),
	row("tabledemo", "KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoadTolerant,
		tabledemo.KeyedConfigMeasure, tabledemo.KeyedConfigSave,
		tabledemo.KeyedConfigFromJSONTolerant, tabledemo.KeyedConfigToJSONMeasure, tabledemo.KeyedConfigToJSON, tabledemo.KeyedConfigCookMeasure, tabledemo.KeyedConfigCookFrom, snapDemo),
	row("tblv1", "Cfg", tblv1.CfgReset, tblv1.CfgLoadTolerant, tblv1.CfgMeasure, tblv1.CfgSave,
		tblv1.CfgFromJSONTolerant, tblv1.CfgToJSONMeasure, tblv1.CfgToJSON, tblv1.CfgCookMeasure, tblv1.CfgCookFrom, snapV1),
	row("tblv2", "Cfg", tblv2.CfgReset, tblv2.CfgLoadTolerant, tblv2.CfgMeasure, tblv2.CfgSave,
		tblv2.CfgFromJSONTolerant, tblv2.CfgToJSONMeasure, tblv2.CfgToJSON, tblv2.CfgCookMeasure, tblv2.CfgCookFrom, snapV2),
	row("tblp1", "Chain", tblp1.ChainReset, tblp1.ChainLoadTolerant, tblp1.ChainMeasure, tblp1.ChainSave,
		tblp1.ChainFromJSONTolerant, tblp1.ChainToJSONMeasure, tblp1.ChainToJSON, tblp1.ChainCookMeasure, tblp1.ChainCookFrom, snapP1),
	row("tblp3", "Chain", tblp3.ChainReset, tblp3.ChainLoadTolerant, tblp3.ChainMeasure, tblp3.ChainSave,
		tblp3.ChainFromJSONTolerant, tblp3.ChainToJSONMeasure, tblp3.ChainToJSON, tblp3.ChainCookMeasure, tblp3.ChainCookFrom, snapP3),
}

// surfaces is what this backend implements. A surface not listed prints as
// ABSENT in the matrix, which is a missing FEATURE and not a failing test.
func surfaces() []string {
	return []string{"wire", "message", "report", "framing", "json-read", "json-write", "json-hostile", "cook-write", "retain", "retain-save", "cook", "cook-foreign", "block", "block-foreign", "block-dump", "forgery", "cook-forgery", "cook-reason", "block-reason"}
}
