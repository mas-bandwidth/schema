using System;

static partial class Program
{
    static bool? builderLoaded;
    static unsafe void RegisterBuilders()
    {
        BindBuilder("tblvarevo1","EvoDoc").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblvarevo1.EvoDocBuilder();
            var inner=new Tblvarevo1.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Tblvarevo1.EvoDoc(); }
            return Tblvarevo1.Schema.TableWire.CopyRegion(Tblvarevo1.Schema.EvoDocTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblvarevo2","EvoDoc").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblvarevo2.EvoDocBuilder();
            var inner=new Tblvarevo2.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Tblvarevo2.EvoDoc(); }
            return Tblvarevo2.Schema.TableWire.CopyRegion(Tblvarevo2.Schema.EvoDocTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblvarevo3","EvoDoc").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblvarevo3.EvoDocBuilder();
            var inner=new Tblvarevo3.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Tblvarevo3.EvoDoc(); }
            return Tblvarevo3.Schema.TableWire.CopyRegion(Tblvarevo3.Schema.EvoDocTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("cooktorture","TortureCook").PartialLoad=(bytes,report)=>
        {
            using var builder=new Cooktorture.TortureCookBuilder();
            var inner=new Cooktorture.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Cooktorture.TortureCook(); }
            return Cooktorture.Schema.TableWire.CopyRegion(Cooktorture.Schema.TortureCookTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("cooktorturewide","TortureWideCook").PartialLoad=(bytes,report)=>
        {
            using var builder=new Cooktorturewide.TortureWideCookBuilder();
            var inner=new Cooktorturewide.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Cooktorturewide.TortureWideCook(); }
            return Cooktorturewide.Schema.TableWire.CopyRegion(Cooktorturewide.Schema.TortureWideCookTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("widelist","Wide").PartialLoad=(bytes,report)=>
        {
            using var builder=new Widelistdemo.WideBuilder();
            var inner=new Widelistdemo.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Widelistdemo.Wide(); }
            return Widelistdemo.Schema.TableWire.CopyRegion(Widelistdemo.Schema.WideTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("featurewitnesses","Root").PartialLoad=(bytes,report)=>
        {
            using var builder=new Featurewitnesses.RootBuilder();
            var inner=new Featurewitnesses.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Featurewitnesses.Schema.TableWire.CopyRegion(Featurewitnesses.Schema.RootTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("featurewitnesses","ArrayRoot").PartialLoad=(bytes,report)=>
        {
            using var builder=new Featurewitnesses.ArrayRootBuilder();
            var inner=new Featurewitnesses.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Featurewitnesses.Schema.TableWire.CopyRegion(Featurewitnesses.Schema.ArrayRootTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Unbounded").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.UnboundedBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.UnboundedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Ints").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.IntsBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.IntsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Floats").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.FloatsBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.FloatsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Bytes").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.BytesBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.BytesTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("blobdemo","Catalog").PartialLoad=(bytes,report)=>
        {
            using var builder=new Blobdemo.CatalogBuilder();
            var inner=new Blobdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Blobdemo.Schema.TableWire.CopyRegion(Blobdemo.Schema.CatalogTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("graphdemo","Album").PartialLoad=(bytes,report)=>
        {
            using var builder=new Graphdemo.AlbumBuilder();
            var inner=new Graphdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Graphdemo.Schema.TableWire.CopyRegion(Graphdemo.Schema.AlbumTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("graphdemo","Depot").PartialLoad=(bytes,report)=>
        {
            using var builder=new Graphdemo.DepotBuilder();
            var inner=new Graphdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Graphdemo.Schema.TableWire.CopyRegion(Graphdemo.Schema.DepotTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("graphdemo","Scene").PartialLoad=(bytes,report)=>
        {
            using var builder=new Graphdemo.SceneBuilder();
            var inner=new Graphdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Graphdemo.Schema.TableWire.CopyRegion(Graphdemo.Schema.SceneTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Album").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.AlbumBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.AlbumTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Army").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.ArmyBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.ArmyTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Mixed").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.MixedBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.MixedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Save").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.SaveBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.SaveTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("listdemo","Sheet").PartialLoad=(bytes,report)=>
        {
            using var builder=new Listdemo.SheetBuilder();
            var inner=new Listdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Listdemo.Schema.TableWire.CopyRegion(Listdemo.Schema.SheetTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Cells").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.CellsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.CellsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Chunks").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.ChunksBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.ChunksTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Crews").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.CrewsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.CrewsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Depth").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.DepthBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.DepthTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Docs").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.DocsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.DocsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","EdgeRow").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.EdgeRowBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.EdgeRowTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Fleet").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.FleetBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.FleetTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Pairs").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.PairsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.PairsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Row").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.RowBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.RowTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Runs").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.RunsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.RunsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Slots").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.SlotsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.SlotsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Spans").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.SpansBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.SpansTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Text").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.TextBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.TextTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","Trails").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.TrailsBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.TrailsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("mapdemo","WideRow").PartialLoad=(bytes,report)=>
        {
            using var builder=new Mapdemo.WideRowBuilder();
            var inner=new Mapdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Mapdemo.Schema.TableWire.CopyRegion(Mapdemo.Schema.WideRowTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("messagedemo","ToolEnvelope").PartialLoad=(bytes,report)=>
        {
            using var builder=new Messagedemo.ToolEnvelopeBuilder();
            var inner=new Messagedemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Messagedemo.Schema.TableWire.CopyRegion(Messagedemo.Schema.ToolEnvelopeTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("streamdemo","Feed").PartialLoad=(bytes,report)=>
        {
            using var builder=new Streamdemo.FeedBuilder();
            var inner=new Streamdemo.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Streamdemo.Schema.TableWire.CopyRegion(Streamdemo.Schema.FeedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblg1","Guarded").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblg1.GuardedBuilder();
            var inner=new Tblg1.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Tblg1.Schema.TableWire.CopyRegion(Tblg1.Schema.GuardedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblp2","Chain").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblp2.ChainBuilder();
            var inner=new Tblp2.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Tblp2.Schema.TableWire.CopyRegion(Tblp2.Schema.ChainTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblw1","Fleet").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblw1.FleetBuilder();
            var inner=new Tblw1.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Tblw1.Schema.TableWire.CopyRegion(Tblw1.Schema.FleetTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblw2","Fleet").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblw2.FleetBuilder();
            var inner=new Tblw2.TableReport();
            builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            return Tblw2.Schema.TableWire.CopyRegion(Tblw2.Schema.FleetTableType(),(IntPtr)builder.GetRoot(),true);
        };
        // THE ROOTS THE BUILDER ARM HAD NO BINDING FOR (shapes, tblsurf,
        // tblvaluedefaults, packetunions): each was claimed and then read by a
        // codec that reports no builder completion, so the driver threw on its
        // first mutant (a truncated shapes.Bounds) instead of answering.
        BindBuilder("shapes","Bounds").PartialLoad=(bytes,report)=>
        {
            using var builder=new Shapes.BoundsBuilder();
            var inner=new Shapes.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Shapes.Bounds(); }
            return Shapes.Schema.TableWire.CopyRegion(Shapes.Schema.BoundsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblsurf","Chain").PartialLoad=(bytes,report)=>
        {
            using var builder=new Surfaces.ChainBuilder();
            var inner=new Surfaces.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Surfaces.Chain(); }
            return Surfaces.Schema.TableWire.CopyRegion(Surfaces.Schema.ChainTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblsurf","Holder").PartialLoad=(bytes,report)=>
        {
            using var builder=new Surfaces.HolderBuilder();
            var inner=new Surfaces.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Surfaces.Holder(); }
            return Surfaces.Schema.TableWire.CopyRegion(Surfaces.Schema.HolderTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblsurf","Counted").PartialLoad=(bytes,report)=>
        {
            using var builder=new Surfaces.CountedBuilder();
            var inner=new Surfaces.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Surfaces.Counted(); }
            return Surfaces.Schema.TableWire.CopyRegion(Surfaces.Schema.CountedTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("tblvaluedefaults","VarDefaults").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblvaluedefaults.VarDefaultsBuilder();
            var inner=new Tblvaluedefaults.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Tblvaluedefaults.VarDefaults(); }
            return Tblvaluedefaults.Schema.TableWire.CopyRegion(Tblvaluedefaults.Schema.VarDefaultsTableType(),(IntPtr)builder.GetRoot(),true);
        };
        BindBuilder("packetunions","ShapeDoc").PartialLoad=(bytes,report)=>
        {
            using var builder=new Tblpacketunions.ShapeDocBuilder();
            var inner=new Tblpacketunions.TableReport(); builderLoaded=builder.Load(bytes,inner); Fill(report,Copy(inner));
            if(inner.Refused || inner.Damaged) { return new Tblpacketunions.ShapeDoc(); }
            return Tblpacketunions.Schema.TableWire.CopyRegion(Tblpacketunions.Schema.ShapeDocTableType(),(IntPtr)builder.GetRoot(),true);
        };
    }

    // A BUILDER BINDING MARKS ITS CODEC (Codec.Builder): the builder arm claims
    // a root only when it holds one, so a root with no builder read is answered
    // absent by name instead of being read by a codec that cannot say whether
    // the builder completed.
    static Codec BindBuilder(string unit, string root)
    {
        Codec c = Find(unit, root) ?? throw new InvalidOperationException($"no codec is registered for {unit}.{root}");
        c.Builder = true;
        return c;
    }
}
