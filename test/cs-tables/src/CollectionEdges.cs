using System;
using System.Runtime.InteropServices;
using A=Cscollections1;
using B=Cscollections2;

static partial class Program
{
    static unsafe void TestCollectionEdges()
    {
        byte[] wrongArm=ReadGolden("map_depth");
        long armExtent=Mapdemo.Schema.DepthLoadMeasure(wrongArm);
        int armAt=wrongArm.AsSpan().IndexOf(new byte[]{9,15,10,13,21});
        Check(armAt>=0,"map depth fixture carries the named table arm");
        if(armAt>=0)
        {
            wrongArm[armAt+3]=0;
            Check(Mapdemo.Schema.DepthLoadMeasure(wrongArm)==armExtent,"named arm framing reserves its extent even when decoding rejects the value kind");
        }
        var source=new A.Row { Values=new byte[20],ValuesCount=20,After=91 };
        Array.Fill(source.Values,(byte)0);
        byte[] file=new byte[A.Schema.RowMeasure(source)]; A.Schema.RowSave(source,file);
        var target=new B.Row(); var report=new B.TableReport();
        Check(B.Schema.RowLoad(target,file,report) && report.Widened==1 && !report.Malformed && target.ValuesCount==20 && target.After==91,"scalar list widens once and reads the following field");
        long need=B.Schema.RowLoadMeasure(file);
        Check(need>=B.Schema.RowTableType().StorageSize+20*8,"widening measure reserves destination-width elements");
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
        try
        {
            new Span<byte>(region+need,64).Fill(0xa5); report=new B.TableReport();
            var root=B.Schema.RowLoad((IntPtr)region,need,file,report);
            Check(root!=null && root->Values.Count==20 && root->After==91 && report.Widened==1 && !report.Malformed,"native widened list lands in its exact measured extent");
            var values=B.TableArena.At<ulong>(ref root->Values.Reference);
            for(int i=0;i<20;i++) { Check(values[i]==0,"widened native element has destination width"); }
            for(int i=0;i<64;i++) { Check(region[need+i]==0xa5,"widened list preserves region guard"); }
        }
        finally { NativeMemory.AlignedFree(region); }
        using(var builder=new B.RowBuilder())
        {
            report=new B.TableReport(); Check(builder.Load(file,report) && report.Widened==1 && builder.Lock(),"widened list builder loads and locks");
            Check(builder.AsConst()->Values.Count==20,"widened builder preserves count");
        }

        // The twenty elements carry zero bits, so their count exceeds the
        // remaining message bits. A width floor of one wrongly refuses it.
        A.Row[] batch={source}; byte[] message=new byte[A.Schema.RowMeasureMessages(batch)]; A.Schema.RowSaveMessages(batch,message);
        var vocabulary=new B.TableVocabulary(); report=new B.TableReport();
        B.Schema.AnnounceRead(vocabulary,A.Schema.Announce(),report);
        B.Row[] output={new B.Row()}; B.Schema.RowLoadMessages(output,message,vocabulary,report,out int count);
        Check(count==1 && !report.Malformed && !report.Refused && report.Widened==1 && output[0].ValuesCount==20 && output[0].After==91,"zero-bit widened list decodes the full message count");
        need=B.Schema.RowLoadMeasure(vocabulary,message,out _,out _);
        Check(need>=B.Schema.RowTableType().StorageSize+20*8,"zero-bit message measure reserves native elements");
        region=(byte*)NativeMemory.AlignedAlloc((nuint)(need+64),64);
        try
        {
            new Span<byte>(region+need,64).Fill(0xa5); report=new B.TableReport(); IntPtr[] roots=new IntPtr[1];
            B.Schema.RowLoadMessages((IntPtr)region,need,roots,message,vocabulary,report,out count);
            Check(count==1 && !report.Malformed && report.Widened==1 && ((B.RowRow*)roots[0])->Values.Count==20,"native zero-bit list message uses the measured extent");
            for(int i=0;i<64;i++) { Check(region[need+i]==0xa5,"zero-bit list preserves region guard"); }
        }
        finally { NativeMemory.AlignedFree(region); }
        using(var builder=new B.WideRootBuilder())
        {
            var root=builder.GetRoot(); var worker=builder.CreateWorker();
            var values=worker.List<UInt128>(ref root->Values,B.Schema.WideRootTableType().Fields[1]);
            UInt128* wide=values.Add(); *wide=((UInt128)1<<100)+19;
            var node=builder.Alloc<B.WideNodeRow>(); node->Value=*wide;
            var nodes=worker.List<long>(ref root->Nodes,B.Schema.WideRootTableType().Fields[0]);
            long* link=nodes.Add(); B.TableArena.SetReference(ref *link,node);
            Check(((ulong)wide&15)==0 && ((ulong)node&15)==0 && builder.Lock(),"wide list elements and pointer nodes are 16-aligned");
            var packed=builder.AsConst(); wide=B.TableArena.At<UInt128>(ref packed->Values.Reference);
            Check(((ulong)wide&15)==0 && *wide==((UInt128)1<<100)+19,"wide alignment and value survive packing");
        }
    }
}
