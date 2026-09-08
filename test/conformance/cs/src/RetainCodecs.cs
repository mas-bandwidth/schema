using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using RT=Tblrt1;

static partial class Program
{
    sealed class RetainAnswer
    {
        public Report Report;
        public long Need,Written=-1;
        public bool Loaded;
        public int Kept,Lost;
        public byte[] Saved=Array.Empty<byte>();
    }
    sealed unsafe class RetainScratch : IDisposable
    {
        public byte* Bytes=(byte*)NativeMemory.Alloc(1<<20);
        public void* Ids=NativeMemory.Alloc(1<<21);
        public void Dispose() { NativeMemory.Free(Bytes); NativeMemory.Free(Ids); }
    }
    static unsafe void RegisterRetains(RetainScratch scratch)
    {
        Find("blobdemo","Catalog").Retain=wire=>
        {
            long need=Blobdemo.Schema.CatalogLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Blobdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Blobdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Blobdemo.TableReport();
                var root=Blobdemo.Schema.CatalogLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Blobdemo.Schema.CatalogMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Blobdemo.Schema.CatalogSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("graphdemo","Album").Retain=wire=>
        {
            long need=Graphdemo.Schema.AlbumLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Graphdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Graphdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Graphdemo.TableReport();
                var root=Graphdemo.Schema.AlbumLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Graphdemo.Schema.AlbumMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Graphdemo.Schema.AlbumSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("graphdemo","Depot").Retain=wire=>
        {
            long need=Graphdemo.Schema.DepotLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Graphdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Graphdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Graphdemo.TableReport();
                var root=Graphdemo.Schema.DepotLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Graphdemo.Schema.DepotMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Graphdemo.Schema.DepotSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("graphdemo","Scene").Retain=wire=>
        {
            long need=Graphdemo.Schema.SceneLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Graphdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Graphdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Graphdemo.TableReport();
                var root=Graphdemo.Schema.SceneLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Graphdemo.Schema.SceneMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Graphdemo.Schema.SceneSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("listdemo","Album").Retain=wire=>
        {
            long need=Listdemo.Schema.AlbumLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Listdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Listdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Listdemo.TableReport();
                var root=Listdemo.Schema.AlbumLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Listdemo.Schema.AlbumMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Listdemo.Schema.AlbumSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("listdemo","Army").Retain=wire=>
        {
            long need=Listdemo.Schema.ArmyLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Listdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Listdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Listdemo.TableReport();
                var root=Listdemo.Schema.ArmyLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Listdemo.Schema.ArmyMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Listdemo.Schema.ArmySaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("listdemo","Mixed").Retain=wire=>
        {
            long need=Listdemo.Schema.MixedLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Listdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Listdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Listdemo.TableReport();
                var root=Listdemo.Schema.MixedLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Listdemo.Schema.MixedMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Listdemo.Schema.MixedSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("listdemo","Save").Retain=wire=>
        {
            long need=Listdemo.Schema.SaveLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Listdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Listdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Listdemo.TableReport();
                var root=Listdemo.Schema.SaveLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Listdemo.Schema.SaveMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Listdemo.Schema.SaveSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("listdemo","Sheet").Retain=wire=>
        {
            long need=Listdemo.Schema.SheetLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Listdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Listdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Listdemo.TableReport();
                var root=Listdemo.Schema.SheetLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Listdemo.Schema.SheetMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Listdemo.Schema.SheetSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Cells").Retain=wire=>
        {
            long need=Mapdemo.Schema.CellsLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.CellsLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.CellsMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.CellsSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Chunks").Retain=wire=>
        {
            long need=Mapdemo.Schema.ChunksLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.ChunksLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.ChunksMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.ChunksSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Crews").Retain=wire=>
        {
            long need=Mapdemo.Schema.CrewsLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.CrewsLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.CrewsMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.CrewsSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Depth").Retain=wire=>
        {
            long need=Mapdemo.Schema.DepthLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.DepthLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.DepthMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.DepthSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Docs").Retain=wire=>
        {
            long need=Mapdemo.Schema.DocsLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.DocsLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.DocsMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.DocsSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","EdgeRow").Retain=wire=>
        {
            long need=Mapdemo.Schema.EdgeRowLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.EdgeRowLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.EdgeRowMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.EdgeRowSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Fleet").Retain=wire=>
        {
            long need=Mapdemo.Schema.FleetLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.FleetLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.FleetMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.FleetSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Pairs").Retain=wire=>
        {
            long need=Mapdemo.Schema.PairsLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.PairsLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.PairsMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.PairsSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Row").Retain=wire=>
        {
            long need=Mapdemo.Schema.RowLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.RowLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.RowMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.RowSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Runs").Retain=wire=>
        {
            long need=Mapdemo.Schema.RunsLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.RunsLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.RunsMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.RunsSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Slots").Retain=wire=>
        {
            long need=Mapdemo.Schema.SlotsLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.SlotsLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.SlotsMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.SlotsSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Spans").Retain=wire=>
        {
            long need=Mapdemo.Schema.SpansLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.SpansLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.SpansMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.SpansSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Text").Retain=wire=>
        {
            long need=Mapdemo.Schema.TextLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.TextLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.TextMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.TextSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","Trails").Retain=wire=>
        {
            long need=Mapdemo.Schema.TrailsLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.TrailsLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.TrailsMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.TrailsSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("mapdemo","WideRow").Retain=wire=>
        {
            long need=Mapdemo.Schema.WideRowLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Mapdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Mapdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Mapdemo.TableReport();
                var root=Mapdemo.Schema.WideRowLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Mapdemo.Schema.WideRowMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Mapdemo.Schema.WideRowSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("streamdemo","Feed").Retain=wire=>
        {
            long need=Streamdemo.Schema.FeedLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Streamdemo.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Streamdemo.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Streamdemo.TableReport();
                var root=Streamdemo.Schema.FeedLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Streamdemo.Schema.FeedMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Streamdemo.Schema.FeedSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("tblg1","Guarded").Retain=wire=>
        {
            long need=Tblg1.Schema.GuardedLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Tblg1.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Tblg1.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Tblg1.TableReport();
                var root=Tblg1.Schema.GuardedLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Tblg1.Schema.GuardedMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Tblg1.Schema.GuardedSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("tblp2","Chain").Retain=wire=>
        {
            long need=Tblp2.Schema.ChainLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Tblp2.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Tblp2.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Tblp2.TableReport();
                var root=Tblp2.Schema.ChainLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Tblp2.Schema.ChainMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Tblp2.Schema.ChainSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("tblw1","Fleet").Retain=wire=>
        {
            long need=Tblw1.Schema.FleetLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Tblw1.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Tblw1.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Tblw1.TableReport();
                var root=Tblw1.Schema.FleetLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Tblw1.Schema.FleetMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Tblw1.Schema.FleetSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
        Find("tblw2","Fleet").Retain=wire=>
        {
            long need=Tblw2.Schema.FleetLoadMeasure(wire);
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(Math.Max(0,need)+64),64);
            try
            {
                new Span<byte>(region+Math.Max(0,need),64).Fill(0xa5);
                var retain=new Tblw2.TableRetain { Bytes=scratch.Bytes,Capacity=1<<20,Ids=(Tblw2.TableRetain.Id*)scratch.Ids,IdCapacity=1<<17 };
                var report=new Tblw2.TableReport();
                var root=Tblw2.Schema.FleetLoadRetain((IntPtr)region,need,wire,ref retain,report);
                var answer=new RetainAnswer { Need=need,Loaded=root!=null };
                if(root!=null)
                {
                    long size=Tblw2.Schema.FleetMeasureRetain((IntPtr)root,ref retain);
                    if(size>=0)
                    {
                        answer.Saved=new byte[checked((int)size)];
                        answer.Written=Tblw2.Schema.FleetSaveRetain((IntPtr)root,ref retain,answer.Saved,report);
                    }
                }
                for(int i=0;i<64;i++) { if(region[Math.Max(0,need)+i]!=0xa5) { throw new InvalidOperationException("Retaining load overran its measured region"); } }
                answer.Report=Copy(report); answer.Kept=report.Retained; answer.Lost=report.RetainLost; return answer;
            }
            finally { NativeMemory.AlignedFree(region); }
        };
    }
    static unsafe int SurfaceRetain(string outDir,bool saved)
    {
        foreach(string[] f in lines)
        {
            if(f[0]!="retain" && f[0]!="retain-message") { continue; }
            bool message=f[0]=="retain-message"; int offset=message?1:0;
            if(f[2+offset]!="tblrt1" || f[3+offset]!="Node") { SpillAbsent(outDir,f[1]); continue; }
            byte[] wire=File.ReadAllBytes(f[4+offset]);
            var report=new RT.TableReport(); var vocabulary=new RT.TableVocabulary();
            if(message)
            {
                string[] connection=lines.Find(row=>row[0]=="connection" && row[1]==f[2]);
                RT.Schema.AnnounceRead(vocabulary,File.ReadAllBytes(connection[4]),report);
            }
            long need=message?RT.Schema.NodeLoadMeasure(vocabulary,wire):RT.Schema.NodeLoadMeasure(wire);
            if(need<0 || report.Malformed || report.Refused) { return 1; }
            int bodies=message?wire[1]+1:1;
            byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)need,64);
            RT.TableRetain[] stores=new RT.TableRetain[bodies]; IntPtr[] roots=new IntPtr[bodies];
            for(int i=0;i<bodies;i++)
            { stores[i]=new RT.TableRetain { Bytes=(byte*)NativeMemory.Alloc(1<<20),Capacity=1<<20,Ids=(RT.TableRetain.Id*)NativeMemory.Alloc(1<<21),IdCapacity=f[6+offset]=="full"?1<<17:int.Parse(f[6+offset]) }; }
            try
            {
                if(message)
                { RT.Schema.NodeLoadRetainMessages((IntPtr)region,need,roots,wire,vocabulary,stores,report,out _); }
                else
                {
                    if(f[5]=="short")
                    { RT.Schema.NodeLoadRetain((IntPtr)region,need,wire,ref stores[0],new RT.TableReport()); stores[0].Capacity=stores[0].Used-1; }
                    roots[0]=(IntPtr)RT.Schema.NodeLoadRetain((IntPtr)region,need,wire,ref stores[0],report);
                }
                if(report.Malformed || report.Refused) { return 1; }
                using var bytes=new MemoryStream(); var save=new RT.TableReport();
                for(int i=0;i<bodies;i++)
                {
                    long size=RT.Schema.NodeMeasureRetain(roots[i],ref stores[i]);
                    if(size<0) { return 1; }
                    byte[] output=new byte[size];
                    if(RT.Schema.NodeSaveRetain(roots[i],ref stores[i],output,save)!=size) { return 1; }
                    bytes.Write(output);
                }
                if(saved) { File.WriteAllBytes(Path.Combine(outDir,f[1]),bytes.ToArray()); }
                else { File.WriteAllText(Path.Combine(outDir,f[1]),report.Retained+","+report.RetainLost+","+report.Unknown+" "+save.RetainLost+"\n"); }
            }
            finally
            {
                NativeMemory.AlignedFree(region);
                foreach(var store in stores) { NativeMemory.Free(store.Bytes); NativeMemory.Free(store.Ids); }
            }
        }
        return 0;
    }
}
