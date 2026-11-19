using System;

static partial class Program
{
    static bool? builderLoaded;
    static unsafe void RegisterBuilders()
    {
        Find("tblvarevo1","EvoDoc").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblvarevo1.EvoDocBuilder();
            var inner=new Tblvarevo1.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Tblvarevo1.EvoDoc(); }
            return Tblvarevo1.Schema.TableWire.CopyRegion(Tblvarevo1.Schema.EvoDocTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("tblvarevo2","EvoDoc").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblvarevo2.EvoDocBuilder();
            var inner=new Tblvarevo2.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Tblvarevo2.EvoDoc(); }
            return Tblvarevo2.Schema.TableWire.CopyRegion(Tblvarevo2.Schema.EvoDocTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("tblvarevo3","EvoDoc").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblvarevo3.EvoDocBuilder();
            var inner=new Tblvarevo3.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Tblvarevo3.EvoDoc(); }
            return Tblvarevo3.Schema.TableWire.CopyRegion(Tblvarevo3.Schema.EvoDocTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("cooktorture","TortureCook").PartialLoad=(bytes,report)=>
        {
            using var builder=new Cooktorture.TortureCookBuilder();
            var inner=new Cooktorture.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Cooktorture.TortureCook(); }
            return Cooktorture.Schema.TableWire.CopyRegion(Cooktorture.Schema.TortureCookTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("cooktorturewide","TortureWideCook").PartialLoad=(bytes,report)=>
        {
            using var builder=new Cooktorturewide.TortureWideCookBuilder();
            var inner=new Cooktorturewide.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Cooktorturewide.TortureWideCook(); }
            return Cooktorturewide.Schema.TableWire.CopyRegion(Cooktorturewide.Schema.TortureWideCookTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("widelist","Wide").PartialLoad=(bytes,report)=>
        {
            using var builder=new Widelistdemo.WideBuilder();
            var inner=new Widelistdemo.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Widelistdemo.Wide(); }
            return Widelistdemo.Schema.TableWire.CopyRegion(Widelistdemo.Schema.WideTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("featurewitnesses","Root").PartialLoad=(bytes,report)=>
        {
            using var builder=new Featurewitnesses.RootBuilder();
            var inner=new Featurewitnesses.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Featurewitnesses.Schema.TableWire.CopyRegion(Featurewitnesses.Schema.RootTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("featurewitnesses","ArrayRoot").PartialLoad=(bytes,report)=>
        {
            using var builder=new Featurewitnesses.ArrayRootBuilder();
            var inner=new Featurewitnesses.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Featurewitnesses.Schema.TableWire.CopyRegion(Featurewitnesses.Schema.ArrayRootTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Unbounded").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.UnboundedBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.UnboundedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Ints").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.IntsBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.IntsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Floats").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.FloatsBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.FloatsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Bytes").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.BytesBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.BytesTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("blobdemo","Catalog").PartialLoad=(bytes,report)=>
        {
            using var builder=new Blobdemo.CatalogBuilder();
            var inner=new Blobdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Blobdemo.Schema.TableWire.CopyRegion(Blobdemo.Schema.CatalogTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("graphdemo","Album").PartialLoad=(bytes,report)=>
        {
            using var builder=new Graphdemo.AlbumBuilder();
            var inner=new Graphdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Graphdemo.Schema.TableWire.CopyRegion(Graphdemo.Schema.AlbumTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("graphdemo","Depot").PartialLoad=(bytes,report)=>
        {
            using var builder=new Graphdemo.DepotBuilder();
            var inner=new Graphdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Graphdemo.Schema.TableWire.CopyRegion(Graphdemo.Schema.DepotTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("graphdemo","Scene").PartialLoad=(bytes,report)=>
        {
            using var builder=new Graphdemo.SceneBuilder();
            var inner=new Graphdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Graphdemo.Schema.TableWire.CopyRegion(Graphdemo.Schema.SceneTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Album").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.AlbumBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.AlbumTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Army").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.ArmyBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.ArmyTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Mixed").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.MixedBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.MixedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Save").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.SaveBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.SaveTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("listdemo","Sheet").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.SheetBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.SheetTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Cells").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.CellsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.CellsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Chunks").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.ChunksBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.ChunksTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Crews").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.CrewsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.CrewsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Depth").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.DepthBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.DepthTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Docs").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.DocsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.DocsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","EdgeRow").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.EdgeRowBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.EdgeRowTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Fleet").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.FleetBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.FleetTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Pairs").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.PairsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.PairsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Row").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.RowBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.RowTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Runs").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.RunsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.RunsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Slots").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.SlotsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.SlotsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Spans").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.SpansBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.SpansTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Text").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.TextBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.TextTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","Trails").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.TrailsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.TrailsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("mapdemo","WideRow").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.WideRowBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.WideRowTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("messagedemo","ToolEnvelope").PartialLoad=(bytes,report)=>
        {
            using var builder=new Messagedemo.ToolEnvelopeBuilder();
            var inner=new Messagedemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Messagedemo.Schema.TableWire.CopyRegion(Messagedemo.Schema.ToolEnvelopeTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("streamdemo","Feed").PartialLoad=(bytes,report)=>
        {
            using var builder=new Streamdemo.FeedBuilder();
            var inner=new Streamdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Streamdemo.Schema.TableWire.CopyRegion(Streamdemo.Schema.FeedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("tblg1","Guarded").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblg1.GuardedBuilder();
            var inner=new Tblg1.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Tblg1.Schema.TableWire.CopyRegion(Tblg1.Schema.GuardedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("tblp2","Chain").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblp2.ChainBuilder();
            var inner=new Tblp2.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Tblp2.Schema.TableWire.CopyRegion(Tblp2.Schema.ChainTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("tblw1","Fleet").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblw1.FleetBuilder();
            var inner=new Tblw1.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Tblw1.Schema.TableWire.CopyRegion(Tblw1.Schema.FleetTableType(),(IntPtr)builder.GetRoot(),true);
        };
        Find("tblw2","Fleet").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblw2.FleetBuilder();
            var inner=new Tblw2.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Tblw2.Schema.TableWire.CopyRegion(Tblw2.Schema.FleetTableType(),(IntPtr)builder.GetRoot(),true);
        };
    }
}
