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
	widedemo "widedemo"

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
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused}
}

func snapV1(r *tblv1.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblv1.TableOpenRefused}
}

func snapV2(r *tblv2.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblv2.TableOpenRefused}
}

func snapP1(r *tblp1.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp1.TableOpenRefused}
}

func snapP3(r *tblp3.TableReport) report {
	return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp3.TableOpenRefused}
}

var codecTable = []codec{
	regionRow("tblp2", "Chain", tblp2.ChainLoadMeasure, tblp2.ChainLoadTolerant, tblp2.ChainMeasure, tblp2.ChainSave, func(text []byte, r *tblp2.TableReport) (*tblp2.Chain, []byte, bool) {
		var b tblp2.ChainBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblp2.ChainFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblp2.ChainToJsonMeasure, tblp2.ChainToJson, tblp2.ChainCookMeasure, tblp2.ChainCookFrom, func(r *tblp2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp2.TableOpenRefused}
	}),
	regionRow("tblw1", "Fleet", tblw1.FleetLoadMeasure, tblw1.FleetLoadTolerant, tblw1.FleetMeasure, tblw1.FleetSave, func(text []byte, r *tblw1.TableReport) (*tblw1.Fleet, []byte, bool) {
		var b tblw1.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblw1.FleetFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblw1.FleetToJsonMeasure, tblw1.FleetToJson, tblw1.FleetCookMeasure, tblw1.FleetCookFrom, func(r *tblw1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw1.TableOpenRefused}
	}),
	regionRow("tblw2", "Fleet", tblw2.FleetLoadMeasure, tblw2.FleetLoadTolerant, tblw2.FleetMeasure, tblw2.FleetSave, func(text []byte, r *tblw2.TableReport) (*tblw2.Fleet, []byte, bool) {
		var b tblw2.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblw2.FleetFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblw2.FleetToJsonMeasure, tblw2.FleetToJson, tblw2.FleetCookMeasure, tblw2.FleetCookFrom, func(r *tblw2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw2.TableOpenRefused}
	}),
	regionRow("tblg1", "Guarded", tblg1.GuardedLoadMeasure, tblg1.GuardedLoadTolerant, tblg1.GuardedMeasure, tblg1.GuardedSave, func(text []byte, r *tblg1.TableReport) (*tblg1.Guarded, []byte, bool) {
		var b tblg1.GuardedBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblg1.GuardedFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblg1.GuardedToJsonMeasure, tblg1.GuardedToJson, tblg1.GuardedCookMeasure, tblg1.GuardedCookFrom, func(r *tblg1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblg1.TableOpenRefused}
	}),

	row("backenddemo", "LoginRequest", backenddemo.LoginRequestReset, backenddemo.LoginRequestLoadTolerant, backenddemo.LoginRequestMeasure, backenddemo.LoginRequestSave, backenddemo.LoginRequestFromJsonTolerant, backenddemo.LoginRequestToJsonMeasure, backenddemo.LoginRequestToJson, backenddemo.LoginRequestCookMeasure, backenddemo.LoginRequestCookFrom, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused}
	}),
	row("backenddemo", "MatchResult", backenddemo.MatchResultReset, backenddemo.MatchResultLoadTolerant, backenddemo.MatchResultMeasure, backenddemo.MatchResultSave, backenddemo.MatchResultFromJsonTolerant, backenddemo.MatchResultToJsonMeasure, backenddemo.MatchResultToJson, backenddemo.MatchResultCookMeasure, backenddemo.MatchResultCookFrom, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused}
	}),
	row("backenddemo", "StorePurchase", backenddemo.StorePurchaseReset, backenddemo.StorePurchaseLoadTolerant, backenddemo.StorePurchaseMeasure, backenddemo.StorePurchaseSave, backenddemo.StorePurchaseFromJsonTolerant, backenddemo.StorePurchaseToJsonMeasure, backenddemo.StorePurchaseToJson, backenddemo.StorePurchaseCookMeasure, backenddemo.StorePurchaseCookFrom, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused}
	}),
	row("backenddemo", "Envelope", backenddemo.EnvelopeReset, backenddemo.EnvelopeLoadTolerant, backenddemo.EnvelopeMeasure, backenddemo.EnvelopeSave, backenddemo.EnvelopeFromJsonTolerant, backenddemo.EnvelopeToJsonMeasure, backenddemo.EnvelopeToJson, backenddemo.EnvelopeCookMeasure, backenddemo.EnvelopeCookFrom, func(r *backenddemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == backenddemo.TableOpenRefused}
	}),
	row("vocabdemo", "Wide00", vocabdemo.Wide00Reset, vocabdemo.Wide00LoadTolerant, vocabdemo.Wide00Measure, vocabdemo.Wide00Save, vocabdemo.Wide00FromJsonTolerant, vocabdemo.Wide00ToJsonMeasure, vocabdemo.Wide00ToJson, vocabdemo.Wide00CookMeasure, vocabdemo.Wide00CookFrom, func(r *vocabdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocabdemo.TableOpenRefused}
	}),
	row("vocabdemo", "Wide09", vocabdemo.Wide09Reset, vocabdemo.Wide09LoadTolerant, vocabdemo.Wide09Measure, vocabdemo.Wide09Save, vocabdemo.Wide09FromJsonTolerant, vocabdemo.Wide09ToJsonMeasure, vocabdemo.Wide09ToJson, vocabdemo.Wide09CookMeasure, vocabdemo.Wide09CookFrom, func(r *vocabdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocabdemo.TableOpenRefused}
	}),
	row("vocab9demo", "Wide00", vocab9demo.Wide00Reset, vocab9demo.Wide00LoadTolerant, vocab9demo.Wide00Measure, vocab9demo.Wide00Save, vocab9demo.Wide00FromJsonTolerant, vocab9demo.Wide00ToJsonMeasure, vocab9demo.Wide00ToJson, vocab9demo.Wide00CookMeasure, vocab9demo.Wide00CookFrom, func(r *vocab9demo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocab9demo.TableOpenRefused}
	}),
	row("vocab9demo", "Wide19", vocab9demo.Wide19Reset, vocab9demo.Wide19LoadTolerant, vocab9demo.Wide19Measure, vocab9demo.Wide19Save, vocab9demo.Wide19FromJsonTolerant, vocab9demo.Wide19ToJsonMeasure, vocab9demo.Wide19ToJson, vocab9demo.Wide19CookMeasure, vocab9demo.Wide19CookFrom, func(r *vocab9demo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == vocab9demo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Cells", mapdemo.CellsLoadMeasure, mapdemo.CellsLoadTolerant, mapdemo.CellsMeasure, mapdemo.CellsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Cells, []byte, bool) {
		var b mapdemo.CellsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.CellsFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.CellsToJsonMeasure, mapdemo.CellsToJson, mapdemo.CellsCookMeasure, mapdemo.CellsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Chunks", mapdemo.ChunksLoadMeasure, mapdemo.ChunksLoadTolerant, mapdemo.ChunksMeasure, mapdemo.ChunksSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Chunks, []byte, bool) {
		var b mapdemo.ChunksBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.ChunksFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.ChunksToJsonMeasure, mapdemo.ChunksToJson, mapdemo.ChunksCookMeasure, mapdemo.ChunksCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Crews", mapdemo.CrewsLoadMeasure, mapdemo.CrewsLoadTolerant, mapdemo.CrewsMeasure, mapdemo.CrewsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Crews, []byte, bool) {
		var b mapdemo.CrewsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.CrewsFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.CrewsToJsonMeasure, mapdemo.CrewsToJson, mapdemo.CrewsCookMeasure, mapdemo.CrewsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Depth", mapdemo.DepthLoadMeasure, mapdemo.DepthLoadTolerant, mapdemo.DepthMeasure, mapdemo.DepthSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Depth, []byte, bool) {
		var b mapdemo.DepthBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.DepthFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.DepthToJsonMeasure, mapdemo.DepthToJson, mapdemo.DepthCookMeasure, mapdemo.DepthCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Docs", mapdemo.DocsLoadMeasure, mapdemo.DocsLoadTolerant, mapdemo.DocsMeasure, mapdemo.DocsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Docs, []byte, bool) {
		var b mapdemo.DocsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.DocsFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.DocsToJsonMeasure, mapdemo.DocsToJson, mapdemo.DocsCookMeasure, mapdemo.DocsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Fleet", mapdemo.FleetLoadMeasure, mapdemo.FleetLoadTolerant, mapdemo.FleetMeasure, mapdemo.FleetSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Fleet, []byte, bool) {
		var b mapdemo.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.FleetFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.FleetToJsonMeasure, mapdemo.FleetToJson, mapdemo.FleetCookMeasure, mapdemo.FleetCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Pairs", mapdemo.PairsLoadMeasure, mapdemo.PairsLoadTolerant, mapdemo.PairsMeasure, mapdemo.PairsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Pairs, []byte, bool) {
		var b mapdemo.PairsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.PairsFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.PairsToJsonMeasure, mapdemo.PairsToJson, mapdemo.PairsCookMeasure, mapdemo.PairsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Slots", mapdemo.SlotsLoadMeasure, mapdemo.SlotsLoadTolerant, mapdemo.SlotsMeasure, mapdemo.SlotsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Slots, []byte, bool) {
		var b mapdemo.SlotsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.SlotsFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.SlotsToJsonMeasure, mapdemo.SlotsToJson, mapdemo.SlotsCookMeasure, mapdemo.SlotsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Spans", mapdemo.SpansLoadMeasure, mapdemo.SpansLoadTolerant, mapdemo.SpansMeasure, mapdemo.SpansSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Spans, []byte, bool) {
		var b mapdemo.SpansBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.SpansFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.SpansToJsonMeasure, mapdemo.SpansToJson, mapdemo.SpansCookMeasure, mapdemo.SpansCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Text", mapdemo.TextLoadMeasure, mapdemo.TextLoadTolerant, mapdemo.TextMeasure, mapdemo.TextSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Text, []byte, bool) {
		var b mapdemo.TextBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.TextFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.TextToJsonMeasure, mapdemo.TextToJson, mapdemo.TextCookMeasure, mapdemo.TextCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Trails", mapdemo.TrailsLoadMeasure, mapdemo.TrailsLoadTolerant, mapdemo.TrailsMeasure, mapdemo.TrailsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Trails, []byte, bool) {
		var b mapdemo.TrailsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.TrailsFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.TrailsToJsonMeasure, mapdemo.TrailsToJson, mapdemo.TrailsCookMeasure, mapdemo.TrailsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Runs", mapdemo.RunsLoadMeasure, mapdemo.RunsLoadTolerant, mapdemo.RunsMeasure, mapdemo.RunsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Runs, []byte, bool) {
		var b mapdemo.RunsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.RunsFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.RunsToJsonMeasure, mapdemo.RunsToJson, mapdemo.RunsCookMeasure, mapdemo.RunsCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "Row", mapdemo.RowLoadMeasure, mapdemo.RowLoadTolerant, mapdemo.RowMeasure, mapdemo.RowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Row, []byte, bool) {
		var b mapdemo.RowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.RowFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.RowToJsonMeasure, mapdemo.RowToJson, mapdemo.RowCookMeasure, mapdemo.RowCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "WideRow", mapdemo.WideRowLoadMeasure, mapdemo.WideRowLoadTolerant, mapdemo.WideRowMeasure, mapdemo.WideRowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.WideRow, []byte, bool) {
		var b mapdemo.WideRowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.WideRowFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.WideRowToJsonMeasure, mapdemo.WideRowToJson, mapdemo.WideRowCookMeasure, mapdemo.WideRowCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("mapdemo", "EdgeRow", mapdemo.EdgeRowLoadMeasure, mapdemo.EdgeRowLoadTolerant, mapdemo.EdgeRowMeasure, mapdemo.EdgeRowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.EdgeRow, []byte, bool) {
		var b mapdemo.EdgeRowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.EdgeRowFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.EdgeRowToJsonMeasure, mapdemo.EdgeRowToJson, mapdemo.EdgeRowCookMeasure, mapdemo.EdgeRowCookFrom, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	regionRow("listdemo", "Sheet", listdemo.SheetLoadMeasure, listdemo.SheetLoadTolerant, listdemo.SheetMeasure, listdemo.SheetSave, func(text []byte, r *listdemo.TableReport) (*listdemo.Sheet, []byte, bool) {
		var b listdemo.SheetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := listdemo.SheetFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, listdemo.SheetToJsonMeasure, listdemo.SheetToJson, listdemo.SheetCookMeasure, listdemo.SheetCookFrom, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	regionRow("listdemo", "Army", listdemo.ArmyLoadMeasure, listdemo.ArmyLoadTolerant, listdemo.ArmyMeasure, listdemo.ArmySave, func(text []byte, r *listdemo.TableReport) (*listdemo.Army, []byte, bool) {
		var b listdemo.ArmyBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := listdemo.ArmyFromJsonTolerant(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, listdemo.ArmyToJsonMeasure, listdemo.ArmyToJson, listdemo.ArmyCookMeasure, listdemo.ArmyCookFrom, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	regionRow("listdemo", "Save", listdemo.SaveLoadMeasure, listdemo.SaveLoadTolerant, listdemo.SaveMeasure, listdemo.SaveSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Save, []byte, bool) {
			var b listdemo.SaveBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.SaveFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.SaveToJsonMeasure, listdemo.SaveToJson, listdemo.SaveCookMeasure, listdemo.SaveCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
	regionRow("listdemo", "Mixed", listdemo.MixedLoadMeasure, listdemo.MixedLoadTolerant, listdemo.MixedMeasure, listdemo.MixedSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Mixed, []byte, bool) {
			var b listdemo.MixedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.MixedFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.MixedToJsonMeasure, listdemo.MixedToJson, listdemo.MixedCookMeasure, listdemo.MixedCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
	regionRow("listdemo", "Bytes", listdemo.BytesLoadMeasure, listdemo.BytesLoadTolerant, listdemo.BytesMeasure, listdemo.BytesSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Bytes, []byte, bool) {
			var b listdemo.BytesBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.BytesFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.BytesToJsonMeasure, listdemo.BytesToJson, listdemo.BytesCookMeasure, listdemo.BytesCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
	regionRow("listdemo", "Ints", listdemo.IntsLoadMeasure, listdemo.IntsLoadTolerant, listdemo.IntsMeasure, listdemo.IntsSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Ints, []byte, bool) {
			var b listdemo.IntsBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.IntsFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.IntsToJsonMeasure, listdemo.IntsToJson, listdemo.IntsCookMeasure, listdemo.IntsCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
	regionRow("listdemo", "Floats", listdemo.FloatsLoadMeasure, listdemo.FloatsLoadTolerant, listdemo.FloatsMeasure, listdemo.FloatsSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Floats, []byte, bool) {
			var b listdemo.FloatsBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.FloatsFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.FloatsToJsonMeasure, listdemo.FloatsToJson, listdemo.FloatsCookMeasure, listdemo.FloatsCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
	regionRow("listdemo", "Album", listdemo.AlbumLoadMeasure, listdemo.AlbumLoadTolerant, listdemo.AlbumMeasure, listdemo.AlbumSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Album, []byte, bool) {
			var b listdemo.AlbumBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.AlbumFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.AlbumToJsonMeasure, listdemo.AlbumToJson, listdemo.AlbumCookMeasure, listdemo.AlbumCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
	regionRow("listdemo", "Unbounded", listdemo.UnboundedLoadMeasure, listdemo.UnboundedLoadTolerant, listdemo.UnboundedMeasure, listdemo.UnboundedSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Unbounded, []byte, bool) {
			var b listdemo.UnboundedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.UnboundedFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.UnboundedToJsonMeasure, listdemo.UnboundedToJson, listdemo.UnboundedCookMeasure, listdemo.UnboundedCookFrom, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),

	regionRow("streamdemo", "Feed", streamdemo.FeedLoadMeasure, streamdemo.FeedLoadTolerant, streamdemo.FeedMeasure, streamdemo.FeedSave,
		func(text []byte, r *streamdemo.TableReport) (*streamdemo.Feed, []byte, bool) {
			var b streamdemo.FeedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := streamdemo.FeedFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		streamdemo.FeedToJsonMeasure, streamdemo.FeedToJson, streamdemo.FeedCookMeasure, streamdemo.FeedCookFrom, func(r *streamdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
		}),

	regionRow("streamdemo", "Chunk", streamdemo.ChunkLoadMeasure, streamdemo.ChunkLoadTolerant, streamdemo.ChunkMeasure, streamdemo.ChunkSave,
		func(text []byte, r *streamdemo.TableReport) (*streamdemo.Chunk, []byte, bool) {
			var b streamdemo.ChunkBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := streamdemo.ChunkFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		streamdemo.ChunkToJsonMeasure, streamdemo.ChunkToJson, streamdemo.ChunkCookMeasure, streamdemo.ChunkCookFrom, func(r *streamdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
		}),

	regionRow("blobdemo", "Catalog", blobdemo.CatalogLoadMeasure, blobdemo.CatalogLoadTolerant, blobdemo.CatalogMeasure, blobdemo.CatalogSave,
		func(text []byte, r *blobdemo.TableReport) (*blobdemo.Catalog, []byte, bool) {
			var b blobdemo.CatalogBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := blobdemo.CatalogFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		blobdemo.CatalogToJsonMeasure, blobdemo.CatalogToJson, blobdemo.CatalogCookMeasure, blobdemo.CatalogCookFrom, func(r *blobdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
		}),

	regionRow("blobdemo", "Asset", blobdemo.AssetLoadMeasure, blobdemo.AssetLoadTolerant, blobdemo.AssetMeasure, blobdemo.AssetSave,
		func(text []byte, r *blobdemo.TableReport) (*blobdemo.Asset, []byte, bool) {
			var b blobdemo.AssetBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := blobdemo.AssetFromJsonTolerant(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		blobdemo.AssetToJsonMeasure, blobdemo.AssetToJson, blobdemo.AssetCookMeasure, blobdemo.AssetCookFrom, func(r *blobdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
		}),

	regionRow("graphdemo", "Scene", graphdemo.SceneLoadMeasure, graphdemo.SceneLoadTolerant, graphdemo.SceneMeasure, graphdemo.SceneSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Scene, []byte, bool) {
		var b graphdemo.SceneBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.SceneFromJsonTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.SceneToJsonMeasure, graphdemo.SceneToJson, graphdemo.SceneCookMeasure, graphdemo.SceneCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "ListNode", graphdemo.ListNodeLoadMeasure, graphdemo.ListNodeLoadTolerant, graphdemo.ListNodeMeasure, graphdemo.ListNodeSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.ListNode, []byte, bool) {
		var b graphdemo.ListNodeBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.ListNodeFromJsonTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.ListNodeToJsonMeasure, graphdemo.ListNodeToJson, graphdemo.ListNodeCookMeasure, graphdemo.ListNodeCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "TreeNode", graphdemo.TreeNodeLoadMeasure, graphdemo.TreeNodeLoadTolerant, graphdemo.TreeNodeMeasure, graphdemo.TreeNodeSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.TreeNode, []byte, bool) {
		var b graphdemo.TreeNodeBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.TreeNodeFromJsonTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.TreeNodeToJsonMeasure, graphdemo.TreeNodeToJson, graphdemo.TreeNodeCookMeasure, graphdemo.TreeNodeCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "Layer", graphdemo.LayerLoadMeasure, graphdemo.LayerLoadTolerant, graphdemo.LayerMeasure, graphdemo.LayerSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Layer, []byte, bool) {
		var b graphdemo.LayerBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.LayerFromJsonTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.LayerToJsonMeasure, graphdemo.LayerToJson, graphdemo.LayerCookMeasure, graphdemo.LayerCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "Depot", graphdemo.DepotLoadMeasure, graphdemo.DepotLoadTolerant, graphdemo.DepotMeasure, graphdemo.DepotSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Depot, []byte, bool) {
		var b graphdemo.DepotBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.DepotFromJsonTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.DepotToJsonMeasure, graphdemo.DepotToJson, graphdemo.DepotCookMeasure, graphdemo.DepotCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "Album", graphdemo.AlbumLoadMeasure, graphdemo.AlbumLoadTolerant, graphdemo.AlbumMeasure, graphdemo.AlbumSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Album, []byte, bool) {
		var b graphdemo.AlbumBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.AlbumFromJsonTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.AlbumToJsonMeasure, graphdemo.AlbumToJson, graphdemo.AlbumCookMeasure, graphdemo.AlbumCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "Marker", graphdemo.MarkerLoadMeasure, graphdemo.MarkerLoadTolerant, graphdemo.MarkerMeasure, graphdemo.MarkerSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Marker, []byte, bool) {
		var b graphdemo.MarkerBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.MarkerFromJsonTolerant(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.MarkerToJsonMeasure, graphdemo.MarkerToJson, graphdemo.MarkerCookMeasure, graphdemo.MarkerCookFrom, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	row("messagedemo", "ToolMessage", messagedemo.ToolMessageReset, messagedemo.ToolMessageLoadTolerant, messagedemo.ToolMessageMeasure, messagedemo.ToolMessageSave, messagedemo.ToolMessageFromJsonTolerant, messagedemo.ToolMessageToJsonMeasure, messagedemo.ToolMessageToJson, messagedemo.ToolMessageCookMeasure, messagedemo.ToolMessageCookFrom, func(r *messagedemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == messagedemo.TableOpenRefused}
	}),

	row("widedemo", "Caption", widedemo.CaptionReset, widedemo.CaptionLoadTolerant, widedemo.CaptionMeasure, widedemo.CaptionSave, widedemo.CaptionFromJsonTolerant, widedemo.CaptionToJsonMeasure, widedemo.CaptionToJson, widedemo.CaptionCookMeasure, widedemo.CaptionCookFrom, func(r *widedemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == widedemo.TableOpenRefused}
	}),
	row("widedemo", "Stamp", widedemo.StampReset, widedemo.StampLoadTolerant, widedemo.StampMeasure, widedemo.StampSave, widedemo.StampFromJsonTolerant, widedemo.StampToJsonMeasure, widedemo.StampToJson, widedemo.StampCookMeasure, widedemo.StampCookFrom, func(r *widedemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == widedemo.TableOpenRefused}
	}),

	row("scalars", "SimState", scalardemo.SimStateReset, scalardemo.SimStateLoadTolerant, scalardemo.SimStateMeasure, scalardemo.SimStateSave, scalardemo.SimStateFromJsonTolerant, scalardemo.SimStateToJsonMeasure, scalardemo.SimStateToJson, scalardemo.SimStateCookMeasure, scalardemo.SimStateCookFrom, func(r *scalardemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == scalardemo.TableOpenRefused}
	}),
	row("tblscalars2", "SimState", tblscalars2.SimStateReset, tblscalars2.SimStateLoadTolerant, tblscalars2.SimStateMeasure, tblscalars2.SimStateSave, tblscalars2.SimStateFromJsonTolerant, tblscalars2.SimStateToJsonMeasure, tblscalars2.SimStateToJson, tblscalars2.SimStateCookMeasure, tblscalars2.SimStateCookFrom, func(r *tblscalars2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblscalars2.TableOpenRefused}
	}),

	row("tblm1", "Msg", tblm1.MsgReset, tblm1.MsgLoadTolerant, tblm1.MsgMeasure, tblm1.MsgSave, tblm1.MsgFromJsonTolerant, tblm1.MsgToJsonMeasure, tblm1.MsgToJson, tblm1.MsgCookMeasure, tblm1.MsgCookFrom, func(r *tblm1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblm1.TableOpenRefused}
	}),
	row("tblm2", "Msg", tblm2.MsgReset, tblm2.MsgLoadTolerant, tblm2.MsgMeasure, tblm2.MsgSave, tblm2.MsgFromJsonTolerant, tblm2.MsgToJsonMeasure, tblm2.MsgToJson, tblm2.MsgCookMeasure, tblm2.MsgCookFrom, func(r *tblm2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblm2.TableOpenRefused}
	}),
	row("tbla1", "Root", tbla1.RootReset, tbla1.RootLoadTolerant, tbla1.RootMeasure, tbla1.RootSave, tbla1.RootFromJsonTolerant, tbla1.RootToJsonMeasure, tbla1.RootToJson, tbla1.RootCookMeasure, tbla1.RootCookFrom, func(r *tbla1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tbla1.TableOpenRefused}
	}),
	row("tbla2", "Root", tbla2.RootReset, tbla2.RootLoadTolerant, tbla2.RootMeasure, tbla2.RootSave, tbla2.RootFromJsonTolerant, tbla2.RootToJsonMeasure, tbla2.RootToJson, tbla2.RootCookMeasure, tbla2.RootCookFrom, func(r *tbla2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tbla2.TableOpenRefused}
	}),
	row("tblk1", "Root", tblk1.RootReset, tblk1.RootLoadTolerant, tblk1.RootMeasure, tblk1.RootSave, tblk1.RootFromJsonTolerant, tblk1.RootToJsonMeasure, tblk1.RootToJson, tblk1.RootCookMeasure, tblk1.RootCookFrom, func(r *tblk1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblk1.TableOpenRefused}
	}),
	row("tblk2", "Root", tblk2.RootReset, tblk2.RootLoadTolerant, tblk2.RootMeasure, tblk2.RootSave, tblk2.RootFromJsonTolerant, tblk2.RootToJsonMeasure, tblk2.RootToJson, tblk2.RootCookMeasure, tblk2.RootCookFrom, func(r *tblk2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblk2.TableOpenRefused}
	}),
	row("tblr1", "Cfg", tblr1.CfgReset, tblr1.CfgLoadTolerant, tblr1.CfgMeasure, tblr1.CfgSave, tblr1.CfgFromJsonTolerant, tblr1.CfgToJsonMeasure, tblr1.CfgToJson, tblr1.CfgCookMeasure, tblr1.CfgCookFrom, func(r *tblr1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblr1.TableOpenRefused}
	}),
	row("tblr2", "Cfg", tblr2.CfgReset, tblr2.CfgLoadTolerant, tblr2.CfgMeasure, tblr2.CfgSave, tblr2.CfgFromJsonTolerant, tblr2.CfgToJsonMeasure, tblr2.CfgToJson, tblr2.CfgCookMeasure, tblr2.CfgCookFrom, func(r *tblr2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblr2.TableOpenRefused}
	}),

	row("tabledemo", "RootConfig", tabledemo.RootConfigReset, tabledemo.RootConfigLoadTolerant,
		tabledemo.RootConfigMeasure, tabledemo.RootConfigSave,
		tabledemo.RootConfigFromJsonTolerant, tabledemo.RootConfigToJsonMeasure, tabledemo.RootConfigToJson, tabledemo.RootConfigCookMeasure, tabledemo.RootConfigCookFrom, snapDemo),
	row("tabledemo", "ProfileConfig", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigLoadTolerant,
		tabledemo.ProfileConfigMeasure, tabledemo.ProfileConfigSave,
		tabledemo.ProfileConfigFromJsonTolerant, tabledemo.ProfileConfigToJsonMeasure, tabledemo.ProfileConfigToJson, tabledemo.ProfileConfigCookMeasure, tabledemo.ProfileConfigCookFrom, snapDemo),
	row("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigLoadTolerant,
		tabledemo.LoadoutConfigMeasure, tabledemo.LoadoutConfigSave,
		tabledemo.LoadoutConfigFromJsonTolerant, tabledemo.LoadoutConfigToJsonMeasure, tabledemo.LoadoutConfigToJson, tabledemo.LoadoutConfigCookMeasure, tabledemo.LoadoutConfigCookFrom, snapDemo),
	row("tabledemo", "WideBlob", tabledemo.WideBlobReset, tabledemo.WideBlobLoadTolerant,
		tabledemo.WideBlobMeasure, tabledemo.WideBlobSave,
		tabledemo.WideBlobFromJsonTolerant, tabledemo.WideBlobToJsonMeasure, tabledemo.WideBlobToJson, tabledemo.WideBlobCookMeasure, tabledemo.WideBlobCookFrom, snapDemo),
	row("tabledemo", "ArchiveConfig", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigLoadTolerant,
		tabledemo.ArchiveConfigMeasure, tabledemo.ArchiveConfigSave,
		tabledemo.ArchiveConfigFromJsonTolerant, tabledemo.ArchiveConfigToJsonMeasure, tabledemo.ArchiveConfigToJson, tabledemo.ArchiveConfigCookMeasure, tabledemo.ArchiveConfigCookFrom, snapDemo),
	row("tabledemo", "PackConfig", tabledemo.PackConfigReset, tabledemo.PackConfigLoadTolerant,
		tabledemo.PackConfigMeasure, tabledemo.PackConfigSave,
		tabledemo.PackConfigFromJsonTolerant, tabledemo.PackConfigToJsonMeasure, tabledemo.PackConfigToJson, tabledemo.PackConfigCookMeasure, tabledemo.PackConfigCookFrom, snapDemo),
	row("tabledemo", "KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoadTolerant,
		tabledemo.KeyedConfigMeasure, tabledemo.KeyedConfigSave,
		tabledemo.KeyedConfigFromJsonTolerant, tabledemo.KeyedConfigToJsonMeasure, tabledemo.KeyedConfigToJson, tabledemo.KeyedConfigCookMeasure, tabledemo.KeyedConfigCookFrom, snapDemo),
	row("tblv1", "Cfg", tblv1.CfgReset, tblv1.CfgLoadTolerant, tblv1.CfgMeasure, tblv1.CfgSave,
		tblv1.CfgFromJsonTolerant, tblv1.CfgToJsonMeasure, tblv1.CfgToJson, tblv1.CfgCookMeasure, tblv1.CfgCookFrom, snapV1),
	row("tblv2", "Cfg", tblv2.CfgReset, tblv2.CfgLoadTolerant, tblv2.CfgMeasure, tblv2.CfgSave,
		tblv2.CfgFromJsonTolerant, tblv2.CfgToJsonMeasure, tblv2.CfgToJson, tblv2.CfgCookMeasure, tblv2.CfgCookFrom, snapV2),
	row("tblp1", "Chain", tblp1.ChainReset, tblp1.ChainLoadTolerant, tblp1.ChainMeasure, tblp1.ChainSave,
		tblp1.ChainFromJsonTolerant, tblp1.ChainToJsonMeasure, tblp1.ChainToJson, tblp1.ChainCookMeasure, tblp1.ChainCookFrom, snapP1),
	row("tblp3", "Chain", tblp3.ChainReset, tblp3.ChainLoadTolerant, tblp3.ChainMeasure, tblp3.ChainSave,
		tblp3.ChainFromJsonTolerant, tblp3.ChainToJsonMeasure, tblp3.ChainToJson, tblp3.ChainCookMeasure, tblp3.ChainCookFrom, snapP3),
}

// surfaces is what this backend implements. A surface not listed prints as
// ABSENT in the matrix, which is a missing FEATURE and not a failing test.
func surfaces() []string {
	return []string{"wire", "message", "report", "json-read", "json-write", "json-hostile", "cook-write", "retain", "retain-save", "cook", "cook-foreign", "block", "block-foreign", "block-dump", "forgery", "cook-forgery", "cook-reason", "block-reason"}
}
