using System;
using System.Text;

static partial class Program
{
    static long BlobJsonAllocation(byte[] json,int count)
    {
        var value=new Blobdemo.Catalog(); var report=new Blobdemo.TableReport();
        Blobdemo.Schema.CatalogFromJson(value,json,report);
        report=new Blobdemo.TableReport();
        long before=GC.GetAllocatedBytesForCurrentThread();
        bool ok=Blobdemo.Schema.CatalogFromJson(value,json,report);
        long bytes=GC.GetAllocatedBytesForCurrentThread()-before;
        Check(ok && !report.Damaged && !report.Refused,"large blob JSON parses with a clean report");
        bool thumb=value.Thumb!=null && value.Thumb.Data.Length==4*count;
        for(int i=0;thumb && i<value.Thumb.Data.Length;i++) { thumb=value.Thumb.Data[i]==(byte)(i&3); }
        Check(value.Note!=null && Encoding.UTF8.GetString(value.Note.Data)==Repeat("hello world",count) && thumb,"large blob JSON decodes its values");
        return bytes;
    }
    static string Repeat(string text,int count) { var b=new StringBuilder(); for(int i=0;i<count;i++) { b.Append(text); } return b.ToString(); }
    static void TestBlobJsonAllocation()
    {
        // A repeated JSON key is damage under strict-only (docs/SPEC-TABLES.md §16),
        // so the scaling is measured on one note and one thumb whose bodies grow.
        byte[] Document(int count)
        {
            byte[] raw=new byte[4*count];
            for(int i=0;i<raw.Length;i++) { raw[i]=(byte)(i&3); }
            return Encoding.UTF8.GetBytes("{\"note\":\""+Repeat("hello world",count)+"\",\"thumb\":\""+Convert.ToBase64String(raw)+"\"}");
        }
        long small=BlobJsonAllocation(Document(500),500),large=BlobJsonAllocation(Document(1000),1000);
        Check(large<small*3,"doubling blob JSON does not quadruple scratch allocation");
        {
            var repeated=new Blobdemo.Catalog(); var repeatedReport=new Blobdemo.TableReport();
            Check(!Blobdemo.Schema.CatalogFromJson(repeated,Encoding.UTF8.GetBytes("{\"note\":\"a\",\"note\":\"b\"}"),repeatedReport) && (repeatedReport.Damaged || repeatedReport.Refused) && repeated.Note==null,
                "a repeated blob JSON key refuses the whole load");
        }
        bool CountersZero(Blobdemo.TableReport r) =>
            r.Unknown==0 && !r.Refused && r.Widened==0 && r.Retained==0 && r.RetainLost==0;
        var value=new Blobdemo.Catalog(); var report=new Blobdemo.TableReport();
        Check(Blobdemo.Schema.CatalogFromJson(value,Encoding.UTF8.GetBytes("{\"name\":\"before\",\"note\":\"before\",\"thumb\":\"AQ==\"}"),report),"blob UTF-8 refusal starts from populated state");
        byte[] text={ (byte)'{',(byte)'"',(byte)'n',(byte)'o',(byte)'t',(byte)'e',(byte)'"',(byte)':',(byte)'"',0xff,0xfe,(byte)'"',(byte)'}' };
        bool ok=Blobdemo.Schema.CatalogFromJson(value,text,report);
        Check(!ok && report.Damaged && !report.Refused && report.Reason=="string_not_utf8: Catalog.note" && report.NonFiniteFloat==null && CountersZero(report),"blob JSON refuses invalid raw UTF-8 without replacement or counter events");
        Check(value.NameLength==0 && value.Head==null && value.Thumb==null && value.Note==null && value.Alias==null,"blob UTF-8 refusal restores declared defaults");
        // §16.3 refuses malformed raw UTF-8. Valid literal and escaped U+FFFD
        // still exercise sizing of two scalars that occupy six stored bytes.
        foreach(string json in new[] { "{\"note\":\"\ufffd\ufffd\"}", "{\"note\":\"\\uFFFD\\uFFFD\"}" })
        {
            report=new Blobdemo.TableReport();
            ok=Blobdemo.Schema.CatalogFromJson(value,Encoding.UTF8.GetBytes(json),report);
            Check(ok && !report.Damaged && !report.Refused && report.Reason==null && report.NonFiniteFloat==null && CountersZero(report),"valid replacement characters parse with a clean report");
            Check(value.Note!=null && value.Note.Data.AsSpan().SequenceEqual(Encoding.UTF8.GetBytes("\ufffd\ufffd")),"blob string sizing includes six replacement-character UTF-8 bytes");
        }
    }
}
