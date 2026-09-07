using System;
using System.Text;
using Serialize;
using Wide;
using P = Wideprobe.Schema;

static class Program
{
    static void Check(bool ok, string message) { if (!ok) throw new Exception(message); }
    static string Hex(ReadOnlySpan<byte> bytes) => Convert.ToHexString(bytes).ToLowerInvariant();
    static void Replay<T>(byte[] wire, T value, Func<ReadStream,T,bool> read, Func<WriteStream,T,bool> write, Func<T,char[]> text, Func<T,int> length)
    {
        Array.Fill(text(value), (char)0x7f7f);
        ReadStream r = new ReadStream(wire);
        if (!read(r, value)) { Console.WriteLine("REFUSE"); return; }
        Check(r.Ok, "accepted latched error");
        WriteStream w = new WriteStream(new byte[256]);
        Check(write(w, value), "write"); w.Flush(); Check(w.Ok, "flush");
        StringBuilder units = new StringBuilder();
        for (int i=0; i<length(value); i++) units.Append(((int)text(value)[i]).ToString("x4"));
        if (units.Length == 0) units.Append('-');
        Console.WriteLine($"OK {r.BitsProcessed} {units} {w.BitsProcessed} {Hex(w.Data)}");
    }
    static (byte[],int) Encode<T>(T v, Func<WriteStream,T,bool> write)
    {
        WriteStream w = new WriteStream(new byte[1024]);
        Check(write(w,v), "write contract"); w.Flush(); Check(w.Ok,"flush contract");
        return (w.Data.ToArray(), (int)w.BitsProcessed);
    }
    static void Contracts()
    {
        WideSeven v = new WideSeven();
        foreach(int n in new[]{-1,8,1}) { v.TextLength=n; Check(!Schema.WriteWideSeven(new WriteStream(new byte[256]),v),"writer bounds/null"); }
        v.Text[0]=(char)0xd800;
        var (wire,_) = Encode(v, Schema.WriteWideSeven);
        Check(!Schema.ReadWideSeven(new ReadStream(wire),v),"unpaired high");
        v.Text[0]=(char)0xffff;
        (wire,_) = Encode(v, Schema.WriteWideSeven);
        Array.Fill(v.Text,(char)0x7f7f);
        ReadStream r = new ReadStream(wire);
        Check(Schema.ReadWideSeven(r,v) && v.Text[0]==0xffff && v.Text[1]==0x7f7f && v.Text[6]==0x7f7f,"tail");
        for(int i=0;i<1000;i++) { r.Reset(wire); Check(Schema.ReadWideSeven(r,v),"warm"); }
        long before=GC.GetAllocatedBytesForCurrentThread();
        for(int i=0;i<10000;i++) { r.Reset(wire); Check(Schema.ReadWideSeven(r,v),"measured"); }
        Check(before==GC.GetAllocatedBytesForCurrentThread(),"read allocated");
        var c = new Wideprobe.Conditional { Enabled=true, TextLength=2 };
        c.Text[0]=(char)0xd800; c.Text[1]=(char)0xdc00;
        var (cw,bits)=Encode(c,P.WriteConditional);
        Check(bits==68 && (cw[0]&15)==5,"unaligned conditional");
        var co = new Wideprobe.Conditional();
        Check(P.ReadConditional(new ReadStream(cw),co) && co.TextLength==2 && co.Text[1]==0xdc00,"conditional");
        c.Enabled=false; (cw,_)=Encode(c,P.WriteConditional);
        Check(P.ReadConditional(new ReadStream(cw),co) && co.TextLength==0 && co.Text[0]==0 && co.Text[1]==0,"branch zero");
        var choice = new Wideprobe.Choice { Type=Wideprobe.ChoiceType.Text };
        choice.Text.ValueLength=1; choice.Text.Value[0]=(char)0xffff;
        (cw,_)=Encode(choice,P.WriteChoice);
        var outChoice = new Wideprobe.Choice();
        for(int i=0;i<2;i++) { outChoice.Text.Value[3]=(char)0x7f7f; Check(P.ReadChoice(new ReadStream(cw),outChoice) && outChoice.Text.ValueLength==1 && outChoice.Text.Value[0]==0xffff && outChoice.Text.Value[3]==0,"union reset"); }
        var box = new Wideprobe.Box(); Check(box.CountedCount==1,"born count");
        box.Items[0].ValueLength=1; box.Items[0].Value[0]=(char)0xffff;
        box.Counted[0].ValueLength=1; box.Counted[0].Value[0]='A';
        box.Choice.Type=Wideprobe.ChoiceType.Text;
        (cw,_)=Encode(box,P.WriteBox);
        var bo = new Wideprobe.Box(); bo.Counted[1].Value[0]=(char)0x7f7f;
        Check(P.ReadBox(new ReadStream(cw),bo) && bo.Items[0].Value[0]==0xffff && bo.CountedCount==1 && bo.Counted[0].Value[0]=='A' && bo.Counted[1].Value[0]==0x7f7f && bo.Choice.Type==Wideprobe.ChoiceType.Text,"composition");
    }
    static void Main(string[] args)
    {
        if(args.Length!=0) { Contracts(); return; }
        string line;
        while((line=Console.ReadLine())!=null)
        {
            string[] parts=line.Split(' ');
            byte[] wire=parts[1]=="-" ? Array.Empty<byte>() : Convert.FromHexString(parts[1]);
            if(parts[0]=="7") Replay(wire,new WideSeven(),Schema.ReadWideSeven,Schema.WriteWideSeven,v=>v.Text,v=>v.TextLength);
            else if(parts[0]=="4") Replay(wire,new WideFour(),Schema.ReadWideFour,Schema.WriteWideFour,v=>v.Text,v=>v.TextLength);
            else throw new Exception("unexpected bound");
        }
    }
}
