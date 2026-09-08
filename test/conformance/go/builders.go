package main

import (
	"tblg1"
	"tblp2"
	"tblw1"
	"tblw2"

	"blobdemo"
	"graphdemo"
	"listdemo"
	"mapdemo"
	"streamdemo"
)

// The builder arm deliberately skips LoadMeasure. A count refusal can stop
// authoring while a region preflight would report a different result.
type builderCodec struct {
	unit, root string
	run        func([]byte) ([]byte, report, bool)
}

func builderRow[B, T, R, A, C any](unit, root string, init func(*B, ...A) bool, shutdown func(*B), get func(*B) *T, context func(*B) C, load func(*B, []byte, *R) bool, measure func(*T, ...C) int64, save func(*T, []byte, ...C) int64, snap func(*R) report) builderCodec {
	return builderCodec{unit, root, func(wire []byte) ([]byte, report, bool) {
		var b B
		var r R
		if !init(&b) {
			return nil, report{}, false
		}
		defer shutdown(&b)
		ok := load(&b, wire, &r)
		rep := snap(&r)
		if !ok {
			return nil, rep, false
		}
		value, ctx := get(&b), context(&b)
		n := measure(value, ctx)
		if n < 0 {
			return nil, rep, true
		}
		out := make([]byte, n)
		if save(value, out, ctx) != n {
			return nil, rep, true
		}
		return out, rep, true
	}}
}
func findBuilderCodec(unit, root string) *builderCodec {
	for i := range builderCodecs {
		if builderCodecs[i].unit == unit && builderCodecs[i].root == root {
			return &builderCodecs[i]
		}
	}
	return nil
}

var builderCodecs = []builderCodec{
	builderRow("tblp2", "Chain", (*tblp2.ChainBuilder).Init, (*tblp2.ChainBuilder).Shutdown, (*tblp2.ChainBuilder).GetRoot, func(b *tblp2.ChainBuilder) tblp2.TableWriteContext { return &b.Arena }, tblp2.ChainLoadBuilder, tblp2.ChainMeasure, tblp2.ChainSave, func(r *tblp2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp2.TableOpenRefused}
	}),
	builderRow("tblw1", "Fleet", (*tblw1.FleetBuilder).Init, (*tblw1.FleetBuilder).Shutdown, (*tblw1.FleetBuilder).GetRoot, func(b *tblw1.FleetBuilder) tblw1.TableWriteContext { return &b.Arena }, tblw1.FleetLoadBuilder, tblw1.FleetMeasure, tblw1.FleetSave, func(r *tblw1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw1.TableOpenRefused}
	}),
	builderRow("tblw2", "Fleet", (*tblw2.FleetBuilder).Init, (*tblw2.FleetBuilder).Shutdown, (*tblw2.FleetBuilder).GetRoot, func(b *tblw2.FleetBuilder) tblw2.TableWriteContext { return &b.Arena }, tblw2.FleetLoadBuilder, tblw2.FleetMeasure, tblw2.FleetSave, func(r *tblw2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw2.TableOpenRefused}
	}),
	builderRow("tblg1", "Guarded", (*tblg1.GuardedBuilder).Init, (*tblg1.GuardedBuilder).Shutdown, (*tblg1.GuardedBuilder).GetRoot, func(b *tblg1.GuardedBuilder) tblg1.TableWriteContext { return &b.Arena }, tblg1.GuardedLoadBuilder, tblg1.GuardedMeasure, tblg1.GuardedSave, func(r *tblg1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblg1.TableOpenRefused}
	}),

	builderRow("mapdemo", "Cells", (*mapdemo.CellsBuilder).Init, (*mapdemo.CellsBuilder).Shutdown, (*mapdemo.CellsBuilder).GetRoot, func(b *mapdemo.CellsBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.CellsLoadBuilder, mapdemo.CellsMeasure, mapdemo.CellsSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Chunks", (*mapdemo.ChunksBuilder).Init, (*mapdemo.ChunksBuilder).Shutdown, (*mapdemo.ChunksBuilder).GetRoot, func(b *mapdemo.ChunksBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.ChunksLoadBuilder, mapdemo.ChunksMeasure, mapdemo.ChunksSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Crews", (*mapdemo.CrewsBuilder).Init, (*mapdemo.CrewsBuilder).Shutdown, (*mapdemo.CrewsBuilder).GetRoot, func(b *mapdemo.CrewsBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.CrewsLoadBuilder, mapdemo.CrewsMeasure, mapdemo.CrewsSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Depth", (*mapdemo.DepthBuilder).Init, (*mapdemo.DepthBuilder).Shutdown, (*mapdemo.DepthBuilder).GetRoot, func(b *mapdemo.DepthBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.DepthLoadBuilder, mapdemo.DepthMeasure, mapdemo.DepthSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Docs", (*mapdemo.DocsBuilder).Init, (*mapdemo.DocsBuilder).Shutdown, (*mapdemo.DocsBuilder).GetRoot, func(b *mapdemo.DocsBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.DocsLoadBuilder, mapdemo.DocsMeasure, mapdemo.DocsSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Fleet", (*mapdemo.FleetBuilder).Init, (*mapdemo.FleetBuilder).Shutdown, (*mapdemo.FleetBuilder).GetRoot, func(b *mapdemo.FleetBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.FleetLoadBuilder, mapdemo.FleetMeasure, mapdemo.FleetSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Pairs", (*mapdemo.PairsBuilder).Init, (*mapdemo.PairsBuilder).Shutdown, (*mapdemo.PairsBuilder).GetRoot, func(b *mapdemo.PairsBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.PairsLoadBuilder, mapdemo.PairsMeasure, mapdemo.PairsSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Slots", (*mapdemo.SlotsBuilder).Init, (*mapdemo.SlotsBuilder).Shutdown, (*mapdemo.SlotsBuilder).GetRoot, func(b *mapdemo.SlotsBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.SlotsLoadBuilder, mapdemo.SlotsMeasure, mapdemo.SlotsSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Spans", (*mapdemo.SpansBuilder).Init, (*mapdemo.SpansBuilder).Shutdown, (*mapdemo.SpansBuilder).GetRoot, func(b *mapdemo.SpansBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.SpansLoadBuilder, mapdemo.SpansMeasure, mapdemo.SpansSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Text", (*mapdemo.TextBuilder).Init, (*mapdemo.TextBuilder).Shutdown, (*mapdemo.TextBuilder).GetRoot, func(b *mapdemo.TextBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.TextLoadBuilder, mapdemo.TextMeasure, mapdemo.TextSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Trails", (*mapdemo.TrailsBuilder).Init, (*mapdemo.TrailsBuilder).Shutdown, (*mapdemo.TrailsBuilder).GetRoot, func(b *mapdemo.TrailsBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.TrailsLoadBuilder, mapdemo.TrailsMeasure, mapdemo.TrailsSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Runs", (*mapdemo.RunsBuilder).Init, (*mapdemo.RunsBuilder).Shutdown, (*mapdemo.RunsBuilder).GetRoot, func(b *mapdemo.RunsBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.RunsLoadBuilder, mapdemo.RunsMeasure, mapdemo.RunsSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "Row", (*mapdemo.RowBuilder).Init, (*mapdemo.RowBuilder).Shutdown, (*mapdemo.RowBuilder).GetRoot, func(b *mapdemo.RowBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.RowLoadBuilder, mapdemo.RowMeasure, mapdemo.RowSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "WideRow", (*mapdemo.WideRowBuilder).Init, (*mapdemo.WideRowBuilder).Shutdown, (*mapdemo.WideRowBuilder).GetRoot, func(b *mapdemo.WideRowBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.WideRowLoadBuilder, mapdemo.WideRowMeasure, mapdemo.WideRowSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("mapdemo", "EdgeRow", (*mapdemo.EdgeRowBuilder).Init, (*mapdemo.EdgeRowBuilder).Shutdown, (*mapdemo.EdgeRowBuilder).GetRoot, func(b *mapdemo.EdgeRowBuilder) mapdemo.TableWriteContext { return &b.Arena }, mapdemo.EdgeRowLoadBuilder, mapdemo.EdgeRowMeasure, mapdemo.EdgeRowSave, func(r *mapdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Sheet", (*listdemo.SheetBuilder).Init, (*listdemo.SheetBuilder).Shutdown, (*listdemo.SheetBuilder).GetRoot, func(b *listdemo.SheetBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.SheetLoadBuilder, listdemo.SheetMeasure, listdemo.SheetSave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Army", (*listdemo.ArmyBuilder).Init, (*listdemo.ArmyBuilder).Shutdown, (*listdemo.ArmyBuilder).GetRoot, func(b *listdemo.ArmyBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.ArmyLoadBuilder, listdemo.ArmyMeasure, listdemo.ArmySave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Save", (*listdemo.SaveBuilder).Init, (*listdemo.SaveBuilder).Shutdown, (*listdemo.SaveBuilder).GetRoot, func(b *listdemo.SaveBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.SaveLoadBuilder, listdemo.SaveMeasure, listdemo.SaveSave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Mixed", (*listdemo.MixedBuilder).Init, (*listdemo.MixedBuilder).Shutdown, (*listdemo.MixedBuilder).GetRoot, func(b *listdemo.MixedBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.MixedLoadBuilder, listdemo.MixedMeasure, listdemo.MixedSave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Bytes", (*listdemo.BytesBuilder).Init, (*listdemo.BytesBuilder).Shutdown, (*listdemo.BytesBuilder).GetRoot, func(b *listdemo.BytesBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.BytesLoadBuilder, listdemo.BytesMeasure, listdemo.BytesSave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Ints", (*listdemo.IntsBuilder).Init, (*listdemo.IntsBuilder).Shutdown, (*listdemo.IntsBuilder).GetRoot, func(b *listdemo.IntsBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.IntsLoadBuilder, listdemo.IntsMeasure, listdemo.IntsSave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Floats", (*listdemo.FloatsBuilder).Init, (*listdemo.FloatsBuilder).Shutdown, (*listdemo.FloatsBuilder).GetRoot, func(b *listdemo.FloatsBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.FloatsLoadBuilder, listdemo.FloatsMeasure, listdemo.FloatsSave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Album", (*listdemo.AlbumBuilder).Init, (*listdemo.AlbumBuilder).Shutdown, (*listdemo.AlbumBuilder).GetRoot, func(b *listdemo.AlbumBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.AlbumLoadBuilder, listdemo.AlbumMeasure, listdemo.AlbumSave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("listdemo", "Unbounded", (*listdemo.UnboundedBuilder).Init, (*listdemo.UnboundedBuilder).Shutdown, (*listdemo.UnboundedBuilder).GetRoot, func(b *listdemo.UnboundedBuilder) listdemo.TableWriteContext { return &b.Arena }, listdemo.UnboundedLoadBuilder, listdemo.UnboundedMeasure, listdemo.UnboundedSave, func(r *listdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
	}),
	builderRow("streamdemo", "Feed", (*streamdemo.FeedBuilder).Init, (*streamdemo.FeedBuilder).Shutdown, (*streamdemo.FeedBuilder).GetRoot, func(b *streamdemo.FeedBuilder) streamdemo.TableWriteContext { return &b.Arena }, streamdemo.FeedLoadBuilder, streamdemo.FeedMeasure, streamdemo.FeedSave, func(r *streamdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
	}),
	builderRow("streamdemo", "Chunk", (*streamdemo.ChunkBuilder).Init, (*streamdemo.ChunkBuilder).Shutdown, (*streamdemo.ChunkBuilder).GetRoot, func(b *streamdemo.ChunkBuilder) streamdemo.TableWriteContext { return &b.Arena }, streamdemo.ChunkLoadBuilder, streamdemo.ChunkMeasure, streamdemo.ChunkSave, func(r *streamdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
	}),
	builderRow("blobdemo", "Catalog", (*blobdemo.CatalogBuilder).Init, (*blobdemo.CatalogBuilder).Shutdown, (*blobdemo.CatalogBuilder).GetRoot, func(b *blobdemo.CatalogBuilder) blobdemo.TableWriteContext { return &b.Arena }, blobdemo.CatalogLoadBuilder, blobdemo.CatalogMeasure, blobdemo.CatalogSave, func(r *blobdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
	}),
	builderRow("blobdemo", "Asset", (*blobdemo.AssetBuilder).Init, (*blobdemo.AssetBuilder).Shutdown, (*blobdemo.AssetBuilder).GetRoot, func(b *blobdemo.AssetBuilder) blobdemo.TableWriteContext { return &b.Arena }, blobdemo.AssetLoadBuilder, blobdemo.AssetMeasure, blobdemo.AssetSave, func(r *blobdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
	}),
	builderRow("graphdemo", "Scene", (*graphdemo.SceneBuilder).Init, (*graphdemo.SceneBuilder).Shutdown, (*graphdemo.SceneBuilder).GetRoot, func(b *graphdemo.SceneBuilder) graphdemo.TableWriteContext { return &b.Arena }, graphdemo.SceneLoadBuilder, graphdemo.SceneMeasure, graphdemo.SceneSave, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),
	builderRow("graphdemo", "ListNode", (*graphdemo.ListNodeBuilder).Init, (*graphdemo.ListNodeBuilder).Shutdown, (*graphdemo.ListNodeBuilder).GetRoot, func(b *graphdemo.ListNodeBuilder) graphdemo.TableWriteContext { return &b.Arena }, graphdemo.ListNodeLoadBuilder, graphdemo.ListNodeMeasure, graphdemo.ListNodeSave, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),
	builderRow("graphdemo", "TreeNode", (*graphdemo.TreeNodeBuilder).Init, (*graphdemo.TreeNodeBuilder).Shutdown, (*graphdemo.TreeNodeBuilder).GetRoot, func(b *graphdemo.TreeNodeBuilder) graphdemo.TableWriteContext { return &b.Arena }, graphdemo.TreeNodeLoadBuilder, graphdemo.TreeNodeMeasure, graphdemo.TreeNodeSave, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),
	builderRow("graphdemo", "Layer", (*graphdemo.LayerBuilder).Init, (*graphdemo.LayerBuilder).Shutdown, (*graphdemo.LayerBuilder).GetRoot, func(b *graphdemo.LayerBuilder) graphdemo.TableWriteContext { return &b.Arena }, graphdemo.LayerLoadBuilder, graphdemo.LayerMeasure, graphdemo.LayerSave, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),
	builderRow("graphdemo", "Depot", (*graphdemo.DepotBuilder).Init, (*graphdemo.DepotBuilder).Shutdown, (*graphdemo.DepotBuilder).GetRoot, func(b *graphdemo.DepotBuilder) graphdemo.TableWriteContext { return &b.Arena }, graphdemo.DepotLoadBuilder, graphdemo.DepotMeasure, graphdemo.DepotSave, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),
	builderRow("graphdemo", "Album", (*graphdemo.AlbumBuilder).Init, (*graphdemo.AlbumBuilder).Shutdown, (*graphdemo.AlbumBuilder).GetRoot, func(b *graphdemo.AlbumBuilder) graphdemo.TableWriteContext { return &b.Arena }, graphdemo.AlbumLoadBuilder, graphdemo.AlbumMeasure, graphdemo.AlbumSave, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),
	builderRow("graphdemo", "Marker", (*graphdemo.MarkerBuilder).Init, (*graphdemo.MarkerBuilder).Shutdown, (*graphdemo.MarkerBuilder).GetRoot, func(b *graphdemo.MarkerBuilder) graphdemo.TableWriteContext { return &b.Arena }, graphdemo.MarkerLoadBuilder, graphdemo.MarkerMeasure, graphdemo.MarkerSave, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),
}
