using System;
using System.Runtime.InteropServices;

static partial class Program
{
    static unsafe void RegisterRegions()
    {
        {
            Codec c=Find("blobdemo","Catalog"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Blobdemo.Schema.CatalogLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Blobdemo.TableReport();
                    var root=Blobdemo.Schema.CatalogLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Blobdemo.Catalog():Blobdemo.Schema.CatalogLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Album"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Graphdemo.Schema.AlbumLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Graphdemo.TableReport();
                    var root=Graphdemo.Schema.AlbumLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Graphdemo.Album():Graphdemo.Schema.AlbumLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Depot"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Graphdemo.Schema.DepotLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Graphdemo.TableReport();
                    var root=Graphdemo.Schema.DepotLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Graphdemo.Depot():Graphdemo.Schema.DepotLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Scene"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Graphdemo.Schema.SceneLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Graphdemo.TableReport();
                    var root=Graphdemo.Schema.SceneLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Graphdemo.Scene():Graphdemo.Schema.SceneLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Album"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.AlbumLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.AlbumLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Album():Listdemo.Schema.AlbumLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Army"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.ArmyLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.ArmyLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Army():Listdemo.Schema.ArmyLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Mixed"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.MixedLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.MixedLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Mixed():Listdemo.Schema.MixedLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Save"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.SaveLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.SaveLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Save():Listdemo.Schema.SaveLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Sheet"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.SheetLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.SheetLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Sheet():Listdemo.Schema.SheetLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Cells"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.CellsLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.CellsLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Cells():Mapdemo.Schema.CellsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Chunks"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.ChunksLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.ChunksLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Chunks():Mapdemo.Schema.ChunksLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Crews"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.CrewsLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.CrewsLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Crews():Mapdemo.Schema.CrewsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Depth"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.DepthLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.DepthLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Depth():Mapdemo.Schema.DepthLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Docs"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.DocsLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.DocsLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Docs():Mapdemo.Schema.DocsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","EdgeRow"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.EdgeRowLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.EdgeRowLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.EdgeRow():Mapdemo.Schema.EdgeRowLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Fleet"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.FleetLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Fleet():Mapdemo.Schema.FleetLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Pairs"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.PairsLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.PairsLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Pairs():Mapdemo.Schema.PairsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Row"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.RowLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.RowLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Row():Mapdemo.Schema.RowLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Runs"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.RunsLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.RunsLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Runs():Mapdemo.Schema.RunsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Slots"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.SlotsLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.SlotsLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Slots():Mapdemo.Schema.SlotsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Spans"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.SpansLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.SpansLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Spans():Mapdemo.Schema.SpansLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Text"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.TextLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.TextLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Text():Mapdemo.Schema.TextLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Trails"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.TrailsLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.TrailsLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Trails():Mapdemo.Schema.TrailsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","WideRow"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.WideRowLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.WideRowLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.WideRow():Mapdemo.Schema.WideRowLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("streamdemo","Feed"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Streamdemo.Schema.FeedLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Streamdemo.TableReport();
                    var root=Streamdemo.Schema.FeedLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Streamdemo.Feed():Streamdemo.Schema.FeedLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblg1","Guarded"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Tblg1.Schema.GuardedLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Tblg1.TableReport();
                    var root=Tblg1.Schema.GuardedLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Tblg1.Guarded():Tblg1.Schema.GuardedLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblp2","Chain"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Tblp2.Schema.ChainLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Tblp2.TableReport();
                    var root=Tblp2.Schema.ChainLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Tblp2.Chain():Tblp2.Schema.ChainLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblw1","Fleet"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Tblw1.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Tblw1.TableReport();
                    var root=Tblw1.Schema.FleetLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Tblw1.Fleet():Tblw1.Schema.FleetLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblw2","Fleet"); var refused=c.PartialLoad;
            c.PartialLoad=(bytes,report)=>
            {
                long need=Tblw2.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return refused(bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Tblw2.TableReport();
                    var root=Tblw2.Schema.FleetLoad((IntPtr)data,need,bytes,inner);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Tblw2.Fleet():Tblw2.Schema.FleetLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
    }
}
