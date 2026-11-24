// The Unity C# leg of test/packet-void (SPEC §4.8): answers every case of
// testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
// line each, for tools/casegate. The pins are the Go oracle's bytes, never
// this writer's. Exit non-zero only for a harness error (no cases.txt, or the
// generated surface beside the wire is wrong).
using System;
using System.IO;
using Serialize;
using Packetvoid;
using static Packetvoid.Schema;
// the union Single, not System.Single (float)
using Single = Packetvoid.Single;

static class Program
{
    enum Kind { Mixed, Tags, Single, Empty, Frame, Batch, Unknown }

    sealed class Value
    {
        public Kind Kind;
        public Mixed Mixed;
        public Tags Tags;
        public Single Single;
        public Empty Empty;
        public Frame Frame;
        public Batch Batch;
    }

    sealed class CaseFailed : Exception
    {
        public CaseFailed(string why) : base(why) { }
    }

    static void Fail(string why) { throw new CaseFailed(why); }

    static Kind KindOf(string type)
    {
        switch (type)
        {
            case "Mixed": return Kind.Mixed;
            case "Tags": return Kind.Tags;
            case "Single": return Kind.Single;
            case "Empty": return Kind.Empty;
            case "Frame": return Kind.Frame;
            case "Batch": return Kind.Batch;
            default: return Kind.Unknown;
        }
    }

    // The payload arm is the type's construction form: entries empty, marker 5.
    static Mixed MakeMixed(MixedType type)
    {
        Mixed m = new Mixed();
        m.Type = type;
        if (type == MixedType.Payload)
        {
            m.Payload = NewDefaultArm();
        }
        return m;
    }

    // The value of each accept case, by name; null for a name this driver
    // does not know, which is FAILED and never written with another value.
    static Value Build(string name)
    {
        Value v = new Value();
        switch (name)
        {
            case "mixed-none": v.Kind = Kind.Mixed; v.Mixed = MakeMixed(MixedType.None); return v;
            case "mixed-ping": v.Kind = Kind.Mixed; v.Mixed = MakeMixed(MixedType.Ping); return v;
            case "mixed-payload": v.Kind = Kind.Mixed; v.Mixed = MakeMixed(MixedType.Payload); return v;
            case "tags-none": v.Kind = Kind.Tags; v.Tags.Type = TagsType.None; return v;
            case "tags-ping": v.Kind = Kind.Tags; v.Tags.Type = TagsType.Ping; return v;
            case "tags-pong": v.Kind = Kind.Tags; v.Tags.Type = TagsType.Pong; return v;
            case "single-none": v.Kind = Kind.Single; v.Single.Type = SingleType.None; return v;
            case "single-ack": v.Kind = Kind.Single; v.Single.Type = SingleType.Ack; return v;
            case "empty-none": v.Kind = Kind.Empty; v.Empty.Type = EmptyType.None; return v;
            case "frame-ping":
                v.Kind = Kind.Frame; v.Frame.Lead = 5; v.Frame.Choice = MakeMixed(MixedType.Ping); v.Frame.Tail = 6;
                return v;
            case "batch-empty": v.Kind = Kind.Batch; v.Batch.ValuesCount = 0; return v;
            case "batch-ping":
                v.Kind = Kind.Batch; v.Batch.ValuesCount = 1; v.Batch.Values[0] = MakeMixed(MixedType.Ping);
                return v;
            case "batch-ping-payload":
                v.Kind = Kind.Batch; v.Batch.ValuesCount = 2;
                v.Batch.Values[0] = MakeMixed(MixedType.Ping);
                v.Batch.Values[1] = MakeMixed(MixedType.Payload);
                return v;
        }
        return null;
    }

    // A payload whose every field differs from its construction form.
    static Mixed DirtyMixed()
    {
        Mixed m = new Mixed();
        m.Type = MixedType.Payload;
        m.Payload.EntriesCount = 2;
        m.Payload.Marker = 2;
        DefaultEntry e = new DefaultEntry();
        e.Retries = 99;
        m.Payload.Entries[0] = e;
        m.Payload.Entries[1] = e;
        return m;
    }

    // Every read lands on storage dirtied first: a selected payload must be
    // constructed with its declared defaults, whatever the storage held.
    static Value Dirty(Kind kind)
    {
        Value v = new Value();
        v.Kind = kind;
        v.Mixed = DirtyMixed();
        v.Tags.Type = TagsType.Pong;
        v.Single.Type = SingleType.Ack;
        v.Frame.Lead = 7; v.Frame.Choice = DirtyMixed(); v.Frame.Tail = 7;
        v.Batch.ValuesCount = 2; v.Batch.Values[0] = DirtyMixed(); v.Batch.Values[1] = DirtyMixed();
        return v;
    }

    static bool Write(WriteStream s, Value v)
    {
        switch (v.Kind)
        {
            case Kind.Mixed: return WriteMixed(s, ref v.Mixed);
            case Kind.Tags: return WriteTags(s, ref v.Tags);
            case Kind.Single: return WriteSingle(s, ref v.Single);
            case Kind.Empty: return WriteEmpty(s, ref v.Empty);
            case Kind.Frame: return WriteFrame(s, ref v.Frame);
            case Kind.Batch: return WriteBatch(s, ref v.Batch);
            default: return false;
        }
    }

    // The generated public read: the one-packet read (SPEC §5).
    static bool Read(ReadStream s, Value v)
    {
        switch (v.Kind)
        {
            case Kind.Mixed: return ReadMixed(s, ref v.Mixed);
            case Kind.Tags: return ReadTags(s, ref v.Tags);
            case Kind.Single: return ReadSingle(s, ref v.Single);
            case Kind.Empty: return ReadEmpty(s, ref v.Empty);
            case Kind.Frame: return ReadFrame(s, ref v.Frame);
            case Kind.Batch: return ReadBatch(s, ref v.Batch);
            default: return false;
        }
    }

    static bool MixedEqual(Mixed got, Mixed want)
    {
        if (got.Type != want.Type) return false;
        if (want.Type != MixedType.Payload) return true;
        if (got.Payload.EntriesCount != want.Payload.EntriesCount || got.Payload.Marker != want.Payload.Marker) return false;
        // every backing entry, used or not, holds what the payload's
        // construction gave it: a read constructs the arm before decoding
        for (int i = 0; i < 2; ++i)
        {
            if (got.Payload.Entries[i].Retries != want.Payload.Entries[i].Retries) return false;
        }
        return true;
    }

    static bool Equal(Value got, Value want)
    {
        switch (want.Kind)
        {
            case Kind.Mixed: return MixedEqual(got.Mixed, want.Mixed);
            case Kind.Tags: return got.Tags.Type == want.Tags.Type;
            case Kind.Single: return got.Single.Type == want.Single.Type;
            case Kind.Empty: return got.Empty.Type == want.Empty.Type;
            case Kind.Frame:
                return got.Frame.Lead == want.Frame.Lead && got.Frame.Tail == want.Frame.Tail &&
                       MixedEqual(got.Frame.Choice, want.Frame.Choice);
            case Kind.Batch:
                if (got.Batch.ValuesCount != want.Batch.ValuesCount) return false;
                for (int i = 0; i < want.Batch.ValuesCount; ++i)
                {
                    if (!MixedEqual(got.Batch.Values[i], want.Batch.Values[i])) return false;
                }
                return true;
            default: return false;
        }
    }

    static byte[] Encode(Value v, out long bits)
    {
        WriteStream w = new WriteStream(new byte[64]);
        if (!Write(w, v) || !w.Ok) Fail("write refused");
        bits = w.BitsProcessed;
        w.Flush();
        if (!w.Ok) Fail("flush refused");
        return w.Data.ToArray();
    }

    static string Hex(byte[] b) { return b.Length == 0 ? "(none)" : BitConverter.ToString(b); }

    static void Accept(string dir, string name, string type, long bits)
    {
        Value want = Build(name);
        if (want == null) Fail("unknown accept case");
        if (want.Kind != KindOf(type)) Fail("case type " + type + ", the driver builds another");
        byte[] pin = File.ReadAllBytes(Path.Combine(dir, name + ".bin"));

        long wrote;
        byte[] bytes = Encode(want, out wrote);
        if (wrote != bits) Fail("write has " + wrote + " bits, want " + bits);
        if (!bytes.AsSpan().SequenceEqual(pin)) Fail("write bytes " + Hex(bytes) + " differ from the pin " + Hex(pin));

        Value got = Dirty(want.Kind);
        ReadStream r = new ReadStream(pin);
        if (!Read(r, got)) Fail("read refused");
        // the accepted end consumes the final byte's zero padding
        if (r.BitsProcessed != (long)pin.Length * 8)
            Fail("read has " + r.BitsProcessed + " bits, want " + (pin.Length * 8));
        if (!Equal(got, want)) Fail("read value differs");

        long again;
        byte[] rewritten = Encode(got, out again);
        if (again != bits || !rewritten.AsSpan().SequenceEqual(pin)) Fail("the read value writes other bytes than the pin");
    }

    static void Refuse(string dir, string name, string type)
    {
        Kind kind = KindOf(type);
        if (kind == Kind.Unknown) Fail("unknown type " + type);
        byte[] pin = File.ReadAllBytes(Path.Combine(dir, name + ".bin"));
        if (Read(new ReadStream(pin), Dirty(kind))) Fail("read accepted");
    }

    static int Main(string[] args)
    {
        if (args.Length != 1)
        {
            Console.Error.WriteLine("usage: packet-void <testdata/wire/packet-void>");
            return 2;
        }
        // The generated surface beside the wire: tag extents, debug names and
        // maximum sizes. A miss exits non-zero, which tools/casegate holds red.
        if (MixedTypeMax != (MixedType)2 || TagsTypeMax != (TagsType)2 || SingleTypeMax != (SingleType)1 ||
            EmptyTypeMax != (EmptyType)0 ||
            EnumNameMixedType(MixedType.Ping) != "Ping" || EnumNameTagsType(TagsType.Pong) != "Pong" ||
            MixedMaxBits != 23 || TagsMaxBits != 2 || SingleMaxBits != 1 || EmptyMaxBits != 0)
        {
            Console.WriteLine("FAILED: packet-void tag extents, debug names or maximum bits");
            return 1;
        }
        string[] lines;
        try
        {
            lines = File.ReadAllLines(Path.Combine(args[0], "cases.txt"));
        }
        catch (Exception e)
        {
            Console.Error.WriteLine("cannot read cases.txt: " + e.Message);
            return 2;
        }
        foreach (string line in lines)
        {
            string[] f = line.Split((char[])null, StringSplitOptions.RemoveEmptyEntries);
            if (f.Length == 0 || f[0].StartsWith("#")) continue;
            string name = f[0];
            try
            {
                if (f.Length < 4) Fail("a case line needs <name> <type> <verdict> <bits>");
                if (f[2] == "accept") Accept(args[0], name, f[1], long.Parse(f[3]));
                else if (f[2] == "refuse") Refuse(args[0], name, f[1]);
                else Fail("verdict " + f[2]);
                Console.WriteLine("case " + name + " ok");
            }
            catch (CaseFailed e)
            {
                Console.WriteLine("case " + name + " FAILED: " + e.Message);
            }
            catch (Exception e)
            {
                Console.WriteLine("case " + name + " FAILED: " + e.GetType().Name + ": " + e.Message.Replace('\n', ' '));
            }
        }
        return 0;
    }
}
