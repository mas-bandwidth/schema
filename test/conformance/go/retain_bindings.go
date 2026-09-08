package main

import (
	"blobdemo"
	"graphdemo"
	"listdemo"
	"mapdemo"
	"streamdemo"
	"tblg1"
	"tblp2"
	"tblw1"
	"tblw2"
)

var retentionCodecs = []retentionCodec{
	retainRow("tblp2", "Chain", func() *tblp2.TableRetain {
		return &tblp2.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]tblp2.TableRetainId, (1<<20)/8)}
	}, tblp2.ChainLoadMeasure, tblp2.ChainLoadRetain, tblp2.ChainMeasureRetain, tblp2.ChainSaveRetain, func(r *tblp2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp2.TableOpenRefused}
	}, func(r *tblp2.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("tblw1", "Fleet", func() *tblw1.TableRetain {
		return &tblw1.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]tblw1.TableRetainId, (1<<20)/8)}
	}, tblw1.FleetLoadMeasure, tblw1.FleetLoadRetain, tblw1.FleetMeasureRetain, tblw1.FleetSaveRetain, func(r *tblw1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw1.TableOpenRefused}
	}, func(r *tblw1.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("tblw2", "Fleet", func() *tblw2.TableRetain {
		return &tblw2.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]tblw2.TableRetainId, (1<<20)/8)}
	}, tblw2.FleetLoadMeasure, tblw2.FleetLoadRetain, tblw2.FleetMeasureRetain, tblw2.FleetSaveRetain, func(r *tblw2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw2.TableOpenRefused}
	}, func(r *tblw2.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("tblg1", "Guarded", func() *tblg1.TableRetain {
		return &tblg1.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]tblg1.TableRetainId, (1<<20)/8)}
	}, tblg1.GuardedLoadMeasure, tblg1.GuardedLoadRetain, tblg1.GuardedMeasureRetain, tblg1.GuardedSaveRetain, func(r *tblg1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblg1.TableOpenRefused}
	}, func(r *tblg1.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Cells", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.CellsLoadMeasure, mapdemo.CellsLoadRetain, mapdemo.CellsMeasureRetain, mapdemo.CellsSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Chunks", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.ChunksLoadMeasure, mapdemo.ChunksLoadRetain, mapdemo.ChunksMeasureRetain, mapdemo.ChunksSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Crews", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.CrewsLoadMeasure, mapdemo.CrewsLoadRetain, mapdemo.CrewsMeasureRetain, mapdemo.CrewsSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Depth", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.DepthLoadMeasure, mapdemo.DepthLoadRetain, mapdemo.DepthMeasureRetain, mapdemo.DepthSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Docs", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.DocsLoadMeasure, mapdemo.DocsLoadRetain, mapdemo.DocsMeasureRetain, mapdemo.DocsSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Fleet", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.FleetLoadMeasure, mapdemo.FleetLoadRetain, mapdemo.FleetMeasureRetain, mapdemo.FleetSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Pairs", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.PairsLoadMeasure, mapdemo.PairsLoadRetain, mapdemo.PairsMeasureRetain, mapdemo.PairsSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Slots", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.SlotsLoadMeasure, mapdemo.SlotsLoadRetain, mapdemo.SlotsMeasureRetain, mapdemo.SlotsSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Spans", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.SpansLoadMeasure, mapdemo.SpansLoadRetain, mapdemo.SpansMeasureRetain, mapdemo.SpansSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Text", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.TextLoadMeasure, mapdemo.TextLoadRetain, mapdemo.TextMeasureRetain, mapdemo.TextSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Trails", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.TrailsLoadMeasure, mapdemo.TrailsLoadRetain, mapdemo.TrailsMeasureRetain, mapdemo.TrailsSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Runs", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.RunsLoadMeasure, mapdemo.RunsLoadRetain, mapdemo.RunsMeasureRetain, mapdemo.RunsSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "Row", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.RowLoadMeasure, mapdemo.RowLoadRetain, mapdemo.RowMeasureRetain, mapdemo.RowSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "WideRow", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.WideRowLoadMeasure, mapdemo.WideRowLoadRetain, mapdemo.WideRowMeasureRetain, mapdemo.WideRowSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("mapdemo", "EdgeRow", func() *mapdemo.TableRetain {
		return &mapdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]mapdemo.TableRetainId, (1<<20)/8)}
	}, mapdemo.EdgeRowLoadMeasure, mapdemo.EdgeRowLoadRetain, mapdemo.EdgeRowMeasureRetain, mapdemo.EdgeRowSaveRetain, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}, func(r *mapdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Sheet", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.SheetLoadMeasure, listdemo.SheetLoadRetain, listdemo.SheetMeasureRetain, listdemo.SheetSaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Army", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.ArmyLoadMeasure, listdemo.ArmyLoadRetain, listdemo.ArmyMeasureRetain, listdemo.ArmySaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Save", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.SaveLoadMeasure, listdemo.SaveLoadRetain, listdemo.SaveMeasureRetain, listdemo.SaveSaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Mixed", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.MixedLoadMeasure, listdemo.MixedLoadRetain, listdemo.MixedMeasureRetain, listdemo.MixedSaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Bytes", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.BytesLoadMeasure, listdemo.BytesLoadRetain, listdemo.BytesMeasureRetain, listdemo.BytesSaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Ints", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.IntsLoadMeasure, listdemo.IntsLoadRetain, listdemo.IntsMeasureRetain, listdemo.IntsSaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Floats", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.FloatsLoadMeasure, listdemo.FloatsLoadRetain, listdemo.FloatsMeasureRetain, listdemo.FloatsSaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Album", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.AlbumLoadMeasure, listdemo.AlbumLoadRetain, listdemo.AlbumMeasureRetain, listdemo.AlbumSaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("listdemo", "Unbounded", func() *listdemo.TableRetain {
		return &listdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]listdemo.TableRetainId, (1<<20)/8)}
	}, listdemo.UnboundedLoadMeasure, listdemo.UnboundedLoadRetain, listdemo.UnboundedMeasureRetain, listdemo.UnboundedSaveRetain, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}, func(r *listdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("streamdemo", "Feed", func() *streamdemo.TableRetain {
		return &streamdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]streamdemo.TableRetainId, (1<<20)/8)}
	}, streamdemo.FeedLoadMeasure, streamdemo.FeedLoadRetain, streamdemo.FeedMeasureRetain, streamdemo.FeedSaveRetain, func(r *streamdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
	}, func(r *streamdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("streamdemo", "Chunk", func() *streamdemo.TableRetain {
		return &streamdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]streamdemo.TableRetainId, (1<<20)/8)}
	}, streamdemo.ChunkLoadMeasure, streamdemo.ChunkLoadRetain, streamdemo.ChunkMeasureRetain, streamdemo.ChunkSaveRetain, func(r *streamdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
	}, func(r *streamdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("blobdemo", "Catalog", func() *blobdemo.TableRetain {
		return &blobdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]blobdemo.TableRetainId, (1<<20)/8)}
	}, blobdemo.CatalogLoadMeasure, blobdemo.CatalogLoadRetain, blobdemo.CatalogMeasureRetain, blobdemo.CatalogSaveRetain, func(r *blobdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
	}, func(r *blobdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("blobdemo", "Asset", func() *blobdemo.TableRetain {
		return &blobdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]blobdemo.TableRetainId, (1<<20)/8)}
	}, blobdemo.AssetLoadMeasure, blobdemo.AssetLoadRetain, blobdemo.AssetMeasureRetain, blobdemo.AssetSaveRetain, func(r *blobdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
	}, func(r *blobdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("graphdemo", "Scene", func() *graphdemo.TableRetain {
		return &graphdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]graphdemo.TableRetainId, (1<<20)/8)}
	}, graphdemo.SceneLoadMeasure, graphdemo.SceneLoadRetain, graphdemo.SceneMeasureRetain, graphdemo.SceneSaveRetain, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}, func(r *graphdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("graphdemo", "ListNode", func() *graphdemo.TableRetain {
		return &graphdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]graphdemo.TableRetainId, (1<<20)/8)}
	}, graphdemo.ListNodeLoadMeasure, graphdemo.ListNodeLoadRetain, graphdemo.ListNodeMeasureRetain, graphdemo.ListNodeSaveRetain, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}, func(r *graphdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("graphdemo", "TreeNode", func() *graphdemo.TableRetain {
		return &graphdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]graphdemo.TableRetainId, (1<<20)/8)}
	}, graphdemo.TreeNodeLoadMeasure, graphdemo.TreeNodeLoadRetain, graphdemo.TreeNodeMeasureRetain, graphdemo.TreeNodeSaveRetain, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}, func(r *graphdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("graphdemo", "Layer", func() *graphdemo.TableRetain {
		return &graphdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]graphdemo.TableRetainId, (1<<20)/8)}
	}, graphdemo.LayerLoadMeasure, graphdemo.LayerLoadRetain, graphdemo.LayerMeasureRetain, graphdemo.LayerSaveRetain, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}, func(r *graphdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("graphdemo", "Depot", func() *graphdemo.TableRetain {
		return &graphdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]graphdemo.TableRetainId, (1<<20)/8)}
	}, graphdemo.DepotLoadMeasure, graphdemo.DepotLoadRetain, graphdemo.DepotMeasureRetain, graphdemo.DepotSaveRetain, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}, func(r *graphdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("graphdemo", "Album", func() *graphdemo.TableRetain {
		return &graphdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]graphdemo.TableRetainId, (1<<20)/8)}
	}, graphdemo.AlbumLoadMeasure, graphdemo.AlbumLoadRetain, graphdemo.AlbumMeasureRetain, graphdemo.AlbumSaveRetain, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}, func(r *graphdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
	retainRow("graphdemo", "Marker", func() *graphdemo.TableRetain {
		return &graphdemo.TableRetain{Bytes: make([]byte, 1<<20), Ids: make([]graphdemo.TableRetainId, (1<<20)/8)}
	}, graphdemo.MarkerLoadMeasure, graphdemo.MarkerLoadRetain, graphdemo.MarkerMeasureRetain, graphdemo.MarkerSaveRetain, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}, func(r *graphdemo.TableReport) (int32, int32) { return r.Retained, r.RetainLost }),
}
