using System;
using System.Runtime.InteropServices;

static partial class Program
{
    static bool? regionLoaded;
    static unsafe void RegisterRegions()
    {
        {
            Codec c=Find("widelist","Wide");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Widelistdemo.Schema.WideLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Widelistdemo.TableReport();
                    regionLoaded=Widelistdemo.Schema.WideLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Widelistdemo.Wide();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Widelistdemo.TableReport();
                    var root=Widelistdemo.Schema.WideLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Widelistdemo.Wide():Widelistdemo.Schema.WideLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("featurewitnesses","Root");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Featurewitnesses.Schema.RootLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Featurewitnesses.TableReport();
                    regionLoaded=Featurewitnesses.Schema.RootLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Featurewitnesses.Root();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Featurewitnesses.TableReport();
                    var root=Featurewitnesses.Schema.RootLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Featurewitnesses.Root():Featurewitnesses.Schema.RootLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("featurewitnesses","ArrayRoot");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Featurewitnesses.Schema.ArrayRootLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Featurewitnesses.TableReport();
                    regionLoaded=Featurewitnesses.Schema.ArrayRootLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Featurewitnesses.ArrayRoot();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Featurewitnesses.TableReport();
                    var root=Featurewitnesses.Schema.ArrayRootLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Featurewitnesses.ArrayRoot():Featurewitnesses.Schema.ArrayRootLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Unbounded");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.UnboundedLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.UnboundedLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Unbounded();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.UnboundedLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Unbounded():Listdemo.Schema.UnboundedLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Ints");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.IntsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.IntsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Ints();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.IntsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Ints():Listdemo.Schema.IntsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Floats");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.FloatsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.FloatsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Floats();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.FloatsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Floats():Listdemo.Schema.FloatsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Bytes");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.BytesLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.BytesLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Bytes();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.BytesLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Bytes():Listdemo.Schema.BytesLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("blobdemo","Catalog");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Blobdemo.Schema.CatalogLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Blobdemo.TableReport();
                    regionLoaded=Blobdemo.Schema.CatalogLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Blobdemo.Catalog();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Blobdemo.TableReport();
                    var root=Blobdemo.Schema.CatalogLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Blobdemo.Catalog():Blobdemo.Schema.CatalogLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Album");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Graphdemo.Schema.AlbumLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Graphdemo.TableReport();
                    regionLoaded=Graphdemo.Schema.AlbumLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Graphdemo.Album();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Graphdemo.TableReport();
                    var root=Graphdemo.Schema.AlbumLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Graphdemo.Album():Graphdemo.Schema.AlbumLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Depot");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Graphdemo.Schema.DepotLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Graphdemo.TableReport();
                    regionLoaded=Graphdemo.Schema.DepotLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Graphdemo.Depot();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Graphdemo.TableReport();
                    var root=Graphdemo.Schema.DepotLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Graphdemo.Depot():Graphdemo.Schema.DepotLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("graphdemo","Scene");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Graphdemo.Schema.SceneLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Graphdemo.TableReport();
                    regionLoaded=Graphdemo.Schema.SceneLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Graphdemo.Scene();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Graphdemo.TableReport();
                    var root=Graphdemo.Schema.SceneLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Graphdemo.Scene():Graphdemo.Schema.SceneLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Album");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.AlbumLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.AlbumLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Album();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.AlbumLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Album():Listdemo.Schema.AlbumLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Army");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.ArmyLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.ArmyLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Army();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.ArmyLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Army():Listdemo.Schema.ArmyLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Mixed");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.MixedLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.MixedLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Mixed();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.MixedLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Mixed():Listdemo.Schema.MixedLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Save");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.SaveLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.SaveLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Save();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.SaveLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Save():Listdemo.Schema.SaveLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("listdemo","Sheet");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Listdemo.Schema.SheetLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Listdemo.TableReport();
                    regionLoaded=Listdemo.Schema.SheetLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Listdemo.Sheet();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Listdemo.TableReport();
                    var root=Listdemo.Schema.SheetLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Listdemo.Sheet():Listdemo.Schema.SheetLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Cells");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.CellsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.CellsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Cells();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.CellsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Cells():Mapdemo.Schema.CellsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Chunks");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.ChunksLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.ChunksLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Chunks();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.ChunksLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Chunks():Mapdemo.Schema.ChunksLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Crews");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.CrewsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.CrewsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Crews();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.CrewsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Crews():Mapdemo.Schema.CrewsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Depth");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.DepthLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.DepthLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Depth();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.DepthLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Depth():Mapdemo.Schema.DepthLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Docs");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.DocsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.DocsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Docs();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.DocsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Docs():Mapdemo.Schema.DocsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","EdgeRow");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.EdgeRowLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.EdgeRowLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.EdgeRow();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.EdgeRowLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.EdgeRow():Mapdemo.Schema.EdgeRowLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Fleet");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.FleetLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.FleetLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Fleet();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.FleetLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Fleet():Mapdemo.Schema.FleetLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Pairs");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.PairsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.PairsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Pairs();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.PairsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Pairs():Mapdemo.Schema.PairsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Row");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.RowLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.RowLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Row();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.RowLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Row():Mapdemo.Schema.RowLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Runs");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.RunsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.RunsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Runs();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.RunsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Runs():Mapdemo.Schema.RunsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Slots");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.SlotsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.SlotsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Slots();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.SlotsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Slots():Mapdemo.Schema.SlotsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Spans");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.SpansLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.SpansLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Spans();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.SpansLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Spans():Mapdemo.Schema.SpansLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Text");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.TextLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.TextLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Text();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.TextLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Text():Mapdemo.Schema.TextLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","Trails");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.TrailsLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.TrailsLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.Trails();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.TrailsLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.Trails():Mapdemo.Schema.TrailsLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("mapdemo","WideRow");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Mapdemo.Schema.WideRowLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Mapdemo.TableReport();
                    regionLoaded=Mapdemo.Schema.WideRowLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Mapdemo.WideRow();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Mapdemo.TableReport();
                    var root=Mapdemo.Schema.WideRowLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Mapdemo.WideRow():Mapdemo.Schema.WideRowLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("streamdemo","Feed");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Streamdemo.Schema.FeedLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Streamdemo.TableReport();
                    regionLoaded=Streamdemo.Schema.FeedLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Streamdemo.Feed();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Streamdemo.TableReport();
                    var root=Streamdemo.Schema.FeedLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Streamdemo.Feed():Streamdemo.Schema.FeedLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblg1","Guarded");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Tblg1.Schema.GuardedLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Tblg1.TableReport();
                    regionLoaded=Tblg1.Schema.GuardedLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Tblg1.Guarded();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Tblg1.TableReport();
                    var root=Tblg1.Schema.GuardedLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Tblg1.Guarded():Tblg1.Schema.GuardedLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblp2","Chain");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Tblp2.Schema.ChainLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Tblp2.TableReport();
                    regionLoaded=Tblp2.Schema.ChainLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Tblp2.Chain();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Tblp2.TableReport();
                    var root=Tblp2.Schema.ChainLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Tblp2.Chain():Tblp2.Schema.ChainLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblw1","Fleet");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Tblw1.Schema.FleetLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Tblw1.TableReport();
                    regionLoaded=Tblw1.Schema.FleetLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Tblw1.Fleet();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Tblw1.TableReport();
                    var root=Tblw1.Schema.FleetLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Tblw1.Fleet():Tblw1.Schema.FleetLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
        {
            Codec c=Find("tblw2","Fleet");
            c.PartialLoad=(bytes,report)=>
            {
                long need=Tblw2.Schema.FleetLoadMeasure(bytes);
                if(need<0)
                {
                    var inner=new Tblw2.TableReport();
                    regionLoaded=Tblw2.Schema.FleetLoad(IntPtr.Zero,0,bytes,inner)!=null;
                    Fill(report,Copy(inner));
                    return new Tblw2.Fleet();
                }
                byte* data=(byte*)NativeMemory.AlignedAlloc(checked((nuint)(need+64)),64);
                try
                {
                    new Span<byte>(data+need,64).Fill(0xa5);
                    var inner=new Tblw2.TableReport();
                    var root=Tblw2.Schema.FleetLoad((IntPtr)data,need,bytes,inner);
                    regionLoaded=root!=null;
                    for(int i=0;i<64;i++) { if(data[need+i]!=0xa5) { throw new InvalidOperationException("native load overran its measured region"); } }
                    Fill(report,Copy(inner));
                    return root==null?new Tblw2.Fleet():Tblw2.Schema.FleetLoadBuilder((IntPtr)data);
                }
                finally { NativeMemory.AlignedFree(data); }
            };
        }
    }
    static unsafe void RegisterNativeCooks()
    {
        {
            Codec c=Find("blobdemo","Catalog");
            c.NativeCook=(bytes,big)=>
            {
                long need=Blobdemo.Schema.CatalogLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Blobdemo.Schema.CatalogLoad((IntPtr)region,need,bytes,new Blobdemo.TableReport())==null) { return null; }
                    long size=Blobdemo.Schema.CatalogCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Blobdemo.Schema.CatalogCook((IntPtr)region,result,big?Blobdemo.TableByteOrder.Big:Blobdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Blobdemo.CatalogBuilder())
                    {
                        if(!Blobdemo.Schema.CatalogLoadBuilder(builder,bytes,new Blobdemo.TableReport())) { throw new InvalidOperationException("builder load: Blobdemo.Catalog"); }
                        long builderSize=builder.CookMeasure();
                        if(builderSize<0) { throw new InvalidOperationException("builder cook measure: Blobdemo.Catalog"); }
                        byte[] built=new byte[builderSize];
                        if(!builder.Cook(built,big?Blobdemo.TableByteOrder.Big:Blobdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Blobdemo.Catalog"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Blobdemo.TableByteOrder.Big:Blobdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Blobdemo.Catalog"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("graphdemo","Album");
            c.NativeCook=(bytes,big)=>
            {
                long need=Graphdemo.Schema.AlbumLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Graphdemo.Schema.AlbumLoad((IntPtr)region,need,bytes,new Graphdemo.TableReport())==null) { return null; }
                    long size=Graphdemo.Schema.AlbumCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Graphdemo.Schema.AlbumCook((IntPtr)region,result,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Graphdemo.AlbumBuilder())
                    {
                        if(!Graphdemo.Schema.AlbumLoadBuilder(builder,bytes,new Graphdemo.TableReport())) { throw new InvalidOperationException("builder load: Graphdemo.Album"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Graphdemo.Album"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Graphdemo.Album"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("graphdemo","Depot");
            c.NativeCook=(bytes,big)=>
            {
                long need=Graphdemo.Schema.DepotLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Graphdemo.Schema.DepotLoad((IntPtr)region,need,bytes,new Graphdemo.TableReport())==null) { return null; }
                    long size=Graphdemo.Schema.DepotCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Graphdemo.Schema.DepotCook((IntPtr)region,result,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Graphdemo.DepotBuilder())
                    {
                        if(!Graphdemo.Schema.DepotLoadBuilder(builder,bytes,new Graphdemo.TableReport())) { throw new InvalidOperationException("builder load: Graphdemo.Depot"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Graphdemo.Depot"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Graphdemo.Depot"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("graphdemo","Scene");
            c.NativeCook=(bytes,big)=>
            {
                long need=Graphdemo.Schema.SceneLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Graphdemo.Schema.SceneLoad((IntPtr)region,need,bytes,new Graphdemo.TableReport())==null) { return null; }
                    long size=Graphdemo.Schema.SceneCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Graphdemo.Schema.SceneCook((IntPtr)region,result,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Graphdemo.SceneBuilder())
                    {
                        if(!Graphdemo.Schema.SceneLoadBuilder(builder,bytes,new Graphdemo.TableReport())) { throw new InvalidOperationException("builder load: Graphdemo.Scene"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Graphdemo.Scene"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Graphdemo.Scene"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("widelist","Wide");
            c.NativeCook=(bytes,big)=>
            {
                long need=Widelistdemo.Schema.WideLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Widelistdemo.Schema.WideLoad((IntPtr)region,need,bytes,new Widelistdemo.TableReport())==null) { return null; }
                    long size=Widelistdemo.Schema.WideCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Widelistdemo.Schema.WideCook((IntPtr)region,result,big?Widelistdemo.TableByteOrder.Big:Widelistdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Widelistdemo.WideBuilder())
                    {
                        if(!Widelistdemo.Schema.WideLoadBuilder(builder,bytes,new Widelistdemo.TableReport())) { throw new InvalidOperationException("builder load: Widelistdemo.Wide"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Widelistdemo.TableByteOrder.Big:Widelistdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Widelistdemo.Wide"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Widelistdemo.TableByteOrder.Big:Widelistdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Widelistdemo.Wide"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Unbounded");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.UnboundedLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.UnboundedLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.UnboundedCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.UnboundedCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.UnboundedBuilder())
                    {
                        if(!Listdemo.Schema.UnboundedLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Unbounded"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Unbounded"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Unbounded"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Album");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.AlbumLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.AlbumLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.AlbumCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.AlbumCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.AlbumBuilder())
                    {
                        if(!Listdemo.Schema.AlbumLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Album"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Album"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Album"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Army");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.ArmyLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.ArmyLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.ArmyCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.ArmyCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.ArmyBuilder())
                    {
                        if(!Listdemo.Schema.ArmyLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Army"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Army"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Army"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Mixed");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.MixedLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.MixedLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.MixedCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.MixedCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.MixedBuilder())
                    {
                        if(!Listdemo.Schema.MixedLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Mixed"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Mixed"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Mixed"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Save");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.SaveLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.SaveLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.SaveCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.SaveCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.SaveBuilder())
                    {
                        if(!Listdemo.Schema.SaveLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Save"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Save"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Save"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Sheet");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.SheetLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.SheetLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.SheetCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.SheetCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.SheetBuilder())
                    {
                        if(!Listdemo.Schema.SheetLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Sheet"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Sheet"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Sheet"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Cells");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.CellsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.CellsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.CellsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.CellsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.CellsBuilder())
                    {
                        if(!Mapdemo.Schema.CellsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Cells"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Cells"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Cells"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Chunks");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.ChunksLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.ChunksLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.ChunksCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.ChunksCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.ChunksBuilder())
                    {
                        if(!Mapdemo.Schema.ChunksLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Chunks"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Chunks"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Chunks"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Crews");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.CrewsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.CrewsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.CrewsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.CrewsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.CrewsBuilder())
                    {
                        if(!Mapdemo.Schema.CrewsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Crews"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Crews"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Crews"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Depth");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.DepthLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.DepthLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.DepthCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.DepthCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.DepthBuilder())
                    {
                        if(!Mapdemo.Schema.DepthLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Depth"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Depth"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Depth"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Docs");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.DocsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.DocsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.DocsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.DocsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.DocsBuilder())
                    {
                        if(!Mapdemo.Schema.DocsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Docs"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Docs"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Docs"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","EdgeRow");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.EdgeRowLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.EdgeRowLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.EdgeRowCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.EdgeRowCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.EdgeRowBuilder())
                    {
                        if(!Mapdemo.Schema.EdgeRowLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.EdgeRow"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.EdgeRow"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.EdgeRow"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Fleet");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.FleetLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.FleetCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.FleetCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.FleetBuilder())
                    {
                        if(!Mapdemo.Schema.FleetLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Fleet"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Fleet"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Fleet"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Pairs");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.PairsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.PairsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.PairsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.PairsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.PairsBuilder())
                    {
                        if(!Mapdemo.Schema.PairsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Pairs"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Pairs"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Pairs"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Row");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.RowLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.RowLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.RowCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.RowCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.RowBuilder())
                    {
                        if(!Mapdemo.Schema.RowLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Row"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Row"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Row"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Runs");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.RunsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.RunsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.RunsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.RunsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.RunsBuilder())
                    {
                        if(!Mapdemo.Schema.RunsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Runs"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Runs"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Runs"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Slots");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.SlotsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.SlotsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.SlotsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.SlotsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.SlotsBuilder())
                    {
                        if(!Mapdemo.Schema.SlotsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Slots"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Slots"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Slots"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Spans");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.SpansLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.SpansLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.SpansCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.SpansCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.SpansBuilder())
                    {
                        if(!Mapdemo.Schema.SpansLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Spans"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Spans"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Spans"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Text");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.TextLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.TextLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.TextCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.TextCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.TextBuilder())
                    {
                        if(!Mapdemo.Schema.TextLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Text"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Text"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Text"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Trails");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.TrailsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.TrailsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.TrailsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.TrailsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.TrailsBuilder())
                    {
                        if(!Mapdemo.Schema.TrailsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Trails"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Trails"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Trails"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","WideRow");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.WideRowLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.WideRowLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.WideRowCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.WideRowCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.WideRowBuilder())
                    {
                        if(!Mapdemo.Schema.WideRowLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.WideRow"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.WideRow"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.WideRow"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("streamdemo","Feed");
            c.NativeCook=(bytes,big)=>
            {
                long need=Streamdemo.Schema.FeedLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Streamdemo.Schema.FeedLoad((IntPtr)region,need,bytes,new Streamdemo.TableReport())==null) { return null; }
                    long size=Streamdemo.Schema.FeedCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Streamdemo.Schema.FeedCook((IntPtr)region,result,big?Streamdemo.TableByteOrder.Big:Streamdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Streamdemo.FeedBuilder())
                    {
                        if(!Streamdemo.Schema.FeedLoadBuilder(builder,bytes,new Streamdemo.TableReport())) { throw new InvalidOperationException("builder load: Streamdemo.Feed"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Streamdemo.TableByteOrder.Big:Streamdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Streamdemo.Feed"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Streamdemo.TableByteOrder.Big:Streamdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Streamdemo.Feed"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("tblg1","Guarded");
            c.NativeCook=(bytes,big)=>
            {
                long need=Tblg1.Schema.GuardedLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Tblg1.Schema.GuardedLoad((IntPtr)region,need,bytes,new Tblg1.TableReport())==null) { return null; }
                    long size=Tblg1.Schema.GuardedCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Tblg1.Schema.GuardedCook((IntPtr)region,result,big?Tblg1.TableByteOrder.Big:Tblg1.TableByteOrder.Little)) { return null; }
                    using(var builder=new Tblg1.GuardedBuilder())
                    {
                        if(!Tblg1.Schema.GuardedLoadBuilder(builder,bytes,new Tblg1.TableReport())) { throw new InvalidOperationException("builder load: Tblg1.Guarded"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Tblg1.TableByteOrder.Big:Tblg1.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Tblg1.Guarded"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Tblg1.TableByteOrder.Big:Tblg1.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Tblg1.Guarded"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("tblp2","Chain");
            c.NativeCook=(bytes,big)=>
            {
                long need=Tblp2.Schema.ChainLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Tblp2.Schema.ChainLoad((IntPtr)region,need,bytes,new Tblp2.TableReport())==null) { return null; }
                    long size=Tblp2.Schema.ChainCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Tblp2.Schema.ChainCook((IntPtr)region,result,big?Tblp2.TableByteOrder.Big:Tblp2.TableByteOrder.Little)) { return null; }
                    using(var builder=new Tblp2.ChainBuilder())
                    {
                        if(!Tblp2.Schema.ChainLoadBuilder(builder,bytes,new Tblp2.TableReport())) { throw new InvalidOperationException("builder load: Tblp2.Chain"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Tblp2.TableByteOrder.Big:Tblp2.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Tblp2.Chain"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Tblp2.TableByteOrder.Big:Tblp2.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Tblp2.Chain"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("tblw1","Fleet");
            c.NativeCook=(bytes,big)=>
            {
                long need=Tblw1.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Tblw1.Schema.FleetLoad((IntPtr)region,need,bytes,new Tblw1.TableReport())==null) { return null; }
                    long size=Tblw1.Schema.FleetCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Tblw1.Schema.FleetCook((IntPtr)region,result,big?Tblw1.TableByteOrder.Big:Tblw1.TableByteOrder.Little)) { return null; }
                    using(var builder=new Tblw1.FleetBuilder())
                    {
                        if(!Tblw1.Schema.FleetLoadBuilder(builder,bytes,new Tblw1.TableReport())) { throw new InvalidOperationException("builder load: Tblw1.Fleet"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Tblw1.TableByteOrder.Big:Tblw1.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Tblw1.Fleet"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Tblw1.TableByteOrder.Big:Tblw1.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Tblw1.Fleet"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("tblw2","Fleet");
            c.NativeCook=(bytes,big)=>
            {
                long need=Tblw2.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Tblw2.Schema.FleetLoad((IntPtr)region,need,bytes,new Tblw2.TableReport())==null) { return null; }
                    long size=Tblw2.Schema.FleetCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Tblw2.Schema.FleetCook((IntPtr)region,result,big?Tblw2.TableByteOrder.Big:Tblw2.TableByteOrder.Little)) { return null; }
                    using(var builder=new Tblw2.FleetBuilder())
                    {
                        if(!Tblw2.Schema.FleetLoadBuilder(builder,bytes,new Tblw2.TableReport())) { throw new InvalidOperationException("builder load: Tblw2.Fleet"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Tblw2.TableByteOrder.Big:Tblw2.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Tblw2.Fleet"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Tblw2.TableByteOrder.Big:Tblw2.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Tblw2.Fleet"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("blobdemo","Catalog");
            c.NativeCook=(bytes,big)=>
            {
                long need=Blobdemo.Schema.CatalogLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Blobdemo.Schema.CatalogLoad((IntPtr)region,need,bytes,new Blobdemo.TableReport())==null) { return null; }
                    long size=Blobdemo.Schema.CatalogCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Blobdemo.Schema.CatalogCook((IntPtr)region,result,big?Blobdemo.TableByteOrder.Big:Blobdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Blobdemo.CatalogBuilder())
                    {
                        if(!Blobdemo.Schema.CatalogLoadBuilder(builder,bytes,new Blobdemo.TableReport())) { throw new InvalidOperationException("builder load: Blobdemo.Catalog"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Blobdemo.TableByteOrder.Big:Blobdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Blobdemo.Catalog"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Blobdemo.TableByteOrder.Big:Blobdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Blobdemo.Catalog"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("graphdemo","Album");
            c.NativeCook=(bytes,big)=>
            {
                long need=Graphdemo.Schema.AlbumLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Graphdemo.Schema.AlbumLoad((IntPtr)region,need,bytes,new Graphdemo.TableReport())==null) { return null; }
                    long size=Graphdemo.Schema.AlbumCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Graphdemo.Schema.AlbumCook((IntPtr)region,result,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Graphdemo.AlbumBuilder())
                    {
                        if(!Graphdemo.Schema.AlbumLoadBuilder(builder,bytes,new Graphdemo.TableReport())) { throw new InvalidOperationException("builder load: Graphdemo.Album"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Graphdemo.Album"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Graphdemo.Album"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("graphdemo","Depot");
            c.NativeCook=(bytes,big)=>
            {
                long need=Graphdemo.Schema.DepotLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Graphdemo.Schema.DepotLoad((IntPtr)region,need,bytes,new Graphdemo.TableReport())==null) { return null; }
                    long size=Graphdemo.Schema.DepotCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Graphdemo.Schema.DepotCook((IntPtr)region,result,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Graphdemo.DepotBuilder())
                    {
                        if(!Graphdemo.Schema.DepotLoadBuilder(builder,bytes,new Graphdemo.TableReport())) { throw new InvalidOperationException("builder load: Graphdemo.Depot"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Graphdemo.Depot"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Graphdemo.Depot"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("graphdemo","Scene");
            c.NativeCook=(bytes,big)=>
            {
                long need=Graphdemo.Schema.SceneLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Graphdemo.Schema.SceneLoad((IntPtr)region,need,bytes,new Graphdemo.TableReport())==null) { return null; }
                    long size=Graphdemo.Schema.SceneCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Graphdemo.Schema.SceneCook((IntPtr)region,result,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Graphdemo.SceneBuilder())
                    {
                        if(!Graphdemo.Schema.SceneLoadBuilder(builder,bytes,new Graphdemo.TableReport())) { throw new InvalidOperationException("builder load: Graphdemo.Scene"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Graphdemo.Scene"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Graphdemo.TableByteOrder.Big:Graphdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Graphdemo.Scene"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Album");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.AlbumLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.AlbumLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.AlbumCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.AlbumCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.AlbumBuilder())
                    {
                        if(!Listdemo.Schema.AlbumLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Album"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Album"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Album"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Army");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.ArmyLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.ArmyLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.ArmyCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.ArmyCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.ArmyBuilder())
                    {
                        if(!Listdemo.Schema.ArmyLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Army"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Army"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Army"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Mixed");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.MixedLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.MixedLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.MixedCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.MixedCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.MixedBuilder())
                    {
                        if(!Listdemo.Schema.MixedLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Mixed"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Mixed"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Mixed"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Save");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.SaveLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.SaveLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.SaveCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.SaveCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.SaveBuilder())
                    {
                        if(!Listdemo.Schema.SaveLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Save"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Save"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Save"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("listdemo","Sheet");
            c.NativeCook=(bytes,big)=>
            {
                long need=Listdemo.Schema.SheetLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Listdemo.Schema.SheetLoad((IntPtr)region,need,bytes,new Listdemo.TableReport())==null) { return null; }
                    long size=Listdemo.Schema.SheetCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Listdemo.Schema.SheetCook((IntPtr)region,result,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Listdemo.SheetBuilder())
                    {
                        if(!Listdemo.Schema.SheetLoadBuilder(builder,bytes,new Listdemo.TableReport())) { throw new InvalidOperationException("builder load: Listdemo.Sheet"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Listdemo.Sheet"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Listdemo.TableByteOrder.Big:Listdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Listdemo.Sheet"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Cells");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.CellsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.CellsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.CellsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.CellsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.CellsBuilder())
                    {
                        if(!Mapdemo.Schema.CellsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Cells"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Cells"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Cells"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Chunks");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.ChunksLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.ChunksLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.ChunksCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.ChunksCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.ChunksBuilder())
                    {
                        if(!Mapdemo.Schema.ChunksLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Chunks"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Chunks"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Chunks"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Crews");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.CrewsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.CrewsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.CrewsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.CrewsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.CrewsBuilder())
                    {
                        if(!Mapdemo.Schema.CrewsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Crews"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Crews"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Crews"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Depth");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.DepthLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.DepthLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.DepthCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.DepthCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.DepthBuilder())
                    {
                        if(!Mapdemo.Schema.DepthLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Depth"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Depth"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Depth"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Docs");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.DocsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.DocsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.DocsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.DocsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.DocsBuilder())
                    {
                        if(!Mapdemo.Schema.DocsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Docs"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Docs"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Docs"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","EdgeRow");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.EdgeRowLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.EdgeRowLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.EdgeRowCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.EdgeRowCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.EdgeRowBuilder())
                    {
                        if(!Mapdemo.Schema.EdgeRowLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.EdgeRow"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.EdgeRow"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.EdgeRow"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Fleet");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.FleetLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.FleetCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.FleetCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.FleetBuilder())
                    {
                        if(!Mapdemo.Schema.FleetLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Fleet"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Fleet"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Fleet"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Pairs");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.PairsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.PairsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.PairsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.PairsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.PairsBuilder())
                    {
                        if(!Mapdemo.Schema.PairsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Pairs"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Pairs"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Pairs"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Row");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.RowLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.RowLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.RowCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.RowCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.RowBuilder())
                    {
                        if(!Mapdemo.Schema.RowLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Row"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Row"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Row"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Runs");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.RunsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.RunsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.RunsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.RunsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.RunsBuilder())
                    {
                        if(!Mapdemo.Schema.RunsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Runs"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Runs"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Runs"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Slots");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.SlotsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.SlotsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.SlotsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.SlotsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.SlotsBuilder())
                    {
                        if(!Mapdemo.Schema.SlotsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Slots"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Slots"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Slots"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Spans");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.SpansLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.SpansLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.SpansCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.SpansCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.SpansBuilder())
                    {
                        if(!Mapdemo.Schema.SpansLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Spans"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Spans"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Spans"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Text");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.TextLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.TextLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.TextCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.TextCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.TextBuilder())
                    {
                        if(!Mapdemo.Schema.TextLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Text"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Text"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Text"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","Trails");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.TrailsLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.TrailsLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.TrailsCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.TrailsCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.TrailsBuilder())
                    {
                        if(!Mapdemo.Schema.TrailsLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.Trails"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.Trails"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.Trails"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("mapdemo","WideRow");
            c.NativeCook=(bytes,big)=>
            {
                long need=Mapdemo.Schema.WideRowLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Mapdemo.Schema.WideRowLoad((IntPtr)region,need,bytes,new Mapdemo.TableReport())==null) { return null; }
                    long size=Mapdemo.Schema.WideRowCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Mapdemo.Schema.WideRowCook((IntPtr)region,result,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Mapdemo.WideRowBuilder())
                    {
                        if(!Mapdemo.Schema.WideRowLoadBuilder(builder,bytes,new Mapdemo.TableReport())) { throw new InvalidOperationException("builder load: Mapdemo.WideRow"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Mapdemo.WideRow"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Mapdemo.TableByteOrder.Big:Mapdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Mapdemo.WideRow"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("streamdemo","Feed");
            c.NativeCook=(bytes,big)=>
            {
                long need=Streamdemo.Schema.FeedLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Streamdemo.Schema.FeedLoad((IntPtr)region,need,bytes,new Streamdemo.TableReport())==null) { return null; }
                    long size=Streamdemo.Schema.FeedCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Streamdemo.Schema.FeedCook((IntPtr)region,result,big?Streamdemo.TableByteOrder.Big:Streamdemo.TableByteOrder.Little)) { return null; }
                    using(var builder=new Streamdemo.FeedBuilder())
                    {
                        if(!Streamdemo.Schema.FeedLoadBuilder(builder,bytes,new Streamdemo.TableReport())) { throw new InvalidOperationException("builder load: Streamdemo.Feed"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Streamdemo.TableByteOrder.Big:Streamdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Streamdemo.Feed"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Streamdemo.TableByteOrder.Big:Streamdemo.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Streamdemo.Feed"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("tblg1","Guarded");
            c.NativeCook=(bytes,big)=>
            {
                long need=Tblg1.Schema.GuardedLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Tblg1.Schema.GuardedLoad((IntPtr)region,need,bytes,new Tblg1.TableReport())==null) { return null; }
                    long size=Tblg1.Schema.GuardedCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Tblg1.Schema.GuardedCook((IntPtr)region,result,big?Tblg1.TableByteOrder.Big:Tblg1.TableByteOrder.Little)) { return null; }
                    using(var builder=new Tblg1.GuardedBuilder())
                    {
                        if(!Tblg1.Schema.GuardedLoadBuilder(builder,bytes,new Tblg1.TableReport())) { throw new InvalidOperationException("builder load: Tblg1.Guarded"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Tblg1.TableByteOrder.Big:Tblg1.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Tblg1.Guarded"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Tblg1.TableByteOrder.Big:Tblg1.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Tblg1.Guarded"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("tblp2","Chain");
            c.NativeCook=(bytes,big)=>
            {
                long need=Tblp2.Schema.ChainLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Tblp2.Schema.ChainLoad((IntPtr)region,need,bytes,new Tblp2.TableReport())==null) { return null; }
                    long size=Tblp2.Schema.ChainCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Tblp2.Schema.ChainCook((IntPtr)region,result,big?Tblp2.TableByteOrder.Big:Tblp2.TableByteOrder.Little)) { return null; }
                    using(var builder=new Tblp2.ChainBuilder())
                    {
                        if(!Tblp2.Schema.ChainLoadBuilder(builder,bytes,new Tblp2.TableReport())) { throw new InvalidOperationException("builder load: Tblp2.Chain"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Tblp2.TableByteOrder.Big:Tblp2.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Tblp2.Chain"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Tblp2.TableByteOrder.Big:Tblp2.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Tblp2.Chain"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("tblw1","Fleet");
            c.NativeCook=(bytes,big)=>
            {
                long need=Tblw1.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Tblw1.Schema.FleetLoad((IntPtr)region,need,bytes,new Tblw1.TableReport())==null) { return null; }
                    long size=Tblw1.Schema.FleetCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Tblw1.Schema.FleetCook((IntPtr)region,result,big?Tblw1.TableByteOrder.Big:Tblw1.TableByteOrder.Little)) { return null; }
                    using(var builder=new Tblw1.FleetBuilder())
                    {
                        if(!Tblw1.Schema.FleetLoadBuilder(builder,bytes,new Tblw1.TableReport())) { throw new InvalidOperationException("builder load: Tblw1.Fleet"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Tblw1.TableByteOrder.Big:Tblw1.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Tblw1.Fleet"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Tblw1.TableByteOrder.Big:Tblw1.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Tblw1.Fleet"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
        {
            Codec c=Find("tblw2","Fleet");
            c.NativeCook=(bytes,big)=>
            {
                long need=Tblw2.Schema.FleetLoadMeasure(bytes);
                if(need<0) { return null; }
                byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
                try
                {
                    if(Tblw2.Schema.FleetLoad((IntPtr)region,need,bytes,new Tblw2.TableReport())==null) { return null; }
                    long size=Tblw2.Schema.FleetCookMeasure((IntPtr)region);
                    if(size<0) { return null; }
                    byte[] result=new byte[checked((int)size)];
                    if(!Tblw2.Schema.FleetCook((IntPtr)region,result,big?Tblw2.TableByteOrder.Big:Tblw2.TableByteOrder.Little)) { return null; }
                    using(var builder=new Tblw2.FleetBuilder())
                    {
                        if(!Tblw2.Schema.FleetLoadBuilder(builder,bytes,new Tblw2.TableReport())) { throw new InvalidOperationException("builder load: Tblw2.Fleet"); }
                        byte[] built=new byte[builder.CookMeasure()];
                        if(!builder.Cook(built,big?Tblw2.TableByteOrder.Big:Tblw2.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("mutable builder cook: Tblw2.Fleet"); }
                        if(!builder.Lock() || !builder.Cook(built,big?Tblw2.TableByteOrder.Big:Tblw2.TableByteOrder.Little) || !built.AsSpan().SequenceEqual(result)) { throw new InvalidOperationException("locked builder cook: Tblw2.Fleet"); }
                    }
                    return result;
                }
                finally { NativeMemory.AlignedFree(region); }
            };
        }
    }
}
