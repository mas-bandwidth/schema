package cstable

// tableWireCheckSource is the C# leg's port of the Go engine's wire-alone walk
// (internal/tablewire/wirecheck.go): THE CONDITIONS DECIDABLE FROM THE WIRE
// ALONE (docs/SPEC-TABLES.md §6.5), applied to EVERY byte of a form-1 file
// before anything is decoded, in fields this reader declares and in those it
// does not. tla/ReadOrder.tla models the split (WireDamage, Unreachable, Cycle
// and BlobShared are SpecRefuses' wire-alone disjuncts, walked over every slot,
// known or not).
//
// The walk allocates nothing for a file without pointers. A file with pointers
// rents its edge, name and mark arrays from ArrayPool and returns them.
const tableWireCheckSource = `
    // WireCheck's answers: the file is whole, damaged, holds a count past the
    // storage cap (count_over_extent_cap), or names one blob record from two
    // slots (blob_shared). A non-finite float is answered apart, so the decode
    // can name the field when this reader declares it.
    internal const int WireWhole = 0, WireDamaged = 1, WireCountCap = 2, WireBlobShared = 3;
    internal static int WireCheck(ReadOnlySpan<byte> body, ReadOnlySpan<byte> ids, bool announcement, out bool nonFinite)
    {
        WireWalk w = new WireWalk { Ids = ids, Words = (ids.Length / 8 + 64) / 64, Announcement = announcement };
        try
        {
            w.Root(body);
            nonFinite = w.NonFinite;
            return w.Fail;
        }
        finally { w.Release(); }
    }
    ref struct WireWalk
    {
        public ReadOnlySpan<byte> Ids, Table;
        public int Words, Depth, Fail, Nodes, EdgeCount;
        public bool Announcement, NonFinite, Present;
        // Edges holds every non-null kind 17 payload in wire order, the root
        // body's first and then each record's; Starts[k] is node k's first edge
        // (node 0 the root, node k record k).
        public ulong[] Edges;
        public int[] Starts;
        public void Release()
        {
            if (Edges != null) { System.Buffers.ArrayPool<ulong>.Shared.Return(Edges); Edges = null; }
            if (Starts != null) { System.Buffers.ArrayPool<int>.Shared.Return(Starts); Starts = null; }
        }
        bool Damage() { if (Fail == WireWhole) { Fail = WireDamaged; } return false; }
        static bool Leb(ReadOnlySpan<byte> b, ref int off, out ulong v)
        {
            v = 0;
            for (int i = 0; i < 10; i++)
            {
                if (off + i >= b.Length) { return false; }
                byte c = b[off + i];
                if (i == 9 && c > 1) { return false; }
                v |= (ulong)(c & 127) << (7 * i);
                if (c < 128) { if (i > 0 && c == 0) { return false; } off += i + 1; return true; }
            }
            return false;
        }
        void Edge(ulong index)
        {
            if (index == 0) { return; }
            if (Edges == null) { Edges = System.Buffers.ArrayPool<ulong>.Shared.Rent(16); }
            else if (EdgeCount == Edges.Length)
            {
                ulong[] grown = System.Buffers.ArrayPool<ulong>.Shared.Rent(Edges.Length * 2);
                Array.Copy(Edges, grown, EdgeCount);
                System.Buffers.ArrayPool<ulong>.Shared.Return(Edges); Edges = grown;
            }
            Edges[EdgeCount++] = index;
        }
        public void Root(ReadOnlySpan<byte> body)
        {
            if (!Body(body, true)) { return; }
            int rootEdges = EdgeCount;
            int records = 0;
            if (Present)
            {
                // THE AUTHORITATIVE RECORD SCAN (docs/SPEC-TABLES.md §3.1): the
                // records are read until the payload is consumed, and a node_count
                // the scan does not match is damage.
                ReadOnlySpan<byte> t = Table;
                int off = 0;
                if (!Leb(t, ref off, out ulong declared)) { Damage(); return; }
                int first = off;
                while (off < t.Length)
                {
                    if (!Leb(t, ref off, out ulong reference) || reference == 0 || reference > (ulong)(Ids.Length / 8)) { Damage(); return; }
                    if (!Leb(t, ref off, out ulong length) || length > (ulong)(t.Length - off)) { Damage(); return; }
                    off += (int)length; records++;
                }
                if (declared != (ulong)records) { Damage(); return; }
                Starts = System.Buffers.ArrayPool<int>.Shared.Rent(records + 2);
                Starts[0] = 0; Starts[1] = rootEdges;
                off = first;
                for (int k = 0; k < records; k++)
                {
                    Leb(t, ref off, out ulong reference); Leb(t, ref off, out ulong length);
                    ReadOnlySpan<byte> rec = t.Slice(off, (int)length); off += (int)length;
                    ulong id = Read64(Ids, (int)(reference - 1) * 8);
                    // A BLOB RECORD IS ITS BYTES (§2.5, §3.1): a text blob's content
                    // rule is a wire-alone condition
                    if (id == StringTypeId) { if (!TextValid(rec)) { Damage(); return; } }
                    else if (id != BytesTypeId) { if (!Body(rec, false)) { return; } }
                    Starts[k + 2] = EdgeCount;
                }
            }
            Nodes = records + 1;
            if (EdgeCount > 0 || records > 0) { Graph(); }
        }
        // Graph judges the pointer graph: every pointer names the root or a
        // record, a walk from the root that meets a node whose descent is still
        // open is a cycle, a record the walk never enters is unreachable, and a
        // blob record named from two slots is blob_shared (§6.5, THE GRAPH,
        // DEFINED ONCE).
        void Graph()
        {
            int n = Nodes;
            int[] names = System.Buffers.ArrayPool<int>.Shared.Rent(n * 3);
            try
            {
                Span<int> name = names.AsSpan(0, n), mark = names.AsSpan(n, n), next = names.AsSpan(2 * n, n);
                name.Clear(); mark.Clear(); next.Clear();
                for (int e = 0; e < EdgeCount; e++)
                {
                    if (Edges[e] - 1 >= (ulong)n) { Damage(); return; }
                    name[(int)(Edges[e] - 1)]++;
                }
                if (n > 1)
                {
                    ReadOnlySpan<byte> t = Table;
                    int off = 0; Leb(t, ref off, out _);
                    for (int k = 1; k < n; k++)
                    {
                        Leb(t, ref off, out ulong reference); Leb(t, ref off, out ulong length); off += (int)length;
                        ulong id = Read64(Ids, (int)(reference - 1) * 8);
                        if ((id == StringTypeId || id == BytesTypeId) && name[k] > 1) { if (Fail == WireWhole) { Fail = WireBlobShared; } return; }
                    }
                }
                // mark: 0 unvisited, 1 open, 2 closed; next[k] counts the edges
                // of node k already followed; path is the stack of open nodes,
                // each entered once, so it never holds more than n.
                int[] rented = System.Buffers.ArrayPool<int>.Shared.Rent(n);
                try
                {
                    Span<int> path = rented.AsSpan(0, n);
                    int top = 0; path[0] = 0; mark[0] = 1;
                    while (top >= 0)
                    {
                        int node = path[top];
                        int from = Starts == null ? 0 : Starts[node];
                        int to = Starts == null ? EdgeCount : Starts[node + 1];
                        if (from + next[node] == to) { mark[node] = 2; top--; continue; }
                        int target = (int)(Edges[from + next[node]] - 1); next[node]++;
                        if (mark[target] == 1) { Damage(); return; }
                        if (mark[target] == 2) { continue; }
                        mark[target] = 1; path[++top] = target;
                    }
                }
                finally { System.Buffers.ArrayPool<int>.Shared.Return(rented); }
                for (int k = 1; k < n; k++) { if (mark[k] == 0) { Damage(); return; } }
            }
            finally { System.Buffers.ArrayPool<int>.Shared.Return(names); }
        }
        // Body walks one table body that must end with its terminator as its
        // LAST byte. The root admits the node table's reserved id, whose kind 12
        // payload is kept for Root instead of being read as text.
        bool Body(ReadOnlySpan<byte> buf, bool root)
        {
            Span<ulong> seen = stackalloc ulong[Words];
            int off = 0;
            while (true)
            {
                if (!Leb(buf, ref off, out ulong reference)) { return Damage(); }
                if (reference == 0) { return off == buf.Length || Damage(); }
                if (reference > (ulong)(Ids.Length / 8)) { return Damage(); }
                int r = (int)reference;
                if ((seen[r >> 6] & (1ul << (r & 63))) != 0) { return Damage(); }
                seen[r >> 6] |= 1ul << (r & 63);
                if (off >= buf.Length) { return Damage(); }
                byte kind = buf[off++];
                ulong id = Read64(Ids, (r - 1) * 8);
                if (id == ulong.MaxValue - 1 || id == ulong.MaxValue - 2)
                {
                    if (!root || !Announcement) { return Damage(); }
                }
                else if (id == ulong.MaxValue)
                {
                    if (!root || kind != 12) { return Damage(); }
                    if (!Leb(buf, ref off, out ulong n) || n > (ulong)(buf.Length - off)) { return Damage(); }
                    Table = buf.Slice(off, (int)n); Present = true; off += (int)n;
                    continue;
                }
                if (!Payload(buf, ref off, kind)) { return false; }
            }
        }
        // Payload walks one SELF-FRAMED payload of kind at off: what follows a
        // field's kind byte, and an array element.
        bool Payload(ReadOnlySpan<byte> buf, ref int off, byte kind)
        {
            int w = Width(kind);
            if (w != 0)
            {
                if (w > buf.Length - off) { return Damage(); }
                off += w;
                return Scalar(buf.Slice(off - w, w), kind);
            }
            switch (kind)
            {
                case 17:
                    if (!Leb(buf, ref off, out ulong index)) { return Damage(); }
                    Edge(index); return true;
                case 30:
                    if (!Leb(buf, ref off, out ulong variant) || variant > (ulong)(Ids.Length / 8)) { return Damage(); }
                    return true;
                case 15: return Union(buf, ref off);
                case 12: case 31: case 32: case 13: case 14: case 16:
                    if (!Leb(buf, ref off, out ulong n) || n > (ulong)(buf.Length - off)) { return Damage(); }
                    off += (int)n;
                    return Content(buf.Slice(off - (int)n, (int)n), kind);
            }
            return Damage();
        }
        // Content walks a payload already framed by a length and requires it to
        // be consumed exactly: a field's L, an arm's L, a keyed slot's L.
        bool Content(ReadOnlySpan<byte> b, byte kind)
        {
            int w = Width(kind);
            if (w != 0) { return b.Length == w ? Scalar(b, kind) : Damage(); }
            switch (kind)
            {
                case 12: return TextValid(b) || Damage();
                case 31: return true;
                case 32: return b.Length == 0 || Damage();
                case 13: case 14: case 16:
                {
                    // THE BINARY NESTING BOUND (docs/SPEC-TABLES.md §4)
                    if (Depth >= 128) { return Damage(); }
                    Depth++;
                    bool good = kind == 13 ? Body(b, false) : kind == 14 ? ArrayBody(b) : KeyedBody(b);
                    Depth--;
                    return good;
                }
                case 17: case 30: case 15:
                {
                    int end = 0;
                    if (!Payload(b, ref end, kind)) { return false; }
                    return end == b.Length || Damage();
                }
            }
            return Damage();
        }
        // Scalar checks a fixed-width value: a bool is 0 or 1, and a float is
        // finite (§3.3); the walk goes on past a non-finite float.
        bool Scalar(ReadOnlySpan<byte> b, byte kind)
        {
            if (kind == 1 && b[0] > 1) { return Damage(); }
            if (kind == 10 || kind == 11)
            {
                ulong raw = kind == 10 ? System.Buffers.Binary.BinaryPrimitives.ReadUInt32LittleEndian(b) : System.Buffers.Binary.BinaryPrimitives.ReadUInt64LittleEndian(b);
                if (NonFiniteBits(kind, raw)) { NonFinite = true; }
            }
            return true;
        }
        // Header reads an array, keyed or map content's element kind and count.
        // THE HEADER LIES INSIDE ITS OWN L (§4); a count past int.MaxValue is
        // the storage cap.
        bool Header(ReadOnlySpan<byte> b, out byte kind, out ulong count, out int off)
        {
            kind = 0; count = 0; off = 1;
            if (b.Length < 2) { return Damage(); }
            kind = b[0];
            if (!Leb(b, ref off, out count)) { return Damage(); }
            if (count > int.MaxValue) { if (Fail == WireWhole) { Fail = WireCountCap; } return false; }
            return true;
        }
        bool ArrayBody(ReadOnlySpan<byte> b)
        {
            if (!Header(b, out byte kind, out ulong count, out int off)) { return false; }
            for (ulong i = 0; i < count; i++) { if (!Payload(b, ref off, kind)) { return false; } }
            return off == b.Length || Damage();
        }
        bool KeyedBody(ReadOnlySpan<byte> b)
        {
            if (!Header(b, out byte kind, out ulong count, out int off)) { return false; }
            Span<ulong> seen = stackalloc ulong[Words];
            for (ulong i = 0; i < count; i++)
            {
                if (!Leb(b, ref off, out ulong key) || key == 0 || key > (ulong)(Ids.Length / 8)) { return Damage(); }
                int k = (int)key;
                if ((seen[k >> 6] & (1ul << (k & 63))) != 0) { return Damage(); }
                seen[k >> 6] |= 1ul << (k & 63);
                if (!Leb(b, ref off, out ulong n) || n > (ulong)(b.Length - off)) { return Damage(); }
                off += (int)n;
                if (!Content(b.Slice(off - (int)n, (int)n), kind)) { return false; }
            }
            return off == b.Length || Damage();
        }
        // Union walks a union payload in place: the arm reference, and for a set
        // arm its kind byte, its L and the content. A STRUCTURAL ARM IS A LEVEL
        // (docs/SPEC-TABLES.md §4).
        bool Union(ReadOnlySpan<byte> buf, ref int off)
        {
            if (!Leb(buf, ref off, out ulong arm)) { return Damage(); }
            if (arm == 0) { return true; }
            if (arm > (ulong)(Ids.Length / 8) || off >= buf.Length) { return Damage(); }
            byte kind = buf[off++];
            if (!Leb(buf, ref off, out ulong n) || n > (ulong)(buf.Length - off)) { return Damage(); }
            off += (int)n;
            ReadOnlySpan<byte> content = buf.Slice(off - (int)n, (int)n);
            bool structural = kind == 13 || kind == 14 || kind == 16 || kind == 15;
            if (!structural) { return Content(content, kind); }
            if (Depth >= 128) { return Damage(); }
            Depth++;
            bool good = Content(content, kind);
            Depth--;
            return good;
        }
    }
`
