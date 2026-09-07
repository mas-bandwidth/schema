// THE CODEC TABLE: one row per (unit, root) the corpus names.
//
// Nothing here is an expectation — the rows only say which generated functions
// answer for which manifest key. The expectations all live in the DATA.
package main

import (
	"blobdemo"
	"graphdemo"
	"messagedemo"
	"scalardemo"
	"streamdemo"
	tblscalars2 "tblscalars2"
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
	regionRow("streamdemo", "Feed", streamdemo.FeedLoadMeasure, streamdemo.FeedLoad, streamdemo.FeedMeasure, streamdemo.FeedSave,
		func(text []byte, r *streamdemo.TableReport) (*streamdemo.Feed, []byte, bool) {
			var b streamdemo.FeedBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := streamdemo.FeedFromJson(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		streamdemo.FeedToJsonMeasure, streamdemo.FeedToJson, func(r *streamdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
		}),

	regionRow("streamdemo", "Chunk", streamdemo.ChunkLoadMeasure, streamdemo.ChunkLoad, streamdemo.ChunkMeasure, streamdemo.ChunkSave,
		func(text []byte, r *streamdemo.TableReport) (*streamdemo.Chunk, []byte, bool) {
			var b streamdemo.ChunkBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := streamdemo.ChunkFromJson(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		streamdemo.ChunkToJsonMeasure, streamdemo.ChunkToJson, func(r *streamdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == streamdemo.TableOpenRefused}
		}),

	regionRow("blobdemo", "Catalog", blobdemo.CatalogLoadMeasure, blobdemo.CatalogLoad, blobdemo.CatalogMeasure, blobdemo.CatalogSave,
		func(text []byte, r *blobdemo.TableReport) (*blobdemo.Catalog, []byte, bool) {
			var b blobdemo.CatalogBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := blobdemo.CatalogFromJson(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		blobdemo.CatalogToJsonMeasure, blobdemo.CatalogToJson, func(r *blobdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
		}),

	regionRow("blobdemo", "Asset", blobdemo.AssetLoadMeasure, blobdemo.AssetLoad, blobdemo.AssetMeasure, blobdemo.AssetSave,
		func(text []byte, r *blobdemo.TableReport) (*blobdemo.Asset, []byte, bool) {
			var b blobdemo.AssetBuilder
			if !b.Init() {
				return nil, nil, false
			}
			defer b.Shutdown()
			ok := blobdemo.AssetFromJson(&b, text, r)
			if !b.Lock() {
				return nil, nil, false
			}
			return b.AsConst(), b.Region(), ok
		},
		blobdemo.AssetToJsonMeasure, blobdemo.AssetToJson, func(r *blobdemo.TableReport) report {
			return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == blobdemo.TableOpenRefused}
		}),

	regionRow("graphdemo", "Scene", graphdemo.SceneLoadMeasure, graphdemo.SceneLoad, graphdemo.SceneMeasure, graphdemo.SceneSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Scene, []byte, bool) {
		var b graphdemo.SceneBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.SceneFromJson(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.SceneToJsonMeasure, graphdemo.SceneToJson, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "ListNode", graphdemo.ListNodeLoadMeasure, graphdemo.ListNodeLoad, graphdemo.ListNodeMeasure, graphdemo.ListNodeSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.ListNode, []byte, bool) {
		var b graphdemo.ListNodeBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.ListNodeFromJson(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.ListNodeToJsonMeasure, graphdemo.ListNodeToJson, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "TreeNode", graphdemo.TreeNodeLoadMeasure, graphdemo.TreeNodeLoad, graphdemo.TreeNodeMeasure, graphdemo.TreeNodeSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.TreeNode, []byte, bool) {
		var b graphdemo.TreeNodeBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.TreeNodeFromJson(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.TreeNodeToJsonMeasure, graphdemo.TreeNodeToJson, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "Layer", graphdemo.LayerLoadMeasure, graphdemo.LayerLoad, graphdemo.LayerMeasure, graphdemo.LayerSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Layer, []byte, bool) {
		var b graphdemo.LayerBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.LayerFromJson(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.LayerToJsonMeasure, graphdemo.LayerToJson, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "Depot", graphdemo.DepotLoadMeasure, graphdemo.DepotLoad, graphdemo.DepotMeasure, graphdemo.DepotSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Depot, []byte, bool) {
		var b graphdemo.DepotBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.DepotFromJson(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.DepotToJsonMeasure, graphdemo.DepotToJson, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "Album", graphdemo.AlbumLoadMeasure, graphdemo.AlbumLoad, graphdemo.AlbumMeasure, graphdemo.AlbumSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Album, []byte, bool) {
		var b graphdemo.AlbumBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.AlbumFromJson(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.AlbumToJsonMeasure, graphdemo.AlbumToJson, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	regionRow("graphdemo", "Marker", graphdemo.MarkerLoadMeasure, graphdemo.MarkerLoad, graphdemo.MarkerMeasure, graphdemo.MarkerSave, func(text []byte, report *graphdemo.TableReport) (*graphdemo.Marker, []byte, bool) {
		var b graphdemo.MarkerBuilder
		if !b.Init() {
			return nil, nil, false
		}
		defer b.Shutdown()
		ok := graphdemo.MarkerFromJson(&b, text, report)
		if !b.Lock() {
			return nil, nil, false
		}
		return b.AsConst(), b.Region(), ok
	}, graphdemo.MarkerToJsonMeasure, graphdemo.MarkerToJson, func(r *graphdemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == graphdemo.TableOpenRefused}
	}),

	row("messagedemo", "ToolMessage", messagedemo.ToolMessageReset, messagedemo.ToolMessageLoad, messagedemo.ToolMessageMeasure, messagedemo.ToolMessageSave, messagedemo.ToolMessageFromJson, messagedemo.ToolMessageToJsonMeasure, messagedemo.ToolMessageToJson, func(r *messagedemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == messagedemo.TableOpenRefused}
	}),

	row("widedemo", "Caption", widedemo.CaptionReset, widedemo.CaptionLoad, widedemo.CaptionMeasure, widedemo.CaptionSave, widedemo.CaptionFromJson, widedemo.CaptionToJsonMeasure, widedemo.CaptionToJson, func(r *widedemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == widedemo.TableOpenRefused}
	}),
	row("widedemo", "Stamp", widedemo.StampReset, widedemo.StampLoad, widedemo.StampMeasure, widedemo.StampSave, widedemo.StampFromJson, widedemo.StampToJsonMeasure, widedemo.StampToJson, func(r *widedemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == widedemo.TableOpenRefused}
	}),

	row("scalars", "SimState", scalardemo.SimStateReset, scalardemo.SimStateLoad, scalardemo.SimStateMeasure, scalardemo.SimStateSave, scalardemo.SimStateFromJson, scalardemo.SimStateToJsonMeasure, scalardemo.SimStateToJson, func(r *scalardemo.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == scalardemo.TableOpenRefused}
	}),
	row("tblscalars2", "SimState", tblscalars2.SimStateReset, tblscalars2.SimStateLoad, tblscalars2.SimStateMeasure, tblscalars2.SimStateSave, tblscalars2.SimStateFromJson, tblscalars2.SimStateToJsonMeasure, tblscalars2.SimStateToJson, func(r *tblscalars2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblscalars2.TableOpenRefused}
	}),

	row("tblm1", "Msg", tblm1.MsgReset, tblm1.MsgLoad, tblm1.MsgMeasure, tblm1.MsgSave, tblm1.MsgFromJson, tblm1.MsgToJsonMeasure, tblm1.MsgToJson, func(r *tblm1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblm1.TableOpenRefused}
	}),
	row("tblm2", "Msg", tblm2.MsgReset, tblm2.MsgLoad, tblm2.MsgMeasure, tblm2.MsgSave, tblm2.MsgFromJson, tblm2.MsgToJsonMeasure, tblm2.MsgToJson, func(r *tblm2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblm2.TableOpenRefused}
	}),
	row("tbla1", "Root", tbla1.RootReset, tbla1.RootLoad, tbla1.RootMeasure, tbla1.RootSave, tbla1.RootFromJson, tbla1.RootToJsonMeasure, tbla1.RootToJson, func(r *tbla1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tbla1.TableOpenRefused}
	}),
	row("tbla2", "Root", tbla2.RootReset, tbla2.RootLoad, tbla2.RootMeasure, tbla2.RootSave, tbla2.RootFromJson, tbla2.RootToJsonMeasure, tbla2.RootToJson, func(r *tbla2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tbla2.TableOpenRefused}
	}),
	row("tblk1", "Root", tblk1.RootReset, tblk1.RootLoad, tblk1.RootMeasure, tblk1.RootSave, tblk1.RootFromJson, tblk1.RootToJsonMeasure, tblk1.RootToJson, func(r *tblk1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblk1.TableOpenRefused}
	}),
	row("tblk2", "Root", tblk2.RootReset, tblk2.RootLoad, tblk2.RootMeasure, tblk2.RootSave, tblk2.RootFromJson, tblk2.RootToJsonMeasure, tblk2.RootToJson, func(r *tblk2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblk2.TableOpenRefused}
	}),
	row("tblr1", "Cfg", tblr1.CfgReset, tblr1.CfgLoad, tblr1.CfgMeasure, tblr1.CfgSave, tblr1.CfgFromJson, tblr1.CfgToJsonMeasure, tblr1.CfgToJson, func(r *tblr1.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblr1.TableOpenRefused}
	}),
	row("tblr2", "Cfg", tblr2.CfgReset, tblr2.CfgLoad, tblr2.CfgMeasure, tblr2.CfgSave, tblr2.CfgFromJson, tblr2.CfgToJsonMeasure, tblr2.CfgToJson, func(r *tblr2.TableReport) report {
		return report{r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed, r.Verdict == tblr2.TableOpenRefused}
	}),

	row("tabledemo", "RootConfig", tabledemo.RootConfigReset, tabledemo.RootConfigLoad,
		tabledemo.RootConfigMeasure, tabledemo.RootConfigSave,
		tabledemo.RootConfigFromJson, tabledemo.RootConfigToJsonMeasure, tabledemo.RootConfigToJson, snapDemo),
	row("tabledemo", "ProfileConfig", tabledemo.ProfileConfigReset, tabledemo.ProfileConfigLoad,
		tabledemo.ProfileConfigMeasure, tabledemo.ProfileConfigSave,
		tabledemo.ProfileConfigFromJson, tabledemo.ProfileConfigToJsonMeasure, tabledemo.ProfileConfigToJson, snapDemo),
	row("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigReset, tabledemo.LoadoutConfigLoad,
		tabledemo.LoadoutConfigMeasure, tabledemo.LoadoutConfigSave,
		tabledemo.LoadoutConfigFromJson, tabledemo.LoadoutConfigToJsonMeasure, tabledemo.LoadoutConfigToJson, snapDemo),
	row("tabledemo", "WideBlob", tabledemo.WideBlobReset, tabledemo.WideBlobLoad,
		tabledemo.WideBlobMeasure, tabledemo.WideBlobSave,
		tabledemo.WideBlobFromJson, tabledemo.WideBlobToJsonMeasure, tabledemo.WideBlobToJson, snapDemo),
	row("tabledemo", "ArchiveConfig", tabledemo.ArchiveConfigReset, tabledemo.ArchiveConfigLoad,
		tabledemo.ArchiveConfigMeasure, tabledemo.ArchiveConfigSave,
		tabledemo.ArchiveConfigFromJson, tabledemo.ArchiveConfigToJsonMeasure, tabledemo.ArchiveConfigToJson, snapDemo),
	row("tabledemo", "PackConfig", tabledemo.PackConfigReset, tabledemo.PackConfigLoad,
		tabledemo.PackConfigMeasure, tabledemo.PackConfigSave,
		tabledemo.PackConfigFromJson, tabledemo.PackConfigToJsonMeasure, tabledemo.PackConfigToJson, snapDemo),
	row("tabledemo", "KeyedConfig", tabledemo.KeyedConfigReset, tabledemo.KeyedConfigLoad,
		tabledemo.KeyedConfigMeasure, tabledemo.KeyedConfigSave,
		tabledemo.KeyedConfigFromJson, tabledemo.KeyedConfigToJsonMeasure, tabledemo.KeyedConfigToJson, snapDemo),
	row("tblv1", "Cfg", tblv1.CfgReset, tblv1.CfgLoad, tblv1.CfgMeasure, tblv1.CfgSave,
		tblv1.CfgFromJson, tblv1.CfgToJsonMeasure, tblv1.CfgToJson, snapV1),
	row("tblv2", "Cfg", tblv2.CfgReset, tblv2.CfgLoad, tblv2.CfgMeasure, tblv2.CfgSave,
		tblv2.CfgFromJson, tblv2.CfgToJsonMeasure, tblv2.CfgToJson, snapV2),
	row("tblp1", "Chain", tblp1.ChainReset, tblp1.ChainLoad, tblp1.ChainMeasure, tblp1.ChainSave,
		tblp1.ChainFromJson, tblp1.ChainToJsonMeasure, tblp1.ChainToJson, snapP1),
	row("tblp3", "Chain", tblp3.ChainReset, tblp3.ChainLoad, tblp3.ChainMeasure, tblp3.ChainSave,
		tblp3.ChainFromJson, tblp3.ChainToJsonMeasure, tblp3.ChainToJson, snapP3),
}

// surfaces is what this backend implements. A surface not listed prints as
// ABSENT in the matrix, which is a missing FEATURE and not a failing test.
func surfaces() []string {
	return []string{"wire", "report", "json-read", "json-write", "json-hostile", "cook", "cook-foreign", "block", "block-foreign", "block-dump", "forgery", "cook-forgery"}
}
