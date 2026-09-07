using System;
using System.IO;
using System.Reflection;
using Serialize;
using D = Packetdefaults;
using P = Packetplain;

static class Program
{
    static void Check(bool ok, string name) { if (!ok) throw new Exception("FAILED: " + name); }
    // Compare all public storage, including unused tails, rather than re-encoding
    // expected objects through the same generated codec under test.
    static bool Equal(object a, object b)
    {
        if (a == null || b == null) return a == b;
        if (a.GetType() != b.GetType()) return false;
        if (a is Array aa && b is Array bb)
        {
            if (aa.Length != bb.Length) return false;
            for (int i = 0; i < aa.Length; i++) if (!Equal(aa.GetValue(i), bb.GetValue(i))) return false;
            return true;
        }
        if (a.GetType().IsValueType) return a.Equals(b);
        foreach (FieldInfo f in a.GetType().GetFields(BindingFlags.Public | BindingFlags.Instance))
            if (!Equal(f.GetValue(a), f.GetValue(b))) return false;
        return true;
    }
    static D.Sample Zero() => new D.Sample { Name = new byte[6], NameLength = 0,
        Token = new byte[4], TokenLength = 0, Caps = 0, EmptyName = new byte[3],
        EmptyNameLength = 0, EmptyToken = new byte[2], EmptyTokenLength = 0, EmptyCaps = 0 };
    static D.Sample Sample()
    {
        D.Sample s = Zero(); s.Name = new byte[] { 195, 169, 240, 144, 128, 128 }; s.NameLength = 6;
        s.Token = new byte[] { 92, 110, 92, 116 }; s.TokenLength = 4; s.Caps = 5; return s;
    }
    static D.Sample Short()
    {
        D.Sample s = Zero(); s.Name[0] = 65; s.NameLength = 1;
        s.Token[1] = 255; s.TokenLength = 2; s.Caps = 2; return s;
    }
    static D.Sample Dirty()
    {
        D.Sample s = Zero(); s.Name.AsSpan().Fill(100); s.NameLength = 6;
        s.Token.AsSpan().Fill(161); s.TokenLength = 4; s.Caps = 7;
        s.EmptyName.AsSpan().Fill(111); s.EmptyNameLength = 3;
        s.EmptyToken.AsSpan().Fill(177); s.EmptyTokenLength = 2; s.EmptyCaps = 7; return s;
    }
    static (byte[], long) Write<T>(T value, Func<WriteStream, T, bool> encode)
    {
        WriteStream stream = new WriteStream(new byte[4096]);
        Check(encode(stream, value) && stream.Ok, "write"); long bits = stream.BitsProcessed;
        stream.Flush(); Check(stream.Ok, "flush"); return (stream.Data.ToArray(), bits);
    }
    static void Read<T>(byte[] bytes, long bits, T initial, T want, Func<ReadStream, T, bool> decode)
    {
        ReadStream stream = new ReadStream(bytes);
        Check(decode(stream, initial) && stream.Ok, "read");
        Check(stream.BitsProcessed == bits, "read consumed bits");
        Check(Equal(initial, want), "read values and backing storage");
    }
    static void Golden<T>(string dir, string name, T value, T initial, T want,
        Func<WriteStream, T, bool> encode, Func<ReadStream, T, bool> decode)
    {
        byte[] bytes = File.ReadAllBytes(Path.Combine(dir, name + ".bin"));
        long bits = long.Parse(File.ReadAllText(Path.Combine(dir, name + ".bits")));
        var wire = Write(value, encode);
        Check(wire.Item2 == bits && wire.Item1.AsSpan().SequenceEqual(bytes), "C++ byte/bit pin " + name);
        Read(bytes, bits, initial, want, decode);
    }
    static D.Sample OverlayWant(D.Sample initial, D.Sample sent)
    {
        sent.Name.AsSpan(0, sent.NameLength).CopyTo(initial.Name); initial.NameLength = sent.NameLength;
        sent.Token.AsSpan(0, sent.TokenLength).CopyTo(initial.Token); initial.TokenLength = sent.TokenLength;
        initial.Caps = sent.Caps; initial.EmptyNameLength = 0; initial.EmptyTokenLength = 0; initial.EmptyCaps = 0;
        return initial;
    }
    static void Main(string[] args)
    {
        string dir = args[0];
        Check(Equal(new D.Sample(), Sample()), "packet-default constructor bytes");
        D.Sample reused = Dirty(); byte[] nameBuffer = reused.Name; byte[] tokenBuffer = reused.Token;
        D.Schema.InitSample(reused);
        Check(Equal(reused, Sample()), "Init restores default bytes and zero tails");
        Check(ReferenceEquals(nameBuffer, reused.Name) && ReferenceEquals(tokenBuffer, reused.Token), "Init retains buffers");
        D.Schema.ZeroSample(reused); Check(Equal(reused, Zero()), "Zero stays distinct from Init");
        D.EmptyOnly empty = new D.EmptyOnly();
        Check(empty.NameLength == 0 && empty.TokenLength == 0 && empty.Caps == 0 &&
            Equal(empty.Name, new byte[2]) && Equal(empty.Token, new byte[1]), "explicit empty defaults");
        D.Prefix prefix = new D.Prefix();
        Check(Equal(prefix.Name, new byte[] { 195, 169, 0, 0, 0 }) && prefix.NameLength == 2 &&
            Equal(prefix.Token, new byte[] { 92, 110, 0, 0, 0 }) && prefix.TokenLength == 2, "short literal zero tails");
        prefix.Name.AsSpan().Fill(255); prefix.Token.AsSpan().Fill(255); D.Schema.InitPrefix(prefix);
        Check(Equal(prefix, new D.Prefix()), "Init clears stale short-literal tails");
        D.WideMask wide = new D.WideMask();
        Check(wide.High == 1UL << 63 && wide.All == ulong.MaxValue, "unsigned 64-bit default masks");
        byte[] wideBytes = new byte[16]; wideBytes[7] = 128; wideBytes.AsSpan(8).Fill(255);
        var wideWire = Write(wide, D.Schema.WriteWideMask);
        Check(wideWire.Item2 == 128 && Equal(wideWire.Item1, wideBytes), "independent 64-bit wire");
        Read(wideBytes, 128, new D.WideMask { High = 0, All = 0 }, wide, D.Schema.ReadWideMask);
        var split = new D.SplitMask { Lead = 5, Mask = 1UL << 32, Tail = 2 };
        var splitWire = Write(split, D.Schema.WriteSplitMask);
        Check(splitWire.Item2 == 38 && Equal(splitWire.Item1, new byte[] { 5, 0, 0, 0, 40 }), "independent 33-bit wire");
        Read(new byte[] { 5, 0, 0, 0, 40 }, 38, new D.SplitMask(), split, D.Schema.ReadSplitMask);
        P.Sample plain = new P.Sample { Name = Sample().Name, NameLength = 6, Token = Sample().Token, TokenLength = 4, Caps = 5 };
        var plainWire = Write(plain, P.Schema.WriteSample); var defaultWire = Write(new D.Sample(), D.Schema.WriteSample);
        Check(plainWire.Item2 == defaultWire.Item2 && Equal(plainWire.Item1, defaultWire.Item1), "defaultless twin has identical wire");
        Golden(dir, "sample-defaults", new D.Sample(), Zero(), Sample(), D.Schema.WriteSample, D.Schema.ReadSample);
        D.Batch batch = new D.Batch();
        Check(Equal(batch.Head, Sample()) && batch.CountedCount == 1, "nested defaults and count birth");
        foreach (D.Sample s in batch.Items) Check(Equal(s, Sample()), "fixed backing defaults");
        foreach (D.Sample s in batch.Counted) Check(Equal(s, Sample()), "counted backing defaults");
        D.Batch initial = new D.Batch(), want = new D.Batch();
        initial.Counted[1] = Dirty(); want.Counted[1] = Dirty(); initial.Counted[2] = Short(); want.Counted[2] = Short();
        Golden(dir, "batch-defaults", batch, initial, want, D.Schema.WriteBatch, D.Schema.ReadBatch);
        D.ZeroCount zeroCount = new D.ZeroCount();
        Check(zeroCount.ItemsCount == 0 && Equal(zeroCount.Items, new D.Sample[] { Sample(), Sample() }), "zero-count backing defaults");
        Golden(dir, "zero-count", zeroCount, new D.ZeroCount { Items = new D.Sample[] { Dirty(), Short() }, ItemsCount = 2 },
            new D.ZeroCount { Items = new D.Sample[] { Dirty(), Short() }, ItemsCount = 0 }, D.Schema.WriteZeroCount, D.Schema.ReadZeroCount);
        Check(new D.Conditional().Enabled && Equal(new D.Conditional().Value, Sample()), "conditional defaults");
        Golden(dir, "conditional-on", new D.Conditional(), new D.Conditional { Enabled = false, Value = Zero() },
            new D.Conditional(), D.Schema.WriteConditional, D.Schema.ReadConditional);
        Golden(dir, "conditional-off", new D.Conditional { Enabled = false }, new D.Conditional { Value = Dirty() },
            new D.Conditional { Enabled = false, Value = Zero() }, D.Schema.WriteConditional, D.Schema.ReadConditional);
        Golden(dir, "choice-sample", new D.Choice { Type = D.ChoiceType.Sample }, new D.Choice { Type = D.ChoiceType.Conditional },
            new D.Choice { Type = D.ChoiceType.Sample }, D.Schema.WriteChoice, D.Schema.ReadChoice);
        Golden(dir, "sample-short", Short(), Dirty(), OverlayWant(Dirty(), Short()), D.Schema.WriteSample, D.Schema.ReadSample);
        Golden(dir, "sample-empty", Zero(), Dirty(), OverlayWant(Dirty(), Zero()), D.Schema.WriteSample, D.Schema.ReadSample);
        foreach (D.Sample sent in new D.Sample[] { Short(), Zero() })
        {
            var wire = Write(new D.Choice { Type = D.ChoiceType.Sample, Sample = sent }, D.Schema.WriteChoice);
            D.Choice choice = new D.Choice { Type = D.ChoiceType.Conditional };
            D.Sample arm = choice.Sample; byte[] buffer = arm.Name;
            for (int attempt = 0; attempt < 2; attempt++)
            {
                arm.Name.AsSpan().Fill(100); arm.Token.AsSpan().Fill(161);
                Read(wire.Item1, wire.Item2, choice, new D.Choice { Type = D.ChoiceType.Sample,
                    Sample = OverlayWant(Sample(), sent) }, D.Schema.ReadChoice);
                Check(ReferenceEquals(arm, choice.Sample) && ReferenceEquals(buffer, arm.Name), "union selection retains objects and buffers");
            }
        }
        var off = Write(new D.Choice { Type = D.ChoiceType.Conditional, Conditional = new D.Conditional { Enabled = false } }, D.Schema.WriteChoice);
        D.Choice offChoice = new D.Choice { Type = D.ChoiceType.Sample };
        for (int attempt = 0; attempt < 2; attempt++)
            Read(off.Item1, off.Item2, offChoice, new D.Choice { Type = D.ChoiceType.Conditional,
                Conditional = new D.Conditional { Enabled = false, Value = Zero() } }, D.Schema.ReadChoice);
        // Warm the code first, then measure only in-place initialization.
        for (int i = 0; i < 100; i++) D.Schema.InitSample(reused);
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 1000; i++) D.Schema.InitSample(reused);
        Check(GC.GetAllocatedBytesForCurrentThread() == before, "Init allocates nothing");
        Console.WriteLine("packet defaults C#: constructors, eight C++ goldens and reused storage OK");
    }
}
