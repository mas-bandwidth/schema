using System;
using V1 = Tblv1;

static partial class Program
{
    sealed class PayloadChild
    {
        public uint Value = 0;
    }

    sealed class PayloadRoot
    {
        public PayloadChild Child = new PayloadChild();
        public PayloadChild[] Items = { new PayloadChild { Value = 42 }, new PayloadChild(), new PayloadChild() };
        public int ItemsCount = 2;
        public byte[] Text = new byte[128];
        public int TextLength = 0;
        public char[] Wide = { 'A', '\u03bb' };
        public uint[] Keys = { 0, 9 };
    }

    // Descriptor fixtures exercise the generated generic writer without adding
    // schema-dependent expected bytes. Padding consists of absent scalar fields;
    // the visible fields cross omitted slots, and a selected empty child and an
    // empty optional string both carry their distinct length prefixes.
    static V1.TableTypeInfo PayloadRootType(int fieldCount, bool variable)
    {
        ulong red = FieldId("Red"), blue = FieldId("Blue");
        var child = new V1.TableTypeInfo {
            Name = "PayloadChild", NumFields = 1,
            Fields = new[] { new V1.TableFieldInfo {
                Name = "value", Id = FieldId("value"), Kind = 4,
                GetRaw = (o, i) => ((PayloadChild)o).Value
            } }
        };
        var visible = new[] {
            new V1.TableFieldInfo {
                Name = "omitted", Id = FieldId("omitted"), Kind = 4,
                GetRaw = (o, i) => 0
            },
            new V1.TableFieldInfo {
                Name = "child", Id = FieldId("child"), Kind = 13, Optional = true,
                GetPresent = o => true, GetChild = (o, i) => ((PayloadRoot)o).Child,
                TableRef = () => child
            },
            new V1.TableFieldInfo {
                Name = "items", Id = FieldId("items"), Kind = 13, IsArray = true,
                Counted = true, ArrayBound = 3, GetCount = o => ((PayloadRoot)o).ItemsCount,
                GetChild = (o, i) => ((PayloadRoot)o).Items[i], TableRef = () => child
            },
            new V1.TableFieldInfo {
                Name = "guarded", Id = FieldId("guarded"), Kind = 12, Counted = true,
                ArrayBound = 128, WireGuard = o => false,
                GetCount = o => throw new InvalidOperationException("absent guarded field was visited"),
                GetBuffer = o => ((PayloadRoot)o).Text
            },
            new V1.TableFieldInfo {
                Name = "text", Id = FieldId("text"), Kind = 12, Optional = true, Counted = true,
                ArrayBound = 128, GetPresent = o => true,
                GetCount = o => ((PayloadRoot)o).TextLength, GetBuffer = o => ((PayloadRoot)o).Text
            },
            new V1.TableFieldInfo {
                Name = "wide", Id = FieldId("wide"), Kind = 33, Counted = true,
                ArrayBound = 2, GetCount = o => 2, GetChars = o => ((PayloadRoot)o).Wide
            },
            new V1.TableFieldInfo {
                Name = "keys", Id = FieldId("keys"), Kind = 4, IsArray = true, ArrayBound = 2,
                GetRaw = (o, i) => ((PayloadRoot)o).Keys[i],
                KeyName = key => key == 1 ? "Red" : "Blue",
                KeyId = key => key == 1 ? red : blue
            },
            new V1.TableFieldInfo {
                Name = "tail", Id = FieldId("tail"), Kind = 4, GetRaw = (o, i) => 7
            }
        };
        Check(fieldCount >= visible.Length, "payload fixture has room for its visible fields");
        var fields = new V1.TableFieldInfo[fieldCount];
        int padding = fieldCount - visible.Length;
        for (int i = 0; i < padding; i++)
        {
            string name = "unused_" + i;
            fields[i] = new V1.TableFieldInfo { Name = name, Id = FieldId(name), Kind = 4, GetRaw = (o, n) => 0 };
        }
        Array.Copy(visible, 0, fields, padding, visible.Length);
        return new V1.TableTypeInfo { Name = "PayloadRoot", Variable = variable, NumFields = fieldCount, Fields = fields };
    }

    static byte[] PayloadRootGolden(PayloadRoot value)
    {
        // Independent form-1 bytes. First-use ids are child, items, value,
        // text, wide, keys, Blue, tail. The empty array element is body [0].
        byte[] child = Join(new byte[] { 3,4 }, U32(42), new byte[] { 0 });
        byte[] items = Join(new byte[] { 13,2 }, Var((ulong)child.Length), child, new byte[] { 1,0 });
        byte[] text = value.Text.AsSpan(0, value.TextLength).ToArray();
        byte[] keys = new byte[] { 4,1,7,4,9,0,0,0 };
        return Fixture(Join(
            new byte[] { 1,13,1,0,2,14 }, Var((ulong)items.Length), items,
            new byte[] { 4,12 }, Var((ulong)text.Length), text,
            new byte[] { 5,33,4,65,0,187,3,6,16 }, Var((ulong)keys.Length), keys,
            new byte[] { 8,4 }, U32(7), new byte[] { 0 }),
            "child", "items", "value", "text", "wide", "keys", "Blue", "tail");
    }

    static void CheckPayloadRootRefusal(PayloadRoot value, V1.TableTypeInfo type, byte[] buffer, ulong[] ids)
    {
        Array.Fill(buffer, (byte)0xa5);
        Check(V1.Schema.TableWire.Save(value, type, buffer, ids, false) == -1 &&
            Array.TrueForAll(buffer, b => b == 0xa5), "payload sizing refusal leaves all output untouched");
    }

    static void TestRootPayloadSizes()
    {
        // Exercise the stack ceiling from both sides; no field count is a
        // benchmark shape. A graph-marked descriptor takes the existing graph
        // path and must produce these same bytes when it has no pointer nodes.
        foreach (int fields in new[] { 8, 255, 256, 257 })
        foreach (bool variable in new[] { false, true })
        {
            var type = PayloadRootType(fields, variable);
            var value = new PayloadRoot();
            Array.Fill(value.Text, (byte)'x');
            ulong[] ids = new ulong[16];
            foreach (int length in new[] { 0, 127, 128 })
            {
                value.TextLength = length;
                byte[] expected = PayloadRootGolden(value);
                Check(V1.Schema.TableWire.Save(value, type, Span<byte>.Empty, ids, true) == expected.Length,
                    "payload fixture measure matches independent wire size");
                byte[] actual = new byte[expected.Length];
                Check(V1.Schema.TableWire.Save(value, type, actual, ids, false) == expected.Length &&
                    actual.AsSpan().SequenceEqual(expected), "root payload sizes preserve exact independent bytes");
                byte[] roomy = new byte[expected.Length + 11];
                Array.Fill(roomy, (byte)0xa5);
                Check(V1.Schema.TableWire.Save(value, type, roomy, ids, false) == expected.Length &&
                    roomy.AsSpan(0, expected.Length).SequenceEqual(expected), "root payload sizes preserve roomy output bytes");
                foreach (byte b in roomy.AsSpan(expected.Length)) { Check(b == 0xa5, "save leaves output suffix untouched"); }
                CheckPayloadRootRefusal(value, type, new byte[expected.Length - 1], ids);
                CheckPayloadRootRefusal(value, type, Array.Empty<byte>(), ids);

                if (!variable)
                {
                    for (int i = 0; i < 1000; i++) { V1.Schema.TableWire.Save(value, type, actual, ids, false); }
                    long before = GC.GetAllocatedBytesForCurrentThread();
                    for (int i = 0; i < 1000; i++) { V1.Schema.TableWire.Save(value, type, actual, ids, false); }
                    Check(GC.GetAllocatedBytesForCurrentThread() == before, "fixed root payload sizing allocates zero after warmup");
                }
            }

            byte[] refused = new byte[1024];
            value.ItemsCount = -1;
            CheckPayloadRootRefusal(value, type, refused, ids);
            value.ItemsCount = 4;
            CheckPayloadRootRefusal(value, type, refused, ids);
            value.ItemsCount = 2;
            value.TextLength = -1;
            CheckPayloadRootRefusal(value, type, refused, ids);
            value.TextLength = value.Text.Length + 1;
            CheckPayloadRootRefusal(value, type, refused, ids);
            value.TextLength = 0;
            CheckPayloadRootRefusal(value, type, refused, new ulong[7]);
        }
    }
}
