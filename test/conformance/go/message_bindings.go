package main

import (
	"blobdemo"
	"graphdemo"
	"listdemo"
	"mapdemo"
	"messagedemo"
	"scalardemo"
	"streamdemo"
	"tabledemo"
	"tbla1"
	"tbla2"
	"tblg1"
	"tblk1"
	"tblk2"
	"tblm1"
	"tblm2"
	"tblp1"
	"tblp2"
	"tblp3"
	"tblr1"
	"tblr2"
	tblscalars2 "tblscalars2"
	"tblv1"
	"tblv2"
	"tblw1"
	"tblw2"
	widedemo "widedemo"
)

func init() {
	messageCodecs = append(messageCodecs, []messageCodec{
		regionMessageRow("tblp2", "Chain", func() *tblp2.TableVocabulary {
			v := new(tblp2.TableVocabulary)
			v.Init(make([]tblp2.TableMessageEntry, tblp2.TableMessageEntriesHere))
			return v
		}, tblp2.AnnounceMeasure, tblp2.Announce, tblp2.AnnounceRead, tblp2.ChainLoadMessagesMeasure, tblp2.ChainLoadMessages, tblp2.ChainMeasureMessages, tblp2.ChainSaveMessages, func(r *tblp2.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp2.TableOpenRefused}
		}),
		regionMessageRow("tblw1", "Fleet", func() *tblw1.TableVocabulary {
			v := new(tblw1.TableVocabulary)
			v.Init(make([]tblw1.TableMessageEntry, tblw1.TableMessageEntriesHere))
			return v
		}, tblw1.AnnounceMeasure, tblw1.Announce, tblw1.AnnounceRead, tblw1.FleetLoadMessagesMeasure, tblw1.FleetLoadMessages, tblw1.FleetMeasureMessages, tblw1.FleetSaveMessages, func(r *tblw1.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw1.TableOpenRefused}
		}),
		regionMessageRow("tblw2", "Fleet", func() *tblw2.TableVocabulary {
			v := new(tblw2.TableVocabulary)
			v.Init(make([]tblw2.TableMessageEntry, tblw2.TableMessageEntriesHere))
			return v
		}, tblw2.AnnounceMeasure, tblw2.Announce, tblw2.AnnounceRead, tblw2.FleetLoadMessagesMeasure, tblw2.FleetLoadMessages, tblw2.FleetMeasureMessages, tblw2.FleetSaveMessages, func(r *tblw2.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblw2.TableOpenRefused}
		}),
		regionMessageRow("tblg1", "Guarded", func() *tblg1.TableVocabulary {
			v := new(tblg1.TableVocabulary)
			v.Init(make([]tblg1.TableMessageEntry, tblg1.TableMessageEntriesHere))
			return v
		}, tblg1.AnnounceMeasure, tblg1.Announce, tblg1.AnnounceRead, tblg1.GuardedLoadMessagesMeasure, tblg1.GuardedLoadMessages, tblg1.GuardedMeasureMessages, tblg1.GuardedSaveMessages, func(r *tblg1.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblg1.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Cells", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.CellsLoadMessagesMeasure, mapdemo.CellsLoadMessages, mapdemo.CellsMeasureMessages, mapdemo.CellsSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Chunks", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.ChunksLoadMessagesMeasure, mapdemo.ChunksLoadMessages, mapdemo.ChunksMeasureMessages, mapdemo.ChunksSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Crews", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.CrewsLoadMessagesMeasure, mapdemo.CrewsLoadMessages, mapdemo.CrewsMeasureMessages, mapdemo.CrewsSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Depth", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.DepthLoadMessagesMeasure, mapdemo.DepthLoadMessages, mapdemo.DepthMeasureMessages, mapdemo.DepthSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Docs", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.DocsLoadMessagesMeasure, mapdemo.DocsLoadMessages, mapdemo.DocsMeasureMessages, mapdemo.DocsSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Fleet", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.FleetLoadMessagesMeasure, mapdemo.FleetLoadMessages, mapdemo.FleetMeasureMessages, mapdemo.FleetSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Pairs", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.PairsLoadMessagesMeasure, mapdemo.PairsLoadMessages, mapdemo.PairsMeasureMessages, mapdemo.PairsSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Slots", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.SlotsLoadMessagesMeasure, mapdemo.SlotsLoadMessages, mapdemo.SlotsMeasureMessages, mapdemo.SlotsSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Spans", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.SpansLoadMessagesMeasure, mapdemo.SpansLoadMessages, mapdemo.SpansMeasureMessages, mapdemo.SpansSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Text", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.TextLoadMessagesMeasure, mapdemo.TextLoadMessages, mapdemo.TextMeasureMessages, mapdemo.TextSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Trails", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.TrailsLoadMessagesMeasure, mapdemo.TrailsLoadMessages, mapdemo.TrailsMeasureMessages, mapdemo.TrailsSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Runs", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.RunsLoadMessagesMeasure, mapdemo.RunsLoadMessages, mapdemo.RunsMeasureMessages, mapdemo.RunsSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "Row", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.RowLoadMessagesMeasure, mapdemo.RowLoadMessages, mapdemo.RowMeasureMessages, mapdemo.RowSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "WideRow", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.WideRowLoadMessagesMeasure, mapdemo.WideRowLoadMessages, mapdemo.WideRowMeasureMessages, mapdemo.WideRowSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("mapdemo", "EdgeRow", func() *mapdemo.TableVocabulary {
			v := new(mapdemo.TableVocabulary)
			v.Init(make([]mapdemo.TableMessageEntry, mapdemo.TableMessageEntriesHere))
			return v
		}, mapdemo.AnnounceMeasure, mapdemo.Announce, mapdemo.AnnounceRead, mapdemo.EdgeRowLoadMessagesMeasure, mapdemo.EdgeRowLoadMessages, mapdemo.EdgeRowMeasureMessages, mapdemo.EdgeRowSaveMessages, func(r *mapdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == mapdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Sheet", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.SheetLoadMessagesMeasure, listdemo.SheetLoadMessages, listdemo.SheetMeasureMessages, listdemo.SheetSaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Army", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.ArmyLoadMessagesMeasure, listdemo.ArmyLoadMessages, listdemo.ArmyMeasureMessages, listdemo.ArmySaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Save", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.SaveLoadMessagesMeasure, listdemo.SaveLoadMessages, listdemo.SaveMeasureMessages, listdemo.SaveSaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Mixed", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.MixedLoadMessagesMeasure, listdemo.MixedLoadMessages, listdemo.MixedMeasureMessages, listdemo.MixedSaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Bytes", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.BytesLoadMessagesMeasure, listdemo.BytesLoadMessages, listdemo.BytesMeasureMessages, listdemo.BytesSaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Ints", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.IntsLoadMessagesMeasure, listdemo.IntsLoadMessages, listdemo.IntsMeasureMessages, listdemo.IntsSaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Floats", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.FloatsLoadMessagesMeasure, listdemo.FloatsLoadMessages, listdemo.FloatsMeasureMessages, listdemo.FloatsSaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Album", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.AlbumLoadMessagesMeasure, listdemo.AlbumLoadMessages, listdemo.AlbumMeasureMessages, listdemo.AlbumSaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("listdemo", "Unbounded", func() *listdemo.TableVocabulary {
			v := new(listdemo.TableVocabulary)
			v.Init(make([]listdemo.TableMessageEntry, listdemo.TableMessageEntriesHere))
			return v
		}, listdemo.AnnounceMeasure, listdemo.Announce, listdemo.AnnounceRead, listdemo.UnboundedLoadMessagesMeasure, listdemo.UnboundedLoadMessages, listdemo.UnboundedMeasureMessages, listdemo.UnboundedSaveMessages, func(r *listdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == listdemo.TableOpenRefused}
		}),
		regionMessageRow("streamdemo", "Feed", func() *streamdemo.TableVocabulary {
			v := new(streamdemo.TableVocabulary)
			v.Init(make([]streamdemo.TableMessageEntry, streamdemo.TableMessageEntriesHere))
			return v
		}, streamdemo.AnnounceMeasure, streamdemo.Announce, streamdemo.AnnounceRead, streamdemo.FeedLoadMessagesMeasure, streamdemo.FeedLoadMessages, streamdemo.FeedMeasureMessages, streamdemo.FeedSaveMessages, func(r *streamdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
		}),
		regionMessageRow("streamdemo", "Chunk", func() *streamdemo.TableVocabulary {
			v := new(streamdemo.TableVocabulary)
			v.Init(make([]streamdemo.TableMessageEntry, streamdemo.TableMessageEntriesHere))
			return v
		}, streamdemo.AnnounceMeasure, streamdemo.Announce, streamdemo.AnnounceRead, streamdemo.ChunkLoadMessagesMeasure, streamdemo.ChunkLoadMessages, streamdemo.ChunkMeasureMessages, streamdemo.ChunkSaveMessages, func(r *streamdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
		}),
		regionMessageRow("blobdemo", "Asset", func() *blobdemo.TableVocabulary {
			v := new(blobdemo.TableVocabulary)
			v.Init(make([]blobdemo.TableMessageEntry, blobdemo.TableMessageEntriesHere))
			return v
		}, blobdemo.AnnounceMeasure, blobdemo.Announce, blobdemo.AnnounceRead, blobdemo.AssetLoadMessagesMeasure, blobdemo.AssetLoadMessages, blobdemo.AssetMeasureMessages, blobdemo.AssetSaveMessages, func(r *blobdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
		}),
		regionMessageRow("graphdemo", "ListNode", func() *graphdemo.TableVocabulary {
			v := new(graphdemo.TableVocabulary)
			v.Init(make([]graphdemo.TableMessageEntry, graphdemo.TableMessageEntriesHere))
			return v
		}, graphdemo.AnnounceMeasure, graphdemo.Announce, graphdemo.AnnounceRead, graphdemo.ListNodeLoadMessagesMeasure, graphdemo.ListNodeLoadMessages, graphdemo.ListNodeMeasureMessages, graphdemo.ListNodeSaveMessages, func(r *graphdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
		}),
		regionMessageRow("graphdemo", "TreeNode", func() *graphdemo.TableVocabulary {
			v := new(graphdemo.TableVocabulary)
			v.Init(make([]graphdemo.TableMessageEntry, graphdemo.TableMessageEntriesHere))
			return v
		}, graphdemo.AnnounceMeasure, graphdemo.Announce, graphdemo.AnnounceRead, graphdemo.TreeNodeLoadMessagesMeasure, graphdemo.TreeNodeLoadMessages, graphdemo.TreeNodeMeasureMessages, graphdemo.TreeNodeSaveMessages, func(r *graphdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
		}),
		regionMessageRow("graphdemo", "Layer", func() *graphdemo.TableVocabulary {
			v := new(graphdemo.TableVocabulary)
			v.Init(make([]graphdemo.TableMessageEntry, graphdemo.TableMessageEntriesHere))
			return v
		}, graphdemo.AnnounceMeasure, graphdemo.Announce, graphdemo.AnnounceRead, graphdemo.LayerLoadMessagesMeasure, graphdemo.LayerLoadMessages, graphdemo.LayerMeasureMessages, graphdemo.LayerSaveMessages, func(r *graphdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
		}),
		regionMessageRow("graphdemo", "Depot", func() *graphdemo.TableVocabulary {
			v := new(graphdemo.TableVocabulary)
			v.Init(make([]graphdemo.TableMessageEntry, graphdemo.TableMessageEntriesHere))
			return v
		}, graphdemo.AnnounceMeasure, graphdemo.Announce, graphdemo.AnnounceRead, graphdemo.DepotLoadMessagesMeasure, graphdemo.DepotLoadMessages, graphdemo.DepotMeasureMessages, graphdemo.DepotSaveMessages, func(r *graphdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
		}),
		regionMessageRow("graphdemo", "Album", func() *graphdemo.TableVocabulary {
			v := new(graphdemo.TableVocabulary)
			v.Init(make([]graphdemo.TableMessageEntry, graphdemo.TableMessageEntriesHere))
			return v
		}, graphdemo.AnnounceMeasure, graphdemo.Announce, graphdemo.AnnounceRead, graphdemo.AlbumLoadMessagesMeasure, graphdemo.AlbumLoadMessages, graphdemo.AlbumMeasureMessages, graphdemo.AlbumSaveMessages, func(r *graphdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
		}),
		regionMessageRow("graphdemo", "Marker", func() *graphdemo.TableVocabulary {
			v := new(graphdemo.TableVocabulary)
			v.Init(make([]graphdemo.TableMessageEntry, graphdemo.TableMessageEntriesHere))
			return v
		}, graphdemo.AnnounceMeasure, graphdemo.Announce, graphdemo.AnnounceRead, graphdemo.MarkerLoadMessagesMeasure, graphdemo.MarkerLoadMessages, graphdemo.MarkerMeasureMessages, graphdemo.MarkerSaveMessages, func(r *graphdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
		}),
		messageRow("messagedemo", "ToolMessage", messagedemo.ToolMessageReset, func() *messagedemo.TableVocabulary {
			v := new(messagedemo.TableVocabulary)
			v.Init(make([]messagedemo.TableMessageEntry, messagedemo.TableMessageEntriesHere))
			return v
		}, messagedemo.AnnounceMeasure, messagedemo.Announce, messagedemo.AnnounceRead, messagedemo.ToolMessageLoadMessages, messagedemo.ToolMessageMeasureMessages, messagedemo.ToolMessageSaveMessages, func(r *messagedemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == messagedemo.TableOpenRefused}
		}),
		messageRow("widedemo", "Caption", widedemo.CaptionReset, func() *widedemo.TableVocabulary {
			v := new(widedemo.TableVocabulary)
			v.Init(make([]widedemo.TableMessageEntry, widedemo.TableMessageEntriesHere))
			return v
		}, widedemo.AnnounceMeasure, widedemo.Announce, widedemo.AnnounceRead, widedemo.CaptionLoadMessages, widedemo.CaptionMeasureMessages, widedemo.CaptionSaveMessages, func(r *widedemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == widedemo.TableOpenRefused}
		}),
		messageRow("widedemo", "Stamp", widedemo.StampReset, func() *widedemo.TableVocabulary {
			v := new(widedemo.TableVocabulary)
			v.Init(make([]widedemo.TableMessageEntry, widedemo.TableMessageEntriesHere))
			return v
		}, widedemo.AnnounceMeasure, widedemo.Announce, widedemo.AnnounceRead, widedemo.StampLoadMessages, widedemo.StampMeasureMessages, widedemo.StampSaveMessages, func(r *widedemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == widedemo.TableOpenRefused}
		}),
		messageRow("scalars", "SimState", scalardemo.SimStateReset, func() *scalardemo.TableVocabulary {
			v := new(scalardemo.TableVocabulary)
			v.Init(make([]scalardemo.TableMessageEntry, scalardemo.TableMessageEntriesHere))
			return v
		}, scalardemo.AnnounceMeasure, scalardemo.Announce, scalardemo.AnnounceRead, scalardemo.SimStateLoadMessages, scalardemo.SimStateMeasureMessages, scalardemo.SimStateSaveMessages, func(r *scalardemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == scalardemo.TableOpenRefused}
		}),
		messageRow("tblscalars2", "SimState", tblscalars2.SimStateReset, func() *tblscalars2.TableVocabulary {
			v := new(tblscalars2.TableVocabulary)
			v.Init(make([]tblscalars2.TableMessageEntry, tblscalars2.TableMessageEntriesHere))
			return v
		}, tblscalars2.AnnounceMeasure, tblscalars2.Announce, tblscalars2.AnnounceRead, tblscalars2.SimStateLoadMessages, tblscalars2.SimStateMeasureMessages, tblscalars2.SimStateSaveMessages, func(r *tblscalars2.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblscalars2.TableOpenRefused}
		}),
		messageRow("tblm1", "Msg", tblm1.MsgReset, func() *tblm1.TableVocabulary {
			v := new(tblm1.TableVocabulary)
			v.Init(make([]tblm1.TableMessageEntry, tblm1.TableMessageEntriesHere))
			return v
		}, tblm1.AnnounceMeasure, tblm1.Announce, tblm1.AnnounceRead, tblm1.MsgLoadMessages, tblm1.MsgMeasureMessages, tblm1.MsgSaveMessages, func(r *tblm1.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblm1.TableOpenRefused}
		}),
		messageRow("tblm2", "Msg", tblm2.MsgReset, func() *tblm2.TableVocabulary {
			v := new(tblm2.TableVocabulary)
			v.Init(make([]tblm2.TableMessageEntry, tblm2.TableMessageEntriesHere))
			return v
		}, tblm2.AnnounceMeasure, tblm2.Announce, tblm2.AnnounceRead, tblm2.MsgLoadMessages, tblm2.MsgMeasureMessages, tblm2.MsgSaveMessages, func(r *tblm2.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblm2.TableOpenRefused}
		}),
		messageRow("tbla1", "Root", tbla1.RootReset, func() *tbla1.TableVocabulary {
			v := new(tbla1.TableVocabulary)
			v.Init(make([]tbla1.TableMessageEntry, tbla1.TableMessageEntriesHere))
			return v
		}, tbla1.AnnounceMeasure, tbla1.Announce, tbla1.AnnounceRead, tbla1.RootLoadMessages, tbla1.RootMeasureMessages, tbla1.RootSaveMessages, func(r *tbla1.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tbla1.TableOpenRefused}
		}),
		messageRow("tbla2", "Root", tbla2.RootReset, func() *tbla2.TableVocabulary {
			v := new(tbla2.TableVocabulary)
			v.Init(make([]tbla2.TableMessageEntry, tbla2.TableMessageEntriesHere))
			return v
		}, tbla2.AnnounceMeasure, tbla2.Announce, tbla2.AnnounceRead, tbla2.RootLoadMessages, tbla2.RootMeasureMessages, tbla2.RootSaveMessages, func(r *tbla2.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tbla2.TableOpenRefused}
		}),
		messageRow("tblk1", "Root", tblk1.RootReset, func() *tblk1.TableVocabulary {
			v := new(tblk1.TableVocabulary)
			v.Init(make([]tblk1.TableMessageEntry, tblk1.TableMessageEntriesHere))
			return v
		}, tblk1.AnnounceMeasure, tblk1.Announce, tblk1.AnnounceRead, tblk1.RootLoadMessages, tblk1.RootMeasureMessages, tblk1.RootSaveMessages, func(r *tblk1.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblk1.TableOpenRefused}
		}),
		messageRow("tblk2", "Root", tblk2.RootReset, func() *tblk2.TableVocabulary {
			v := new(tblk2.TableVocabulary)
			v.Init(make([]tblk2.TableMessageEntry, tblk2.TableMessageEntriesHere))
			return v
		}, tblk2.AnnounceMeasure, tblk2.Announce, tblk2.AnnounceRead, tblk2.RootLoadMessages, tblk2.RootMeasureMessages, tblk2.RootSaveMessages, func(r *tblk2.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblk2.TableOpenRefused}
		}),
		messageRow("tblr1", "Cfg", tblr1.CfgReset, func() *tblr1.TableVocabulary {
			v := new(tblr1.TableVocabulary)
			v.Init(make([]tblr1.TableMessageEntry, tblr1.TableMessageEntriesHere))
			return v
		}, tblr1.AnnounceMeasure, tblr1.Announce, tblr1.AnnounceRead, tblr1.CfgLoadMessages, tblr1.CfgMeasureMessages, tblr1.CfgSaveMessages, func(r *tblr1.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblr1.TableOpenRefused}
		}),
		messageRow("tblr2", "Cfg", tblr2.CfgReset, func() *tblr2.TableVocabulary {
			v := new(tblr2.TableVocabulary)
			v.Init(make([]tblr2.TableMessageEntry, tblr2.TableMessageEntriesHere))
			return v
		}, tblr2.AnnounceMeasure, tblr2.Announce, tblr2.AnnounceRead, tblr2.CfgLoadMessages, tblr2.CfgMeasureMessages, tblr2.CfgSaveMessages, func(r *tblr2.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblr2.TableOpenRefused}
		}),
		messageRow("tabledemo", "RootConfig", tabledemo.RootConfigReset, func() *tabledemo.TableVocabulary {
			v := new(tabledemo.TableVocabulary)
			v.Init(make([]tabledemo.TableMessageEntry, tabledemo.TableMessageEntriesHere))
			return v
		}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.RootConfigLoadMessages, tabledemo.RootConfigMeasureMessages, tabledemo.RootConfigSaveMessages, func(r *tabledemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused}
		}),
		messageRow("tabledemo", "ProfileConfig", tabledemo.ProfileConfigReset, func() *tabledemo.TableVocabulary {
			v := new(tabledemo.TableVocabulary)
			v.Init(make([]tabledemo.TableMessageEntry, tabledemo.TableMessageEntriesHere))
			return v
		}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.ProfileConfigLoadMessages, tabledemo.ProfileConfigMeasureMessages, tabledemo.ProfileConfigSaveMessages, func(r *tabledemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused}
		}),
		messageRow("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigReset, func() *tabledemo.TableVocabulary {
			v := new(tabledemo.TableVocabulary)
			v.Init(make([]tabledemo.TableMessageEntry, tabledemo.TableMessageEntriesHere))
			return v
		}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.LoadoutConfigLoadMessages, tabledemo.LoadoutConfigMeasureMessages, tabledemo.LoadoutConfigSaveMessages, func(r *tabledemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused}
		}),
		messageRow("tabledemo", "WideBlob", tabledemo.WideBlobReset, func() *tabledemo.TableVocabulary {
			v := new(tabledemo.TableVocabulary)
			v.Init(make([]tabledemo.TableMessageEntry, tabledemo.TableMessageEntriesHere))
			return v
		}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.WideBlobLoadMessages, tabledemo.WideBlobMeasureMessages, tabledemo.WideBlobSaveMessages, func(r *tabledemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused}
		}),
		messageRow("tabledemo", "ArchiveConfig", tabledemo.ArchiveConfigReset, func() *tabledemo.TableVocabulary {
			v := new(tabledemo.TableVocabulary)
			v.Init(make([]tabledemo.TableMessageEntry, tabledemo.TableMessageEntriesHere))
			return v
		}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.ArchiveConfigLoadMessages, tabledemo.ArchiveConfigMeasureMessages, tabledemo.ArchiveConfigSaveMessages, func(r *tabledemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused}
		}),
		messageRow("tabledemo", "PackConfig", tabledemo.PackConfigReset, func() *tabledemo.TableVocabulary {
			v := new(tabledemo.TableVocabulary)
			v.Init(make([]tabledemo.TableMessageEntry, tabledemo.TableMessageEntriesHere))
			return v
		}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.PackConfigLoadMessages, tabledemo.PackConfigMeasureMessages, tabledemo.PackConfigSaveMessages, func(r *tabledemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused}
		}),
		messageRow("tabledemo", "KeyedConfig", tabledemo.KeyedConfigReset, func() *tabledemo.TableVocabulary {
			v := new(tabledemo.TableVocabulary)
			v.Init(make([]tabledemo.TableMessageEntry, tabledemo.TableMessageEntriesHere))
			return v
		}, tabledemo.AnnounceMeasure, tabledemo.Announce, tabledemo.AnnounceRead, tabledemo.KeyedConfigLoadMessages, tabledemo.KeyedConfigMeasureMessages, tabledemo.KeyedConfigSaveMessages, func(r *tabledemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tabledemo.TableOpenRefused}
		}),
		messageRow("tblv1", "Cfg", tblv1.CfgReset, func() *tblv1.TableVocabulary {
			v := new(tblv1.TableVocabulary)
			v.Init(make([]tblv1.TableMessageEntry, tblv1.TableMessageEntriesHere))
			return v
		}, tblv1.AnnounceMeasure, tblv1.Announce, tblv1.AnnounceRead, tblv1.CfgLoadMessages, tblv1.CfgMeasureMessages, tblv1.CfgSaveMessages, func(r *tblv1.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblv1.TableOpenRefused}
		}),
		messageRow("tblv2", "Cfg", tblv2.CfgReset, func() *tblv2.TableVocabulary {
			v := new(tblv2.TableVocabulary)
			v.Init(make([]tblv2.TableMessageEntry, tblv2.TableMessageEntriesHere))
			return v
		}, tblv2.AnnounceMeasure, tblv2.Announce, tblv2.AnnounceRead, tblv2.CfgLoadMessages, tblv2.CfgMeasureMessages, tblv2.CfgSaveMessages, func(r *tblv2.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblv2.TableOpenRefused}
		}),
		messageRow("tblp1", "Chain", tblp1.ChainReset, func() *tblp1.TableVocabulary {
			v := new(tblp1.TableVocabulary)
			v.Init(make([]tblp1.TableMessageEntry, tblp1.TableMessageEntriesHere))
			return v
		}, tblp1.AnnounceMeasure, tblp1.Announce, tblp1.AnnounceRead, tblp1.ChainLoadMessages, tblp1.ChainMeasureMessages, tblp1.ChainSaveMessages, func(r *tblp1.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp1.TableOpenRefused}
		}),
		messageRow("tblp3", "Chain", tblp3.ChainReset, func() *tblp3.TableVocabulary {
			v := new(tblp3.TableVocabulary)
			v.Init(make([]tblp3.TableMessageEntry, tblp3.TableMessageEntriesHere))
			return v
		}, tblp3.AnnounceMeasure, tblp3.Announce, tblp3.AnnounceRead, tblp3.ChainLoadMessages, tblp3.ChainMeasureMessages, tblp3.ChainSaveMessages, func(r *tblp3.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblp3.TableOpenRefused}
		}),
	}...)
}
