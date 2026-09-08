using System;
using System.Buffers.Binary;
using System.Runtime.InteropServices;
using System.Text;
using Endianprobe;

unsafe class Program
{
    static int failures;
    static void Require(bool ok,string message)
    { if(!ok) { Console.WriteLine("FAILED BLOB HEADER: "+message); failures++; } }

    static int Main()
    {
        var source=new Root(); var report=new TableReport();
        Require(Schema.RootFromJson(source,Encoding.UTF8.GetBytes("{\"payload\":\"AQIDBA==\",\"note\":\"hello\"}"),report),"source JSON");
        byte[] expected=new byte[Schema.RootMeasure(source)]; Schema.RootSave(source,expected);
        Root[] batch={source}; byte[] message=new byte[Schema.RootMeasureMessages(batch)]; Schema.RootSaveMessages(batch,message);
        var vocabulary=new TableVocabulary(); Schema.AnnounceRead(vocabulary,Schema.Announce(),new TableReport());
        long need=Schema.RootLoadMeasure(vocabulary,message);
        byte* region=(byte*)NativeMemory.AlignedAlloc((nuint)need,64);
        byte* storage=stackalloc byte[1024]; TableRetain.Id* ids=stackalloc TableRetain.Id[16];
        Span<IntPtr> roots=stackalloc IntPtr[1]; Span<TableRetain> stores=stackalloc TableRetain[1];
        try
        {
            for(int retaining=0;retaining<2;retaining++)
            {
                string arm=retaining==0?"native":"retain";
                stores[0]=new TableRetain { Bytes=storage,Capacity=1024,Ids=ids,IdCapacity=16 };
                report=new TableReport(); int count;
                var verdict=retaining==0
                    ?Schema.RootLoadMessages((IntPtr)region,need,roots,message,vocabulary,report,out count)
                    :Schema.RootLoadRetainMessages((IntPtr)region,need,roots,message,vocabulary,stores,report,out count);
                Require(verdict==Schema.TableWire.Verdict.Ok && count==1 && !report.Malformed,arm+" load");
                foreach(var field in Schema.RootTableType().Fields)
                {
                    byte* slot=(byte*)roots[0]+field.NativeOffset;
                    long offset=BinaryPrimitives.ReadInt64BigEndian(new ReadOnlySpan<byte>(slot,8));
                    byte* blob=slot+offset;
                    int length=field.Name=="payload"?4:5;
                    Require(BinaryPrimitives.ReadUInt32BigEndian(new ReadOnlySpan<byte>(blob,4))==length,arm+" length "+field.Name);
                    Require(BinaryPrimitives.ReadUInt32BigEndian(new ReadOnlySpan<byte>(blob+4,4))==0,arm+" reserved word "+field.Name);
                    ReadOnlySpan<byte> content=field.Name=="payload"?new byte[]{1,2,3,4}:Encoding.UTF8.GetBytes("hello");
                    Require(new ReadOnlySpan<byte>(blob+8,length).SequenceEqual(content),arm+" payload "+field.Name);
                    if(field.Name=="note") { Require(blob[8+length]==0,arm+" terminator"); }
                }
                long size=retaining==0?Schema.RootMeasure(roots[0]):Schema.RootMeasureRetain(roots[0],ref stores[0]);
                byte[] saved=new byte[size];
                long written=retaining==0?Schema.RootSave(roots[0],saved):Schema.RootSaveRetain(roots[0],ref stores[0],saved,new TableReport());
                Require(written==expected.Length && saved.AsSpan().SequenceEqual(expected),arm+" canonical file round trip");
            }
        }
        finally { NativeMemory.AlignedFree(region); }
        return failures==0?0:1;
    }
}
