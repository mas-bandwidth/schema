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
	"widelistdemo"

	"featurewitnesses"

	"tbla1"
	"tbla2"
	"tblframe"
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
	return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
}

func snapV1(r *tblv1.TableReport) report {
	return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
}

func snapV2(r *tblv2.TableReport) report {
	return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
}

func snapP1(r *tblp1.TableReport) report {
	return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
}

func snapP3(r *tblp3.TableReport) report {
	return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
}

var codecTable = []codec{
	regionRow("tblp2", "Chain", tblp2.ChainLoadMeasure, tblp2.ChainLoad, tblp2.ChainMeasure, tblp2.ChainSave, func(text []byte, r *tblp2.TableReport) (*tblp2.Chain, []byte, bool) {
		var b tblp2.ChainBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblp2.ChainFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblp2.ChainToJSONMeasure, tblp2.ChainToJSON, tblp2.ChainCookMeasure, tblp2.ChainCookFrom, func(r *tblp2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("tblw1", "Fleet", tblw1.FleetLoadMeasure, tblw1.FleetLoad, tblw1.FleetMeasure, tblw1.FleetSave, func(text []byte, r *tblw1.TableReport) (*tblw1.Fleet, []byte, bool) {
		var b tblw1.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblw1.FleetFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblw1.FleetToJSONMeasure, tblw1.FleetToJSON, tblw1.FleetCookMeasure, tblw1.FleetCookFrom, func(r *tblw1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("tblw2", "Fleet", tblw2.FleetLoadMeasure, tblw2.FleetLoad, tblw2.FleetMeasure, tblw2.FleetSave, func(text []byte, r *tblw2.TableReport) (*tblw2.Fleet, []byte, bool) {
		var b tblw2.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblw2.FleetFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblw2.FleetToJSONMeasure, tblw2.FleetToJSON, tblw2.FleetCookMeasure, tblw2.FleetCookFrom, func(r *tblw2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("tblg1", "Guarded", tblg1.GuardedLoadMeasure, tblg1.GuardedLoad, tblg1.GuardedMeasure, tblg1.GuardedSave, func(text []byte, r *tblg1.TableReport) (*tblg1.Guarded, []byte, bool) {
		var b tblg1.GuardedBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := tblg1.GuardedFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, tblg1.GuardedToJSONMeasure, tblg1.GuardedToJSON, tblg1.GuardedCookMeasure, tblg1.GuardedCookFrom, func(r *tblg1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	row("backenddemo", "LoginRequest", backenddemo.LoginRequestReset, backenddemo.LoginRequestLoad, backenddemo.LoginRequestMeasure, backenddemo.LoginRequestSave, backenddemo.LoginRequestFromJSON, backenddemo.LoginRequestToJSONMeasure, backenddemo.LoginRequestToJSON, backenddemo.LoginRequestCookMeasure, backenddemo.LoginRequestCookFrom, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("backenddemo", "MatchResult", backenddemo.MatchResultReset, backenddemo.MatchResultLoad, backenddemo.MatchResultMeasure, backenddemo.MatchResultSave, backenddemo.MatchResultFromJSON, backenddemo.MatchResultToJSONMeasure, backenddemo.MatchResultToJSON, backenddemo.MatchResultCookMeasure, backenddemo.MatchResultCookFrom, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("backenddemo", "StorePurchase", backenddemo.StorePurchaseReset, backenddemo.StorePurchaseLoad, backenddemo.StorePurchaseMeasure, backenddemo.StorePurchaseSave, backenddemo.StorePurchaseFromJSON, backenddemo.StorePurchaseToJSONMeasure, backenddemo.StorePurchaseToJSON, backenddemo.StorePurchaseCookMeasure, backenddemo.StorePurchaseCookFrom, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("backenddemo", "Envelope", backenddemo.EnvelopeReset, backenddemo.EnvelopeLoad, backenddemo.EnvelopeMeasure, backenddemo.EnvelopeSave, backenddemo.EnvelopeFromJSON, backenddemo.EnvelopeToJSONMeasure, backenddemo.EnvelopeToJSON, backenddemo.EnvelopeCookMeasure, backenddemo.EnvelopeCookFrom, func(r *backenddemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("vocabdemo", "Wide00", vocabdemo.Wide00Reset, vocabdemo.Wide00Load, vocabdemo.Wide00Measure, vocabdemo.Wide00Save, vocabdemo.Wide00FromJSON, vocabdemo.Wide00ToJSONMeasure, vocabdemo.Wide00ToJSON, vocabdemo.Wide00CookMeasure, vocabdemo.Wide00CookFrom, func(r *vocabdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("vocabdemo", "Wide09", vocabdemo.Wide09Reset, vocabdemo.Wide09Load, vocabdemo.Wide09Measure, vocabdemo.Wide09Save, vocabdemo.Wide09FromJSON, vocabdemo.Wide09ToJSONMeasure, vocabdemo.Wide09ToJSON, vocabdemo.Wide09CookMeasure, vocabdemo.Wide09CookFrom, func(r *vocabdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("vocab9demo", "Wide00", vocab9demo.Wide00Reset, vocab9demo.Wide00Load, vocab9demo.Wide00Measure, vocab9demo.Wide00Save, vocab9demo.Wide00FromJSON, vocab9demo.Wide00ToJSONMeasure, vocab9demo.Wide00ToJSON, vocab9demo.Wide00CookMeasure, vocab9demo.Wide00CookFrom, func(r *vocab9demo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("vocab9demo", "Wide19", vocab9demo.Wide19Reset, vocab9demo.Wide19Load, vocab9demo.Wide19Measure, vocab9demo.Wide19Save, vocab9demo.Wide19FromJSON, vocab9demo.Wide19ToJSONMeasure, vocab9demo.Wide19ToJSON, vocab9demo.Wide19CookMeasure, vocab9demo.Wide19CookFrom, func(r *vocab9demo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Cells", mapdemo.CellsLoadMeasure, mapdemo.CellsLoad, mapdemo.CellsMeasure, mapdemo.CellsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Cells, []byte, bool) {
		var b mapdemo.CellsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.CellsFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.CellsToJSONMeasure, mapdemo.CellsToJSON, mapdemo.CellsCookMeasure, mapdemo.CellsCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Chunks", mapdemo.ChunksLoadMeasure, mapdemo.ChunksLoad, mapdemo.ChunksMeasure, mapdemo.ChunksSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Chunks, []byte, bool) {
		var b mapdemo.ChunksBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.ChunksFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.ChunksToJSONMeasure, mapdemo.ChunksToJSON, mapdemo.ChunksCookMeasure, mapdemo.ChunksCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Crews", mapdemo.CrewsLoadMeasure, mapdemo.CrewsLoad, mapdemo.CrewsMeasure, mapdemo.CrewsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Crews, []byte, bool) {
		var b mapdemo.CrewsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.CrewsFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.CrewsToJSONMeasure, mapdemo.CrewsToJSON, mapdemo.CrewsCookMeasure, mapdemo.CrewsCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Depth", mapdemo.DepthLoadMeasure, mapdemo.DepthLoad, mapdemo.DepthMeasure, mapdemo.DepthSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Depth, []byte, bool) {
		var b mapdemo.DepthBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.DepthFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.DepthToJSONMeasure, mapdemo.DepthToJSON, mapdemo.DepthCookMeasure, mapdemo.DepthCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Docs", mapdemo.DocsLoadMeasure, mapdemo.DocsLoad, mapdemo.DocsMeasure, mapdemo.DocsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Docs, []byte, bool) {
		var b mapdemo.DocsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.DocsFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.DocsToJSONMeasure, mapdemo.DocsToJSON, mapdemo.DocsCookMeasure, mapdemo.DocsCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}, mapdemo.DocsMeasureReason),
	regionRow("mapdemo", "Fleet", mapdemo.FleetLoadMeasure, mapdemo.FleetLoad, mapdemo.FleetMeasure, mapdemo.FleetSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Fleet, []byte, bool) {
		var b mapdemo.FleetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.FleetFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.FleetToJSONMeasure, mapdemo.FleetToJSON, mapdemo.FleetCookMeasure, mapdemo.FleetCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Pairs", mapdemo.PairsLoadMeasure, mapdemo.PairsLoad, mapdemo.PairsMeasure, mapdemo.PairsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Pairs, []byte, bool) {
		var b mapdemo.PairsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.PairsFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.PairsToJSONMeasure, mapdemo.PairsToJSON, mapdemo.PairsCookMeasure, mapdemo.PairsCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Slots", mapdemo.SlotsLoadMeasure, mapdemo.SlotsLoad, mapdemo.SlotsMeasure, mapdemo.SlotsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Slots, []byte, bool) {
		var b mapdemo.SlotsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.SlotsFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.SlotsToJSONMeasure, mapdemo.SlotsToJSON, mapdemo.SlotsCookMeasure, mapdemo.SlotsCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Spans", mapdemo.SpansLoadMeasure, mapdemo.SpansLoad, mapdemo.SpansMeasure, mapdemo.SpansSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Spans, []byte, bool) {
		var b mapdemo.SpansBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.SpansFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.SpansToJSONMeasure, mapdemo.SpansToJSON, mapdemo.SpansCookMeasure, mapdemo.SpansCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Text", mapdemo.TextLoadMeasure, mapdemo.TextLoad, mapdemo.TextMeasure, mapdemo.TextSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Text, []byte, bool) {
		var b mapdemo.TextBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.TextFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.TextToJSONMeasure, mapdemo.TextToJSON, mapdemo.TextCookMeasure, mapdemo.TextCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Trails", mapdemo.TrailsLoadMeasure, mapdemo.TrailsLoad, mapdemo.TrailsMeasure, mapdemo.TrailsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Trails, []byte, bool) {
		var b mapdemo.TrailsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.TrailsFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.TrailsToJSONMeasure, mapdemo.TrailsToJSON, mapdemo.TrailsCookMeasure, mapdemo.TrailsCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Runs", mapdemo.RunsLoadMeasure, mapdemo.RunsLoad, mapdemo.RunsMeasure, mapdemo.RunsSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Runs, []byte, bool) {
		var b mapdemo.RunsBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.RunsFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.RunsToJSONMeasure, mapdemo.RunsToJSON, mapdemo.RunsCookMeasure, mapdemo.RunsCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "Row", mapdemo.RowLoadMeasure, mapdemo.RowLoad, mapdemo.RowMeasure, mapdemo.RowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.Row, []byte, bool) {
		var b mapdemo.RowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.RowFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.RowToJSONMeasure, mapdemo.RowToJSON, mapdemo.RowCookMeasure, mapdemo.RowCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "WideRow", mapdemo.WideRowLoadMeasure, mapdemo.WideRowLoad, mapdemo.WideRowMeasure, mapdemo.WideRowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.WideRow, []byte, bool) {
		var b mapdemo.WideRowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.WideRowFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.WideRowToJSONMeasure, mapdemo.WideRowToJSON, mapdemo.WideRowCookMeasure, mapdemo.WideRowCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("mapdemo", "EdgeRow", mapdemo.EdgeRowLoadMeasure, mapdemo.EdgeRowLoad, mapdemo.EdgeRowMeasure, mapdemo.EdgeRowSave, func(text []byte, r *mapdemo.TableReport) (*mapdemo.EdgeRow, []byte, bool) {
		var b mapdemo.EdgeRowBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := mapdemo.EdgeRowFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, mapdemo.EdgeRowToJSONMeasure, mapdemo.EdgeRowToJSON, mapdemo.EdgeRowCookMeasure, mapdemo.EdgeRowCookFrom, func(r *mapdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("widelist", "Wide", widelistdemo.WideLoadMeasure, widelistdemo.WideLoad, widelistdemo.WideMeasure, widelistdemo.WideSave,
		func(text []byte, r *widelistdemo.TableReport) (*widelistdemo.Wide, []byte, bool) {
			var b widelistdemo.WideBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := widelistdemo.WideFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, widelistdemo.WideToJSONMeasure, widelistdemo.WideToJSON, widelistdemo.WideCookMeasure, widelistdemo.WideCookFrom,
		func(r *widelistdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("featurewitnesses", "Root", featurewitnesses.RootLoadMeasure, featurewitnesses.RootLoad, featurewitnesses.RootMeasure, featurewitnesses.RootSave,
		func(text []byte, r *featurewitnesses.TableReport) (*featurewitnesses.Root, []byte, bool) {
			var b featurewitnesses.RootBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := featurewitnesses.RootFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, featurewitnesses.RootToJSONMeasure, featurewitnesses.RootToJSON, featurewitnesses.RootCookMeasure, featurewitnesses.RootCookFrom,
		func(r *featurewitnesses.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("featurewitnesses", "ArrayRoot", featurewitnesses.ArrayRootLoadMeasure, featurewitnesses.ArrayRootLoad, featurewitnesses.ArrayRootMeasure, featurewitnesses.ArrayRootSave,
		func(text []byte, r *featurewitnesses.TableReport) (*featurewitnesses.ArrayRoot, []byte, bool) {
			var b featurewitnesses.ArrayRootBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := featurewitnesses.ArrayRootFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, featurewitnesses.ArrayRootToJSONMeasure, featurewitnesses.ArrayRootToJSON, featurewitnesses.ArrayRootCookMeasure, featurewitnesses.ArrayRootCookFrom,
		func(r *featurewitnesses.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("listdemo", "Sheet", listdemo.SheetLoadMeasure, listdemo.SheetLoad, listdemo.SheetMeasure, listdemo.SheetSave, func(text []byte, r *listdemo.TableReport) (*listdemo.Sheet, []byte, bool) {
		var b listdemo.SheetBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := listdemo.SheetFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, listdemo.SheetToJSONMeasure, listdemo.SheetToJSON, listdemo.SheetCookMeasure, listdemo.SheetCookFrom, func(r *listdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("listdemo", "Army", listdemo.ArmyLoadMeasure, listdemo.ArmyLoad, listdemo.ArmyMeasure, listdemo.ArmySave, func(text []byte, r *listdemo.TableReport) (*listdemo.Army, []byte, bool) {
		var b listdemo.ArmyBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := listdemo.ArmyFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, listdemo.ArmyToJSONMeasure, listdemo.ArmyToJSON, listdemo.ArmyCookMeasure, listdemo.ArmyCookFrom, func(r *listdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("listdemo", "Squad", listdemo.SquadLoadMeasure, listdemo.SquadLoad, listdemo.SquadMeasure, listdemo.SquadSave, func(text []byte, r *listdemo.TableReport) (*listdemo.Squad, []byte, bool) {
		var b listdemo.SquadBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := listdemo.SquadFromJSON(&b, text, r)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, listdemo.SquadToJSONMeasure, listdemo.SquadToJSON, listdemo.SquadCookMeasure, listdemo.SquadCookFrom, func(r *listdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("listdemo", "Save", listdemo.SaveLoadMeasure, listdemo.SaveLoad, listdemo.SaveMeasure, listdemo.SaveSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Save, []byte, bool) {
			var b listdemo.SaveBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.SaveFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.SaveToJSONMeasure, listdemo.SaveToJSON, listdemo.SaveCookMeasure, listdemo.SaveCookFrom, func(r *listdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("listdemo", "Mixed", listdemo.MixedLoadMeasure, listdemo.MixedLoad, listdemo.MixedMeasure, listdemo.MixedSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Mixed, []byte, bool) {
			var b listdemo.MixedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.MixedFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.MixedToJSONMeasure, listdemo.MixedToJSON, listdemo.MixedCookMeasure, listdemo.MixedCookFrom, func(r *listdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("listdemo", "Bytes", listdemo.BytesLoadMeasure, listdemo.BytesLoad, listdemo.BytesMeasure, listdemo.BytesSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Bytes, []byte, bool) {
			var b listdemo.BytesBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.BytesFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.BytesToJSONMeasure, listdemo.BytesToJSON, listdemo.BytesCookMeasure, listdemo.BytesCookFrom, func(r *listdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("listdemo", "Ints", listdemo.IntsLoadMeasure, listdemo.IntsLoad, listdemo.IntsMeasure, listdemo.IntsSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Ints, []byte, bool) {
			var b listdemo.IntsBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.IntsFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.IntsToJSONMeasure, listdemo.IntsToJSON, listdemo.IntsCookMeasure, listdemo.IntsCookFrom, func(r *listdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("listdemo", "Floats", listdemo.FloatsLoadMeasure, listdemo.FloatsLoad, listdemo.FloatsMeasure, listdemo.FloatsSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Floats, []byte, bool) {
			var b listdemo.FloatsBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.FloatsFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.FloatsToJSONMeasure, listdemo.FloatsToJSON, listdemo.FloatsCookMeasure, listdemo.FloatsCookFrom, func(r *listdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("listdemo", "Album", listdemo.AlbumLoadMeasure, listdemo.AlbumLoad, listdemo.AlbumMeasure, listdemo.AlbumSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Album, []byte, bool) {
			var b listdemo.AlbumBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.AlbumFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.AlbumToJSONMeasure, listdemo.AlbumToJSON, listdemo.AlbumCookMeasure, listdemo.AlbumCookFrom, func(r *listdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("listdemo", "Unbounded", listdemo.UnboundedLoadMeasure, listdemo.UnboundedLoad, listdemo.UnboundedMeasure, listdemo.UnboundedSave,
		func(text []byte, r *listdemo.TableReport) (*listdemo.Unbounded, []byte, bool) {
			var b listdemo.UnboundedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := listdemo.UnboundedFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, listdemo.UnboundedToJSONMeasure, listdemo.UnboundedToJSON, listdemo.UnboundedCookMeasure, listdemo.UnboundedCookFrom, func(r *listdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),

	regionRow("streamdemo", "Feed", streamdemo.FeedLoadMeasure, streamdemo.FeedLoad, streamdemo.FeedMeasure, streamdemo.FeedSave,
		func(text []byte, r *streamdemo.TableReport) (*streamdemo.Feed, []byte, bool) {
			var b streamdemo.FeedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := streamdemo.FeedFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		streamdemo.FeedToJSONMeasure, streamdemo.FeedToJSON, streamdemo.FeedCookMeasure, streamdemo.FeedCookFrom, func(r *streamdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),

	regionRow("streamdemo", "Chunk", streamdemo.ChunkLoadMeasure, streamdemo.ChunkLoad, streamdemo.ChunkMeasure, streamdemo.ChunkSave,
		func(text []byte, r *streamdemo.TableReport) (*streamdemo.Chunk, []byte, bool) {
			var b streamdemo.ChunkBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := streamdemo.ChunkFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		streamdemo.ChunkToJSONMeasure, streamdemo.ChunkToJSON, streamdemo.ChunkCookMeasure, streamdemo.ChunkCookFrom, func(r *streamdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),

	withCookInput(regionRow("blobdemo", "Catalog", blobdemo.CatalogLoadMeasure, blobdemo.CatalogLoad, blobdemo.CatalogMeasure, blobdemo.CatalogSave,
		func(text []byte, r *blobdemo.TableReport) (*blobdemo.Catalog, []byte, bool) {
			var b blobdemo.CatalogBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := blobdemo.CatalogFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		blobdemo.CatalogToJSONMeasure, blobdemo.CatalogToJSON, blobdemo.CatalogCookMeasure, blobdemo.CatalogCookFrom, func(r *blobdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}, blobdemo.CatalogMeasureReason), cookCatalogInput),

	regionRow("blobdemo", "Asset", blobdemo.AssetLoadMeasure, blobdemo.AssetLoad, blobdemo.AssetMeasure, blobdemo.AssetSave,
		func(text []byte, r *blobdemo.TableReport) (*blobdemo.Asset, []byte, bool) {
			var b blobdemo.AssetBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := blobdemo.AssetFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		blobdemo.AssetToJSONMeasure, blobdemo.AssetToJSON, blobdemo.AssetCookMeasure, blobdemo.AssetCookFrom, func(r *blobdemo.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),

	regionRow("graphdemo", "Scene", graphdemo.SceneLoadMeasure, graphdemo.SceneLoad, graphdemo.SceneMeasure, graphdemo.SceneSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Scene, []byte, bool) {
		var b graphdemo.SceneBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.SceneFromJSON(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.SceneToJSONMeasure, graphdemo.SceneToJSON, graphdemo.SceneCookMeasure, graphdemo.SceneCookFrom, func(r *graphdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	regionRow("graphdemo", "ListNode", graphdemo.ListNodeLoadMeasure, graphdemo.ListNodeLoad, graphdemo.ListNodeMeasure, graphdemo.ListNodeSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.ListNode, []byte, bool) {
		var b graphdemo.ListNodeBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.ListNodeFromJSON(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.ListNodeToJSONMeasure, graphdemo.ListNodeToJSON, graphdemo.ListNodeCookMeasure, graphdemo.ListNodeCookFrom, func(r *graphdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	regionRow("graphdemo", "TreeNode", graphdemo.TreeNodeLoadMeasure, graphdemo.TreeNodeLoad, graphdemo.TreeNodeMeasure, graphdemo.TreeNodeSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.TreeNode, []byte, bool) {
		var b graphdemo.TreeNodeBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.TreeNodeFromJSON(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.TreeNodeToJSONMeasure, graphdemo.TreeNodeToJSON, graphdemo.TreeNodeCookMeasure, graphdemo.TreeNodeCookFrom, func(r *graphdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	regionRow("graphdemo", "Layer", graphdemo.LayerLoadMeasure, graphdemo.LayerLoad, graphdemo.LayerMeasure, graphdemo.LayerSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Layer, []byte, bool) {
		var b graphdemo.LayerBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.LayerFromJSON(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.LayerToJSONMeasure, graphdemo.LayerToJSON, graphdemo.LayerCookMeasure, graphdemo.LayerCookFrom, func(r *graphdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	regionRow("graphdemo", "Depot", graphdemo.DepotLoadMeasure, graphdemo.DepotLoad, graphdemo.DepotMeasure, graphdemo.DepotSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Depot, []byte, bool) {
		var b graphdemo.DepotBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.DepotFromJSON(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.DepotToJSONMeasure, graphdemo.DepotToJSON, graphdemo.DepotCookMeasure, graphdemo.DepotCookFrom, func(r *graphdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	regionRow("graphdemo", "Album", graphdemo.AlbumLoadMeasure, graphdemo.AlbumLoad, graphdemo.AlbumMeasure, graphdemo.AlbumSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Album, []byte, bool) {
		var b graphdemo.AlbumBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.AlbumFromJSON(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.AlbumToJSONMeasure, graphdemo.AlbumToJSON, graphdemo.AlbumCookMeasure, graphdemo.AlbumCookFrom, func(r *graphdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	regionRow("graphdemo", "Marker", graphdemo.MarkerLoadMeasure, graphdemo.MarkerLoad, graphdemo.MarkerMeasure, graphdemo.MarkerSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Marker, []byte, bool) {
		var b graphdemo.MarkerBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.MarkerFromJSON(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.MarkerToJSONMeasure, graphdemo.MarkerToJSON, graphdemo.MarkerCookMeasure, graphdemo.MarkerCookFrom, func(r *graphdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	row("messagedemo", "ToolMessage", messagedemo.ToolMessageReset, messagedemo.ToolMessageLoad, messagedemo.ToolMessageMeasure, messagedemo.ToolMessageSave, messagedemo.ToolMessageFromJSON, messagedemo.ToolMessageToJSONMeasure, messagedemo.ToolMessageToJSON, messagedemo.ToolMessageCookMeasure, messagedemo.ToolMessageCookFrom, func(r *messagedemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	row("widedemo", "Caption", widedemo.CaptionReset, widedemo.CaptionLoad, widedemo.CaptionMeasure, widedemo.CaptionSave, widedemo.CaptionFromJSON, widedemo.CaptionToJSONMeasure, widedemo.CaptionToJSON, widedemo.CaptionCookMeasure, widedemo.CaptionCookFrom, func(r *widedemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("widedemo", "Stamp", widedemo.StampReset, widedemo.StampLoad, widedemo.StampMeasure, widedemo.StampSave, widedemo.StampFromJSON, widedemo.StampToJSONMeasure, widedemo.StampToJSON, widedemo.StampCookMeasure, widedemo.StampCookFrom, func(r *widedemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	row("scalars", "SimState", scalardemo.SimStateReset, scalardemo.SimStateLoad, scalardemo.SimStateMeasure, scalardemo.SimStateSave, scalardemo.SimStateFromJSON, scalardemo.SimStateToJSONMeasure, scalardemo.SimStateToJSON, scalardemo.SimStateCookMeasure, scalardemo.SimStateCookFrom, func(r *scalardemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("tblscalars2", "SimState", tblscalars2.SimStateReset, tblscalars2.SimStateLoad, tblscalars2.SimStateMeasure, tblscalars2.SimStateSave, tblscalars2.SimStateFromJSON, tblscalars2.SimStateToJSONMeasure, tblscalars2.SimStateToJSON, tblscalars2.SimStateCookMeasure, tblscalars2.SimStateCookFrom, func(r *tblscalars2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	row("tblm1", "Msg", tblm1.MsgReset, tblm1.MsgLoad, tblm1.MsgMeasure, tblm1.MsgSave, tblm1.MsgFromJSON, tblm1.MsgToJSONMeasure, tblm1.MsgToJSON, tblm1.MsgCookMeasure, tblm1.MsgCookFrom, func(r *tblm1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("tblm2", "Msg", tblm2.MsgReset, tblm2.MsgLoad, tblm2.MsgMeasure, tblm2.MsgSave, tblm2.MsgFromJSON, tblm2.MsgToJSONMeasure, tblm2.MsgToJSON, tblm2.MsgCookMeasure, tblm2.MsgCookFrom, func(r *tblm2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("tbla1", "Root", tbla1.RootReset, tbla1.RootLoad, tbla1.RootMeasure, tbla1.RootSave, tbla1.RootFromJSON, tbla1.RootToJSONMeasure, tbla1.RootToJSON, tbla1.RootCookMeasure, tbla1.RootCookFrom, func(r *tbla1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("tbla2", "Root", tbla2.RootReset, tbla2.RootLoad, tbla2.RootMeasure, tbla2.RootSave, tbla2.RootFromJSON, tbla2.RootToJSONMeasure, tbla2.RootToJSON, tbla2.RootCookMeasure, tbla2.RootCookFrom, func(r *tbla2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("tblk1", "Root", tblk1.RootReset, tblk1.RootLoad, tblk1.RootMeasure, tblk1.RootSave, tblk1.RootFromJSON, tblk1.RootToJSONMeasure, tblk1.RootToJSON, tblk1.RootCookMeasure, tblk1.RootCookFrom, func(r *tblk1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("tblk2", "Root", tblk2.RootReset, tblk2.RootLoad, tblk2.RootMeasure, tblk2.RootSave, tblk2.RootFromJSON, tblk2.RootToJSONMeasure, tblk2.RootToJSON, tblk2.RootCookMeasure, tblk2.RootCookFrom, func(r *tblk2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("listdemo", "Bounded", listdemo.BoundedReset, listdemo.BoundedLoad, listdemo.BoundedMeasure, listdemo.BoundedSave, listdemo.BoundedFromJSON, listdemo.BoundedToJSONMeasure, listdemo.BoundedToJSON, listdemo.BoundedCookMeasure, listdemo.BoundedCookFrom, func(r *listdemo.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	regionRow("tblframe", "Bounded", tblframe.BoundedLoadMeasure, tblframe.BoundedLoad, tblframe.BoundedMeasure, tblframe.BoundedSave,
		func(text []byte, r *tblframe.TableReport) (*tblframe.Bounded, []byte, bool) {
			var b tblframe.BoundedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := tblframe.BoundedFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, tblframe.BoundedToJSONMeasure, tblframe.BoundedToJSON, tblframe.BoundedCookMeasure, tblframe.BoundedCookFrom, func(r *tblframe.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	regionRow("tblframe", "Cfg", tblframe.CfgLoadMeasure, tblframe.CfgLoad, tblframe.CfgMeasure, tblframe.CfgSave,
		func(text []byte, r *tblframe.TableReport) (*tblframe.Cfg, []byte, bool) {
			var b tblframe.CfgBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := tblframe.CfgFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, tblframe.CfgToJSONMeasure, tblframe.CfgToJSON, tblframe.CfgCookMeasure, tblframe.CfgCookFrom, func(r *tblframe.TableReport) report {
			return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
		}),
	row("tblr1", "Cfg", tblr1.CfgReset, tblr1.CfgLoad, tblr1.CfgMeasure, tblr1.CfgSave, tblr1.CfgFromJSON, tblr1.CfgToJSONMeasure, tblr1.CfgToJSON, tblr1.CfgCookMeasure, tblr1.CfgCookFrom, func(r *tblr1.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),
	row("tblr2", "Cfg", tblr2.CfgReset, tblr2.CfgLoad, tblr2.CfgMeasure, tblr2.CfgSave, tblr2.CfgFromJSON, tblr2.CfgToJSONMeasure, tblr2.CfgToJSON, tblr2.CfgCookMeasure, tblr2.CfgCookFrom, func(r *tblr2.TableReport) report {
		return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
	}),

	row("tabledemo", "RootConfig", tabledemo.RootConfigReset, tabledemo.RootConfigLoad,
		tabledemo.RootConfigMeasure, tabledemo.RootConfigSave,
		tabledemo.RootConfigFromJSON, tabledemo.RootConfigToJSONMeasure, tabledemo.RootConfigToJSON, tabledemo.RootConfigCookMeasure, tabledemo.RootConfigCookFrom, snapDemo),
	row("tabledemo", "ProfileConfig", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigLoad,
		tabledemo.ProfileConfigMeasure, tabledemo.ProfileConfigSave,
		tabledemo.ProfileConfigFromJSON, tabledemo.ProfileConfigToJSONMeasure, tabledemo.ProfileConfigToJSON, tabledemo.ProfileConfigCookMeasure, tabledemo.ProfileConfigCookFrom, snapDemo),
	row("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigLoad,
		tabledemo.LoadoutConfigMeasure, tabledemo.LoadoutConfigSave,
		tabledemo.LoadoutConfigFromJSON, tabledemo.LoadoutConfigToJSONMeasure, tabledemo.LoadoutConfigToJSON, tabledemo.LoadoutConfigCookMeasure, tabledemo.LoadoutConfigCookFrom, snapDemo),
	regionRow("tabledemo", "WideExtent", tabledemo.WideExtentLoadMeasure, tabledemo.WideExtentLoad,
		tabledemo.WideExtentMeasure, tabledemo.WideExtentSave,
		func(text []byte, r *tabledemo.TableReport) (*tabledemo.WideExtent, []byte, bool) {
			var b tabledemo.WideExtentBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := tabledemo.WideExtentFromJSON(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		}, tabledemo.WideExtentToJSONMeasure, tabledemo.WideExtentToJSON, tabledemo.WideExtentCookMeasure, tabledemo.WideExtentCookFrom, snapDemo),
	row("tabledemo", "WideBlob", tabledemo.WideBlobReset, tabledemo.WideBlobLoad,
		tabledemo.WideBlobMeasure, tabledemo.WideBlobSave,
		tabledemo.WideBlobFromJSON, tabledemo.WideBlobToJSONMeasure, tabledemo.WideBlobToJSON, tabledemo.WideBlobCookMeasure, tabledemo.WideBlobCookFrom, snapDemo),
	row("tabledemo", "ArchiveConfig", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigLoad,
		tabledemo.ArchiveConfigMeasure, tabledemo.ArchiveConfigSave,
		tabledemo.ArchiveConfigFromJSON, tabledemo.ArchiveConfigToJSONMeasure, tabledemo.ArchiveConfigToJSON, tabledemo.ArchiveConfigCookMeasure, tabledemo.ArchiveConfigCookFrom, snapDemo),
	row("tabledemo", "PackConfig", tabledemo.PackConfigReset, tabledemo.PackConfigLoad,
		tabledemo.PackConfigMeasure, tabledemo.PackConfigSave,
		tabledemo.PackConfigFromJSON, tabledemo.PackConfigToJSONMeasure, tabledemo.PackConfigToJSON, tabledemo.PackConfigCookMeasure, tabledemo.PackConfigCookFrom, snapDemo),
	row("tabledemo", "KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoad,
		tabledemo.KeyedConfigMeasure, tabledemo.KeyedConfigSave,
		tabledemo.KeyedConfigFromJSON, tabledemo.KeyedConfigToJSONMeasure, tabledemo.KeyedConfigToJSON, tabledemo.KeyedConfigCookMeasure, tabledemo.KeyedConfigCookFrom, snapDemo),
	row("tblv1", "Cfg", tblv1.CfgReset, tblv1.CfgLoad, tblv1.CfgMeasure, tblv1.CfgSave,
		tblv1.CfgFromJSON, tblv1.CfgToJSONMeasure, tblv1.CfgToJSON, tblv1.CfgCookMeasure, tblv1.CfgCookFrom, snapV1),
	row("tblv2", "Cfg", tblv2.CfgReset, tblv2.CfgLoad, tblv2.CfgMeasure, tblv2.CfgSave,
		tblv2.CfgFromJSON, tblv2.CfgToJSONMeasure, tblv2.CfgToJSON, tblv2.CfgCookMeasure, tblv2.CfgCookFrom, snapV2),
	row("tblp1", "Chain", tblp1.ChainReset, tblp1.ChainLoad, tblp1.ChainMeasure, tblp1.ChainSave,
		tblp1.ChainFromJSON, tblp1.ChainToJSONMeasure, tblp1.ChainToJSON, tblp1.ChainCookMeasure, tblp1.ChainCookFrom, snapP1),
	row("tblp3", "Chain", tblp3.ChainReset, tblp3.ChainLoad, tblp3.ChainMeasure, tblp3.ChainSave,
		tblp3.ChainFromJSON, tblp3.ChainToJSONMeasure, tblp3.ChainToJSON, tblp3.ChainCookMeasure, tblp3.ChainCookFrom, snapP3),
}

// surfaces is what this backend implements. A surface not listed prints as
// ABSENT in the matrix, which is a missing FEATURE and not a failing test.
func surfaces() []string {
	return []string{"wire", "message", "report", "framing", "json-read", "json-write", "json-hostile", "cook-write", "retain", "retain-save", "cook", "cook-foreign", "block", "block-foreign", "block-dump", "forgery", "cook-forgery", "cook-reason", "block-reason"}
}
