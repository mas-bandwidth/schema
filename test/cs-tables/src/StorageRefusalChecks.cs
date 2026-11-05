using System;
using Demo = Tabledemo;
using B = Backenddemo;

// A COUNT OR A LENGTH PAST ITS STORAGE IS REFUSED IN EVERY BUILD, AND A REFUSED
// WRITE LEAVES THE CALLER'S BUFFER UNTOUCHED (docs/SPEC.md §5.1). Each writer
// gets a buffer poisoned with one byte, a value that overruns its storage (or,
// for the text writer, ill-formed text and a buffer one byte short), and must
// answer -1 with every byte of the poison intact. This leg runs Debug and
// Release, so both configurations hold it.
static partial class Program
{
    const byte Poison = 0xA5;

    static byte[] Poisoned(int n) { byte[] b = new byte[n]; Array.Fill(b, Poison); return b; }

    static bool Intact(byte[] b) { foreach (byte x in b) { if (x != Poison) { return false; } } return true; }

    static Demo.RootConfig StorageRoot()
    {
        Demo.RootConfig root = new Demo.RootConfig();
        root.VersionNote[0] = (byte)'a'; root.VersionNoteLength = 1;
        root.WeaponsCount = 1;
        return root;
    }

    static void TestStorageRefusal()
    {
        foreach ((string what, Action<Demo.RootConfig> spoil) in new (string, Action<Demo.RootConfig>)[] {
            ("a weapons count past its storage", r => r.WeaponsCount = r.Weapons.Length + 1),
            ("a negative weapons count", r => r.WeaponsCount = -1),
            ("a version_note length past its storage", r => r.VersionNoteLength = r.VersionNote.Length + 1),
            ("a negative version_note length", r => r.VersionNoteLength = -1),
        })
        {
            Demo.RootConfig good = StorageRoot();
            Demo.RootConfig bad = StorageRoot(); spoil(bad);

            long fixedNeed = Demo.Schema.RootConfigFixedMeasure(1);
            Check(Demo.Schema.RootConfigFixedSave(good, Poisoned((int)fixedNeed)) == fixedNeed, "storage: FixedSave writes the in-bound twin of " + what);
            byte[] fixedBuffer = Poisoned((int)fixedNeed);
            Check(Demo.Schema.RootConfigFixedSave(bad, fixedBuffer) == -1, "storage: FixedSave refuses " + what);
            Check(Intact(fixedBuffer), "storage: FixedSave leaves the buffer untouched on " + what);

            byte[] variableBuffer = Poisoned(4096);
            Check(Demo.Schema.RootConfigSave(good, Poisoned(4096)) > 0, "storage: Save writes the in-bound twin of " + what);
            Check(Demo.Schema.RootConfigSave(bad, variableBuffer) == -1, "storage: Save refuses " + what);
            Check(Intact(variableBuffer), "storage: Save leaves the buffer untouched on " + what);

            byte[] jsonBuffer = Poisoned(65536);
            Check(Demo.Schema.RootConfigToJson(good, Poisoned(65536)) > 0, "storage: ToJson writes the in-bound twin of " + what);
            Check(Demo.Schema.RootConfigToJson(bad, jsonBuffer) == -1, "storage: ToJson refuses " + what);
            Check(Intact(jsonBuffer), "storage: ToJson leaves the buffer untouched on " + what);
        }

        // the text writer's own refusals answer before its first byte too
        {
            Demo.RootConfig illFormed = StorageRoot();
            illFormed.VersionNote[0] = 0x61; illFormed.VersionNote[1] = 0xFF; illFormed.VersionNote[2] = 0x62; illFormed.VersionNoteLength = 3;
            byte[] jsonBuffer = Poisoned(65536);
            Check(Demo.Schema.RootConfigToJson(illFormed, jsonBuffer) == -1, "storage: ToJson refuses ill-formed text");
            Check(Intact(jsonBuffer), "storage: ToJson leaves the buffer untouched on ill-formed text");
            Demo.RootConfig good = StorageRoot();
            long need = Demo.Schema.RootConfigToJsonMeasure(good);
            byte[] shortBuffer = Poisoned((int)need - 1);
            Check(Demo.Schema.RootConfigToJson(good, shortBuffer) == -1, "storage: ToJson refuses a buffer one byte short");
            Check(Intact(shortBuffer), "storage: ToJson leaves a short buffer untouched");
            Check(Demo.Schema.RootConfigToJson(good, Poisoned((int)need)) == need, "storage: ToJson writes into a buffer of exactly its measure");
        }

        // SaveMessages: a sku length past its storage
        foreach ((string what, int length) in new (string, int)[] { ("a sku length past its storage", 33), ("a negative sku length", -1) })
        {
            B.StorePurchase purchase = new B.StorePurchase();
            purchase.Sku[0] = (byte)'a'; purchase.SkuLength = 1;
            Check(B.Schema.StorePurchaseSaveMessages(new[] { purchase }, Poisoned(4096)) > 0, "storage: SaveMessages writes the in-bound twin of " + what);
            purchase.SkuLength = length;
            byte[] messageBuffer = Poisoned(4096);
            Check(B.Schema.StorePurchaseSaveMessages(new[] { purchase }, messageBuffer) == -1, "storage: SaveMessages refuses " + what);
            Check(Intact(messageBuffer), "storage: SaveMessages leaves the buffer untouched on " + what);
        }
    }
}
