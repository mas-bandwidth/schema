// THE STRICT-REASON SURFACE (test/conformance/README.md): the strict read's
// refusal reason for every wire a `report` or `framing` line names.
//
// Each row binds one root's generated <Root>ReadStrict (docs/SPEC-TABLES.md
// §6.5), the one-call strict read that answers one typed reason. The driver
// only SPELLS that reason as the refusal-names enum member; it never decides
// one.
package main

import (
	"os"
	"strings"

	"backenddemo"
	"blobdemo"
	"cards"
	"featurewitnesses"
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
	"vocab9demo"
	"vocabdemo"
	widedemo "widedemo"
	"widelistdemo"
)

type strictCodec struct {
	unit string
	root string
	read func([]byte) error
}

func strictRow[T any](unit, root string, read func([]byte) (*T, error)) strictCodec {
	return strictCodec{unit: unit, root: root, read: func(wire []byte) error {
		_, err := read(wire)
		return err
	}}
}

var strictTable = []strictCodec{
	strictRow("tblp2", "Chain", tblp2.ChainReadStrict),
	strictRow("tblw1", "Fleet", tblw1.FleetReadStrict),
	strictRow("tblw2", "Fleet", tblw2.FleetReadStrict),
	strictRow("tblg1", "Guarded", tblg1.GuardedReadStrict),
	strictRow("backenddemo", "LoginRequest", backenddemo.LoginRequestReadStrict),
	strictRow("backenddemo", "MatchResult", backenddemo.MatchResultReadStrict),
	strictRow("backenddemo", "StorePurchase", backenddemo.StorePurchaseReadStrict),
	strictRow("backenddemo", "Envelope", backenddemo.EnvelopeReadStrict),
	strictRow("vocabdemo", "Wide00", vocabdemo.Wide00ReadStrict),
	strictRow("vocabdemo", "Wide09", vocabdemo.Wide09ReadStrict),
	strictRow("vocab9demo", "Wide00", vocab9demo.Wide00ReadStrict),
	strictRow("vocab9demo", "Wide19", vocab9demo.Wide19ReadStrict),
	strictRow("mapdemo", "Cells", mapdemo.CellsReadStrict),
	strictRow("mapdemo", "Chunks", mapdemo.ChunksReadStrict),
	strictRow("mapdemo", "Crews", mapdemo.CrewsReadStrict),
	strictRow("mapdemo", "Depth", mapdemo.DepthReadStrict),
	strictRow("mapdemo", "Docs", mapdemo.DocsReadStrict),
	strictRow("mapdemo", "Fleet", mapdemo.FleetReadStrict),
	strictRow("mapdemo", "Pairs", mapdemo.PairsReadStrict),
	strictRow("mapdemo", "Slots", mapdemo.SlotsReadStrict),
	strictRow("mapdemo", "Spans", mapdemo.SpansReadStrict),
	strictRow("mapdemo", "Text", mapdemo.TextReadStrict),
	strictRow("mapdemo", "Trails", mapdemo.TrailsReadStrict),
	strictRow("mapdemo", "Runs", mapdemo.RunsReadStrict),
	strictRow("mapdemo", "Row", mapdemo.RowReadStrict),
	strictRow("mapdemo", "WideRow", mapdemo.WideRowReadStrict),
	strictRow("mapdemo", "EdgeRow", mapdemo.EdgeRowReadStrict),
	strictRow("widelist", "Wide", widelistdemo.WideReadStrict),
	strictRow("featurewitnesses", "Root", featurewitnesses.RootReadStrict),
	strictRow("featurewitnesses", "ArrayRoot", featurewitnesses.ArrayRootReadStrict),
	strictRow("listdemo", "Sheet", listdemo.SheetReadStrict),
	strictRow("listdemo", "Army", listdemo.ArmyReadStrict),
	strictRow("listdemo", "Squad", listdemo.SquadReadStrict),
	strictRow("listdemo", "Save", listdemo.SaveReadStrict),
	strictRow("listdemo", "Mixed", listdemo.MixedReadStrict),
	strictRow("listdemo", "Bytes", listdemo.BytesReadStrict),
	strictRow("listdemo", "Ints", listdemo.IntsReadStrict),
	strictRow("listdemo", "Floats", listdemo.FloatsReadStrict),
	strictRow("listdemo", "Album", listdemo.AlbumReadStrict),
	strictRow("listdemo", "Unbounded", listdemo.UnboundedReadStrict),
	strictRow("streamdemo", "Feed", streamdemo.FeedReadStrict),
	strictRow("streamdemo", "Chunk", streamdemo.ChunkReadStrict),
	strictRow("blobdemo", "Catalog", blobdemo.CatalogReadStrict),
	strictRow("blobdemo", "Asset", blobdemo.AssetReadStrict),
	strictRow("graphdemo", "Scene", graphdemo.SceneReadStrict),
	strictRow("graphdemo", "ListNode", graphdemo.ListNodeReadStrict),
	strictRow("graphdemo", "TreeNode", graphdemo.TreeNodeReadStrict),
	strictRow("graphdemo", "Layer", graphdemo.LayerReadStrict),
	strictRow("graphdemo", "Depot", graphdemo.DepotReadStrict),
	strictRow("graphdemo", "Album", graphdemo.AlbumReadStrict),
	strictRow("graphdemo", "Marker", graphdemo.MarkerReadStrict),
	strictRow("messagedemo", "ToolMessage", messagedemo.ToolMessageReadStrict),
	strictRow("widedemo", "Caption", widedemo.CaptionReadStrict),
	strictRow("widedemo", "Stamp", widedemo.StampReadStrict),
	strictRow("scalars", "SimState", scalardemo.SimStateReadStrict),
	strictRow("tblscalars2", "SimState", tblscalars2.SimStateReadStrict),
	strictRow("tblm1", "Msg", tblm1.MsgReadStrict),
	strictRow("tblm2", "Msg", tblm2.MsgReadStrict),
	strictRow("tbla1", "Root", tbla1.RootReadStrict),
	strictRow("tbla2", "Root", tbla2.RootReadStrict),
	strictRow("tblk1", "Root", tblk1.RootReadStrict),
	strictRow("tblk2", "Root", tblk2.RootReadStrict),
	strictRow("listdemo", "Bounded", listdemo.BoundedReadStrict),
	strictRow("tblr1", "Cfg", tblr1.CfgReadStrict),
	strictRow("tblr2", "Cfg", tblr2.CfgReadStrict),
	strictRow("tabledemo", "RootConfig", tabledemo.RootConfigReadStrict),
	strictRow("tabledemo", "ProfileConfig", tabledemo.ProfileConfigReadStrict),
	strictRow("tabledemo", "LoadoutConfig", tabledemo.LoadoutConfigReadStrict),
	strictRow("tabledemo", "WideBlob", tabledemo.WideBlobReadStrict),
	strictRow("tabledemo", "ArchiveConfig", tabledemo.ArchiveConfigReadStrict),
	strictRow("tabledemo", "PackConfig", tabledemo.PackConfigReadStrict),
	strictRow("tabledemo", "KeyedConfig", tabledemo.KeyedConfigReadStrict),
	strictRow("tblv1", "Cfg", tblv1.CfgReadStrict),
	strictRow("tblv2", "Cfg", tblv2.CfgReadStrict),
	strictRow("tblp1", "Chain", tblp1.ChainReadStrict),
	strictRow("tblp3", "Chain", tblp3.ChainReadStrict),
	strictRow("cards", "Card", cards.CardReadStrict),
}

func findStrict(unit, root string) *strictCodec {
	for i := range strictTable {
		if strictTable[i].unit == unit && strictTable[i].root == root {
			return &strictTable[i]
		}
	}
	return nil
}

// reasonName is the refusal-names enum member a strict read's error carries.
// A typed error's text is the member, then ": " and its detail (the field a
// clamp or a malformation names, the fields a missing_required lacks), and
// the member alone is the answer.
func reasonName(err error) string {
	name, _, _ := strings.Cut(err.Error(), ": ")
	return name
}

// surfaceStrictReason runs the strict read of each `report` and `framing`
// line's wire at the line's root and writes the reason it names, or `ok` for
// a read the strict rule accepts. A derived line is
// `<report|framing> <case> <unit> <root> <wire> [no-value]`.
func surfaceStrictReason(lines []line, out string) error {
	for _, f := range lines {
		if f[0] != "report" && f[0] != "framing" {
			continue
		}
		s := findStrict(f[2], f[3])
		if s == nil {
			if err := spillAbsent(out, f[1]); err != nil {
				return err
			}
			continue
		}
		wire, err := os.ReadFile(f[4])
		if err != nil {
			return err
		}
		answer := "ok\n"
		if refusal := s.read(wire); refusal != nil {
			answer = reasonName(refusal) + "\n"
		}
		if err := spill(out, f[1], []byte(answer)); err != nil {
			return err
		}
	}
	return nil
}
