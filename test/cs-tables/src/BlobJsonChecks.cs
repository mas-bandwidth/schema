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
        bool CountersZero(Blobdemo.TableReport r) =>
            r.Unknown==0 && r.KindMismatch==0 && r.Widened==0 && r.Clamped==0 &&
            r.Duplicate==0 && r.BlobShared==0 && r.Retained==0 && r.RetainLost==0;
        var value=new Blobdemo.Catalog(); var report=new Blobdemo.TableReport();
        Check(Blobdemo.Schema.CatalogFromJson(value,Encoding.UTF8.GetBytes("{\"name\":\"before\",\"note\":\"before\",\"thumb\":\"AQ==\"}"),report),"blob UTF-8 refusal starts from populated state");
        byte[] text={ (byte)'{',(byte)'"',(byte)'n',(byte)'o',(byte)'t',(byte)'e',(byte)'"',(byte)':',(byte)'"',0xff,0xfe,(byte)'"',(byte)'}' };
        bool ok=Blobdemo.Schema.CatalogFromJson(value,text,report);
        Check(!ok && report.Malformed && !report.Refused && report.Reason=="string_not_utf8: Catalog.note" && report.NonFiniteFloat==null && CountersZero(report),"blob JSON refuses invalid raw UTF-8 without replacement or counter events");
        Check(value.NameLength==0 && value.Head==null && value.Thumb==null && value.Note==null && value.Alias==null,"blob UTF-8 refusal restores declared defaults");
        // §16.3 refuses malformed raw UTF-8. Valid literal and escaped U+FFFD
        // still exercise sizing of two scalars that occupy six stored bytes.
        foreach(string json in new[] { "{\"note\":\"\ufffd\ufffd\"}", "{\"note\":\"\\uFFFD\\uFFFD\"}" })
        {
            report=new Blobdemo.TableReport();
            ok=Blobdemo.Schema.CatalogFromJson(value,Encoding.UTF8.GetBytes(json),report);
            Check(ok && !report.Malformed && !report.Refused && report.Reason==null && report.NonFiniteFloat==null && CountersZero(report),"valid replacement characters parse with a clean report");
            Check(value.Note!=null && value.Note.Data.AsSpan().SequenceEqual(Encoding.UTF8.GetBytes("\ufffd\ufffd")),"blob string sizing includes six replacement-character UTF-8 bytes");
        }
    }
}
