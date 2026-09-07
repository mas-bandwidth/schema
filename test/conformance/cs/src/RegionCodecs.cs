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
        {
            Codec c=Find("blobdemo","Catalog"); var fallback=c.MessageLoad;
            var own=new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(own,c.Announcement,new Blobdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Blobdemo.TableVocabulary(); Blobdemo.Schema.AnnounceRead(vocabulary,announcement,new Blobdemo.TableReport()); }
                long need=Blobdemo.Schema.CatalogLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Blobdemo.TableReport();
                    Blobdemo.Schema.CatalogLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Blobdemo.Catalog():Blobdemo.Schema.CatalogLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Album"); var fallback=c.MessageLoad;
            var own=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(own,c.Announcement,new Graphdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(vocabulary,announcement,new Graphdemo.TableReport()); }
                long need=Graphdemo.Schema.AlbumLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Graphdemo.TableReport();
                    Graphdemo.Schema.AlbumLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Graphdemo.Album():Graphdemo.Schema.AlbumLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Depot"); var fallback=c.MessageLoad;
            var own=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(own,c.Announcement,new Graphdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(vocabulary,announcement,new Graphdemo.TableReport()); }
                long need=Graphdemo.Schema.DepotLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Graphdemo.TableReport();
                    Graphdemo.Schema.DepotLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Graphdemo.Depot():Graphdemo.Schema.DepotLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Scene"); var fallback=c.MessageLoad;
            var own=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(own,c.Announcement,new Graphdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Graphdemo.TableVocabulary(); Graphdemo.Schema.AnnounceRead(vocabulary,announcement,new Graphdemo.TableReport()); }
                long need=Graphdemo.Schema.SceneLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Graphdemo.TableReport();
                    Graphdemo.Schema.SceneLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Graphdemo.Scene():Graphdemo.Schema.SceneLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Album"); var fallback=c.MessageLoad;
            var own=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(own,c.Announcement,new Listdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,announcement,new Listdemo.TableReport()); }
                long need=Listdemo.Schema.AlbumLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Listdemo.TableReport();
                    Listdemo.Schema.AlbumLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Listdemo.Album():Listdemo.Schema.AlbumLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Army"); var fallback=c.MessageLoad;
            var own=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(own,c.Announcement,new Listdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,announcement,new Listdemo.TableReport()); }
                long need=Listdemo.Schema.ArmyLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Listdemo.TableReport();
                    Listdemo.Schema.ArmyLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Listdemo.Army():Listdemo.Schema.ArmyLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Mixed"); var fallback=c.MessageLoad;
            var own=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(own,c.Announcement,new Listdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,announcement,new Listdemo.TableReport()); }
                long need=Listdemo.Schema.MixedLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Listdemo.TableReport();
                    Listdemo.Schema.MixedLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Listdemo.Mixed():Listdemo.Schema.MixedLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Save"); var fallback=c.MessageLoad;
            var own=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(own,c.Announcement,new Listdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,announcement,new Listdemo.TableReport()); }
                long need=Listdemo.Schema.SaveLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Listdemo.TableReport();
                    Listdemo.Schema.SaveLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Listdemo.Save():Listdemo.Schema.SaveLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Sheet"); var fallback=c.MessageLoad;
            var own=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(own,c.Announcement,new Listdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Listdemo.TableVocabulary(); Listdemo.Schema.AnnounceRead(vocabulary,announcement,new Listdemo.TableReport()); }
                long need=Listdemo.Schema.SheetLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Listdemo.TableReport();
                    Listdemo.Schema.SheetLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Listdemo.Sheet():Listdemo.Schema.SheetLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Cells"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.CellsLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.CellsLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Cells():Mapdemo.Schema.CellsLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Chunks"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.ChunksLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.ChunksLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Chunks():Mapdemo.Schema.ChunksLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Crews"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.CrewsLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.CrewsLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Crews():Mapdemo.Schema.CrewsLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Depth"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.DepthLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.DepthLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Depth():Mapdemo.Schema.DepthLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Docs"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.DocsLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.DocsLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Docs():Mapdemo.Schema.DocsLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","EdgeRow"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.EdgeRowLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.EdgeRowLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.EdgeRow():Mapdemo.Schema.EdgeRowLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Fleet"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.FleetLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.FleetLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Fleet():Mapdemo.Schema.FleetLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Pairs"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.PairsLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.PairsLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Pairs():Mapdemo.Schema.PairsLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Row"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.RowLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.RowLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Row():Mapdemo.Schema.RowLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Runs"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.RunsLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.RunsLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Runs():Mapdemo.Schema.RunsLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Slots"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.SlotsLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.SlotsLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Slots():Mapdemo.Schema.SlotsLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Spans"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.SpansLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.SpansLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Spans():Mapdemo.Schema.SpansLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Text"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.TextLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.TextLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Text():Mapdemo.Schema.TextLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Trails"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.TrailsLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.TrailsLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.Trails():Mapdemo.Schema.TrailsLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","WideRow"); var fallback=c.MessageLoad;
            var own=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(own,c.Announcement,new Mapdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Mapdemo.TableVocabulary(); Mapdemo.Schema.AnnounceRead(vocabulary,announcement,new Mapdemo.TableReport()); }
                long need=Mapdemo.Schema.WideRowLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Mapdemo.TableReport();
                    Mapdemo.Schema.WideRowLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Mapdemo.WideRow():Mapdemo.Schema.WideRowLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("streamdemo","Feed"); var fallback=c.MessageLoad;
            var own=new Streamdemo.TableVocabulary(); Streamdemo.Schema.AnnounceRead(own,c.Announcement,new Streamdemo.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Streamdemo.TableVocabulary(); Streamdemo.Schema.AnnounceRead(vocabulary,announcement,new Streamdemo.TableReport()); }
                long need=Streamdemo.Schema.FeedLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Streamdemo.TableReport();
                    Streamdemo.Schema.FeedLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Streamdemo.Feed():Streamdemo.Schema.FeedLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblg1","Guarded"); var fallback=c.MessageLoad;
            var own=new Tblg1.TableVocabulary(); Tblg1.Schema.AnnounceRead(own,c.Announcement,new Tblg1.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Tblg1.TableVocabulary(); Tblg1.Schema.AnnounceRead(vocabulary,announcement,new Tblg1.TableReport()); }
                long need=Tblg1.Schema.GuardedLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Tblg1.TableReport();
                    Tblg1.Schema.GuardedLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Tblg1.Guarded():Tblg1.Schema.GuardedLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblp2","Chain"); var fallback=c.MessageLoad;
            var own=new Tblp2.TableVocabulary(); Tblp2.Schema.AnnounceRead(own,c.Announcement,new Tblp2.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Tblp2.TableVocabulary(); Tblp2.Schema.AnnounceRead(vocabulary,announcement,new Tblp2.TableReport()); }
                long need=Tblp2.Schema.ChainLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Tblp2.TableReport();
                    Tblp2.Schema.ChainLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Tblp2.Chain():Tblp2.Schema.ChainLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblw1","Fleet"); var fallback=c.MessageLoad;
            var own=new Tblw1.TableVocabulary(); Tblw1.Schema.AnnounceRead(own,c.Announcement,new Tblw1.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Tblw1.TableVocabulary(); Tblw1.Schema.AnnounceRead(vocabulary,announcement,new Tblw1.TableReport()); }
                long need=Tblw1.Schema.FleetLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Tblw1.TableReport();
                    Tblw1.Schema.FleetLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Tblw1.Fleet():Tblw1.Schema.FleetLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblw2","Fleet"); var fallback=c.MessageLoad;
            var own=new Tblw2.TableVocabulary(); Tblw2.Schema.AnnounceRead(own,c.Announcement,new Tblw2.TableReport());
            c.MessageLoad=(announcement,bytes,report)=>
            {
                var vocabulary=own;
                if(!ReferenceEquals(announcement,c.Announcement)) { vocabulary=new Tblw2.TableVocabulary(); Tblw2.Schema.AnnounceRead(vocabulary,announcement,new Tblw2.TableReport()); }
                long need=Tblw2.Schema.FleetLoadMeasure(vocabulary,bytes);
                if(need<0) { return fallback(announcement,bytes,report); }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5); IntPtr[] roots=new IntPtr[256];
                    var inner=new Tblw2.TableReport();
                    Tblw2.Schema.FleetLoadMessages((IntPtr)data,need,roots,bytes,vocabulary,inner,out _);
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native message overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return roots[0]==IntPtr.Zero?new Tblw2.Fleet():Tblw2.Schema.FleetLoadBuilder(roots[0]);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
    }
}
