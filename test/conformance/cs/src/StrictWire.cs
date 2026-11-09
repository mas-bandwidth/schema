using System;
using System.Collections.Generic;
using System.Text;

static partial class Program
{
    // A schema-free strict check of a form-1 wire (the harness's
    // strictWireCheck, test/conformance/harness/strictcheck.go): repeated
    // field ids, unreferenced or repeated node records, node indexes past
    // the table, invalid text and non-finite floats each refuse, decidable
    // from the wire alone. The managed C# load accepts some of these; the
    // framing surface applies this check so its verdict is the strict one.
    static ulong WireId(string name)
    {
        ulong h = 0xCBF29CE484222325UL;
        foreach (byte b in Encoding.ASCII.GetBytes(name)) { h ^= b; h *= 0x00000100000001B3UL; }
        return h;
    }

    sealed class StrictWireChecker
    {
        public ulong[] Ids;
        public List<ulong> Edges = new List<ulong>();
        public bool NonFinite;
        public int Depth;

        public static bool Check(byte[] data)
        {
            if (data.Length < 9 || data[0] != 1) return false;
            ulong count = BitConverter.ToUInt64(data, data.Length - 8);
            if (count > (ulong)(data.Length / 8) || count > 4096) return false;
            int span = (int)count * 8 + 8;
            if (span + 1 > data.Length) return false;
            int first = data.Length - span;
            var ids = new ulong[(int)count];
            var seenIds = new HashSet<ulong>();
            for (int i = 0; i < ids.Length; i++)
            {
                ulong id = BitConverter.ToUInt64(data, first + i * 8);
                if (!seenIds.Add(id)) return false;
                ids[i] = id;
            }
            var c = new StrictWireChecker { Ids = ids };
            var body = new ArraySegment<byte>(data, 1, first - 1);
            bool ok = c.CheckBody(body);
            return ok && !c.NonFinite;
        }

        static bool Leb(ArraySegment<byte> buf, int off, out ulong v, out int next)
        {
            v = 0; next = off;
            int shift = 0;
            for (int i = 0; ; i++)
            {
                if (off >= buf.Count || i >= 10) return false;
                byte b = buf.Array[buf.Offset + off];
                off++;
                if (i == 9 && b > 1) return false;
                v |= (ulong)(b & 0x7F) << shift;
                if ((b & 0x80) == 0)
                {
                    if (i > 0 && b == 0) return false;
                    next = off;
                    return true;
                }
                shift += 7;
            }
        }

        static int Width(int kind)
        {
            switch (kind)
            {
                case 1: case 2: case 6: case 20: case 25: return 1;
                case 3: case 7: case 21: case 26: return 2;
                case 4: case 8: case 10: case 22: case 27: return 4;
                case 5: case 9: case 11: case 23: case 28: case 36: return 8;
                case 18: case 19: case 24: case 29: return 16;
                default: return 0;
            }
        }

        static bool FixedWidth(int kind) { return Width(kind) > 0; }

        bool Scalar(ArraySegment<byte> b, int kind)
        {
            if (kind == 1 && b.Count > 0 && b.Array[b.Offset] > 1) return false;
            if (kind == 10 && !float.IsFinite(BitConverter.UInt32BitsToSingle(BitConverter.ToUInt32(b.Array, b.Offset)))) NonFinite = true;
            if (kind == 11 && !double.IsFinite(BitConverter.UInt64BitsToDouble(BitConverter.ToUInt64(b.Array, b.Offset)))) NonFinite = true;
            return true;
        }

        static bool TextValid(ArraySegment<byte> b)
        {
            for (int i = 0; i < b.Count; i++) if (b.Array[b.Offset + i] == 0) return false;
            try { new UTF8Encoding(false, true).GetString(b.Array, b.Offset, b.Count); return true; }
            catch { return false; }
        }

        static bool WideValid(ArraySegment<byte> b)
        {
            if ((b.Count & 1) != 0) return false;
            int n = b.Count / 2;
            for (int i = 0; i < n; i++)
            {
                ushort u = BitConverter.ToUInt16(b.Array, b.Offset + i * 2);
                if (u == 0) return false;
                if (u >= 0xD800 && u <= 0xDBFF)
                {
                    if (i + 1 >= n) return false;
                    ushort low = BitConverter.ToUInt16(b.Array, b.Offset + (i + 1) * 2);
                    if (low < 0xDC00 || low > 0xDFFF) return false;
                    i++;
                }
                else if (u >= 0xDC00 && u <= 0xDFFF) return false;
            }
            return true;
        }

        bool CheckBody(ArraySegment<byte> body)
        {
            ArraySegment<byte> table = default;
            bool present = false;
            if (!Body(body, true, ref table, ref present)) return false;
            var children = new List<List<ulong>> { Edges };
            var blobs = new List<bool>();
            if (present)
            {
                // scan node records
                ulong declared; int off;
                if (!Leb(table, 0, out declared, out off)) return false;
                var records = new List<KeyValuePair<ulong, ArraySegment<byte>>>();
                while (off < table.Count)
                {
                    ulong r; int next;
                    if (!Leb(table, off, out r, out next) || r == 0 || r > (ulong)Ids.Length) return false;
                    off = next;
                    ulong len; int after;
                    if (!Leb(table, off, out len, out after)) return false;
                    off = after;
                    if (len > (ulong)(table.Count - off)) return false;
                    records.Add(new KeyValuePair<ulong, ArraySegment<byte>>(Ids[r - 1], new ArraySegment<byte>(table.Array, table.Offset + off, (int)len)));
                    off += (int)len;
                }
                if (off != table.Count || declared != (ulong)records.Count) return false;
                ulong bytesId = WireId("bytes"), stringId = WireId("string");
                foreach (var rec in records)
                {
                    Edges = new List<ulong>();
                    if (rec.Key == bytesId || rec.Key == stringId)
                    {
                        if (rec.Key == stringId && !TextValid(rec.Value)) return false;
                        blobs.Add(true);
                    }
                    else
                    {
                        ArraySegment<byte> t2 = default; bool p2 = false;
                        if (!Body(rec.Value, false, ref t2, ref p2)) return false;
                        blobs.Add(false);
                    }
                    children.Add(Edges);
                }
            }
            return Graph(children, blobs);
        }

        bool Graph(List<List<ulong>> children, List<bool> blobs)
        {
            int n = children.Count;
            var names = new int[n];
            foreach (var outs in children)
            {
                foreach (ulong idx in outs)
                {
                    if (idx == 0) continue;
                    if (idx - 1 >= (ulong)n) return false;
                    names[idx - 1]++;
                }
            }
            for (int k = 0; k < blobs.Count; k++) if (blobs[k] && names[k + 1] > 1) return false;
            var mark = new int[n];
            var stack = new List<int[]> { new int[] { 0, 0 } };
            mark[0] = 1;
            while (stack.Count > 0)
            {
                int[] top = stack[stack.Count - 1];
                var outs = children[top[0]];
                if (top[1] == outs.Count) { mark[top[0]] = 2; stack.RemoveAt(stack.Count - 1); continue; }
                ulong idx = outs[top[1]++];
                if (idx == 0) continue;
                int t = (int)(idx - 1);
                if (mark[t] == 1) return false;
                if (mark[t] == 2) continue;
                mark[t] = 1;
                stack.Add(new int[] { t, 0 });
            }
            for (int k = 1; k < n; k++) if (mark[k] == 0) return false;
            return true;
        }

        bool Body(ArraySegment<byte> buf, bool root, ref ArraySegment<byte> table, ref bool present)
        {
            int off = 0;
            HashSet<ulong> seen = null;
            while (true)
            {
                ulong r; int next;
                if (!Leb(buf, off, out r, out next)) return false;
                off = next;
                if (r == 0) return off == buf.Count;
                if (r > (ulong)Ids.Length) return false;
                ulong id = Ids[r - 1];
                if (seen == null) seen = new HashSet<ulong>();
                if (!seen.Add(id)) return false;
                if (off >= buf.Count) return false;
                int kind = buf.Array[buf.Offset + off];
                off++;
                if (id == 0xFFFFFFFFFFFFFFFEUL || id == 0xFFFFFFFFFFFFFFFDUL) return false;
                if (id == 0xFFFFFFFFFFFFFFFFUL)
                {
                    if (!root || kind != 12) return false;
                    ulong ln; int after;
                    if (!Leb(buf, off, out ln, out after) || ln > (ulong)(buf.Count - after)) return false;
                    table = new ArraySegment<byte>(buf.Array, buf.Offset + after, (int)ln);
                    present = true;
                    off = after + (int)ln;
                    continue;
                }
                if (!Payload(buf, off, kind, out off)) return false;
            }
        }

        bool Payload(ArraySegment<byte> buf, int off, int kind, out int result)
        {
            result = off;
            if (FixedWidth(kind))
            {
                int w = Width(kind);
                if (w > buf.Count - off) return false;
                result = off + w;
                return Scalar(new ArraySegment<byte>(buf.Array, buf.Offset + off, w), kind);
            }
            switch (kind)
            {
                case 17:
                    {
                        ulong idx; int next;
                        if (!Leb(buf, off, out idx, out next)) return false;
                        Edges.Add(idx);
                        result = next; return true;
                    }
                case 30:
                    {
                        ulong r; int next;
                        if (!Leb(buf, off, out r, out next) || r > (ulong)Ids.Length) return false;
                        result = next; return true;
                    }
                case 15:
                    return Union(buf, off, out result);
                case 12: case 33: case 31: case 32: case 13: case 14: case 16:
                    {
                        ulong ln; int next;
                        if (!Leb(buf, off, out ln, out next) || ln > (ulong)(buf.Count - next)) return false;
                        int end = next + (int)ln;
                        result = end;
                        return Content(new ArraySegment<byte>(buf.Array, buf.Offset + next, (int)ln), kind);
                    }
                default:
                    return false;
            }
        }

        bool Content(ArraySegment<byte> b, int kind)
        {
            if (FixedWidth(kind)) return b.Count == Width(kind) && Scalar(b, kind);
            switch (kind)
            {
                case 12: return TextValid(b);
                case 33: return WideValid(b);
                case 31: return true;
                case 32: return b.Count == 0;
                case 13: case 14: case 16:
                    {
                        if (Depth >= 64) return false;
                        Depth++;
                        bool ok;
                        if (kind == 13) { ArraySegment<byte> t = default; bool p = false; ok = Body(b, false, ref t, ref p); }
                        else if (kind == 14) ok = ArrayCheck(b);
                        else ok = KeyedCheck(b);
                        Depth--;
                        return ok;
                    }
                case 17: case 30: case 15:
                    {
                        int end; bool ok = Payload(b, 0, kind, out end);
                        return ok && end == b.Count;
                    }
                default: return false;
            }
        }

        bool ArrayCheck(ArraySegment<byte> b)
        {
            int ek; ulong count; int off;
            if (!ArrayHeader(b, out ek, out count, out off)) return false;
            for (ulong i = 0; i < count; i++) if (!Payload(b, off, ek, out off)) return false;
            return off == b.Count;
        }

        bool KeyedCheck(ArraySegment<byte> b)
        {
            int ek; ulong count; int off;
            if (!ArrayHeader(b, out ek, out count, out off)) return false;
            var seen = new HashSet<ulong>();
            for (ulong i = 0; i < count; i++)
            {
                ulong key; int next;
                if (!Leb(b, off, out key, out next) || key == 0 || key > (ulong)Ids.Length) return false;
                if (!seen.Add(key)) return false;
                ulong ln; int after;
                if (!Leb(b, next, out ln, out after) || ln > (ulong)(b.Count - after)) return false;
                off = after + (int)ln;
                if (!Content(new ArraySegment<byte>(b.Array, b.Offset + after, (int)ln), ek)) return false;
            }
            return off == b.Count;
        }

        static bool ArrayHeader(ArraySegment<byte> b, out int ek, out ulong count, out int off)
        {
            ek = 0; count = 0; off = 0;
            if (b.Count < 2) return false;
            int next;
            if (!Leb(b, 1, out count, out next) || count > int.MaxValue) return false;
            ek = b.Array[b.Offset];
            off = next;
            return true;
        }

        bool Union(ArraySegment<byte> buf, int off, out int result)
        {
            result = off;
            ulong r; int next;
            if (!Leb(buf, off, out r, out next)) return false;
            if (r == 0) { result = next; return true; }
            if (r > (ulong)Ids.Length || next >= buf.Count) return false;
            int kind = buf.Array[buf.Offset + next];
            ulong ln; int after;
            if (!Leb(buf, next + 1, out ln, out after) || ln > (ulong)(buf.Count - after)) return false;
            int end = after + (int)ln;
            result = end;
            return Content(new ArraySegment<byte>(buf.Array, buf.Offset + after, (int)ln), kind);
        }
    }

    static bool StrictWireCheck(byte[] data) { return StrictWireChecker.Check(data); }
}
