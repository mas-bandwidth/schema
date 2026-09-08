using System;
using System.Text;

static partial class Program
{
    static long BlobJsonAllocation(byte[] json)
    {
        var value=new Blobdemo.Catalog(); var report=new Blobdemo.TableReport();
        Blobdemo.Schema.CatalogFromJson(value,json,report);
        report=new Blobdemo.TableReport();
        long before=GC.GetAllocatedBytesForCurrentThread();
        bool ok=Blobdemo.Schema.CatalogFromJson(value,json,report);
        long bytes=GC.GetAllocatedBytesForCurrentThread()-before;
        Check(ok && !report.Malformed && report.KindMismatch==0 && report.Clamped==0,"repeated blob JSON preserves parse and report");
        Check(Encoding.UTF8.GetString(value.Note.Data)=="hello world" && value.Thumb.Data.AsSpan().SequenceEqual(new byte[] {0,1,2,3}),"repeated blob JSON preserves decoded values");
        return bytes;
    }
    static void TestBlobJsonAllocation()
    {
        byte[] Document(int count)
        {
            var text=new StringBuilder("{");
            for(int i=0;i<count;i++) { if(i!=0) { text.Append(','); } text.Append("\"note\":\"hello world\",\"thumb\":\"AAECAw==\""); }
            return Encoding.UTF8.GetBytes(text.Append('}').ToString());
        }
        long small=BlobJsonAllocation(Document(500)),large=BlobJsonAllocation(Document(1000));
        Check(large<small*3,"doubling blob JSON does not quadruple scratch allocation");
        var value=new Blobdemo.Catalog(); var report=new Blobdemo.TableReport();
        byte[] text={ (byte)'{',(byte)'"',(byte)'n',(byte)'o',(byte)'t',(byte)'e',(byte)'"',(byte)':',(byte)'"',0xff,0xfe,(byte)'"',(byte)'}' };
        Check(Blobdemo.Schema.CatalogFromJson(value,text,report) && value.Note.Data.AsSpan().SequenceEqual(Encoding.UTF8.GetBytes("\ufffd\ufffd")) && report.Clamped==0,"blob string sizing includes replacement UTF-8 expansion");
    }
}
