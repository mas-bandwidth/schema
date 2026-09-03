// The file, in a buffer of EXACTLY its bytes, with a GUARD PAGE on the far
// side (docs/SPEC-TABLES.md §7.5).
//
// `Open` runs on whatever bytes a disk hands back — corrupt, truncated, or from
// a build that moved — and what the battery and the fuzzer hold is that
// refusing is CLEAN: no crash, no read past the `length` the caller passed. The
// C++ leg proves that with ASan, whose redzone sits on the very next byte. C#
// has no sanitizer, so the instrument here is an mmap'd region with a PROT_NONE
// page immediately after the buffer: a read past the end faults, and the site
// line this harness flushes before every attempt is what names the forgery.
//
// THE SLACK IS STATED RATHER THAN HIDDEN. A guard page is page-granular, so the
// buffer's last byte can only be placed against it by choosing where inside the
// mapping it starts — which also fixes the base's ALIGNMENT, and the two
// demands are the same knob. This places the base at the file's own region
// ALIGNMENT plus the caller's `lead`, and takes the slack that leaves: at most
// `alignment - 1` bytes, and EXACTLY ZERO for a valid cook, whose total is
// header + a data part rounded to `alignment` + a whole number of sixteen-byte
// directory entries. The residual cases are the truncations, which refuse on
// the part-length equation before any byte past the header is looked at.

using System;
using System.Runtime.InteropServices;

static unsafe class Native
{
    const int ProtNone = 0;
    const int ProtRead = 1;
    const int ProtWrite = 2;
    const int MapPrivate = 2;

    // MAP_ANONYMOUS is 0x20 on Linux and 0x1000 on Darwin — one of the few
    // places the two disagree on a constant this harness needs.
    static readonly int MapAnon = OperatingSystem.IsMacOS() ? 0x1000 : 0x20;

    [DllImport("libc", SetLastError = true, EntryPoint = "mmap")]
    static extern IntPtr Mmap(IntPtr addr, nuint length, int prot, int flags, int fd, long offset);

    [DllImport("libc", SetLastError = true, EntryPoint = "munmap")]
    static extern int Munmap(IntPtr addr, nuint length);

    [DllImport("libc", SetLastError = true, EntryPoint = "mprotect")]
    static extern int Mprotect(IntPtr addr, nuint length, int prot);

    // One placed file: the mapping, the base Open is handed, and the length the
    // caller claims.
    public struct File
    {
        public IntPtr Mapping;
        public nuint MappingBytes;
        public byte* Base;
        public long Length;

        public void Destroy()
        {
            if (Mapping != IntPtr.Zero)
            {
                Munmap(Mapping, MappingBytes);
                Mapping = IntPtr.Zero;
                Base = null;
                Length = 0;
            }
        }
    }

    // A copy of `source`, `claim` bytes long, at a base `lead` bytes past an
    // `alignment`-aligned address, with an inaccessible page immediately after
    // the mapping's writable extent.
    public static File Place(byte[] source, long claim, int lead, long alignment)
    {
        if (alignment < 1)
        {
            alignment = 1;
        }
        int page = Environment.SystemPageSize;
        long usable = (claim + lead + page - 1) / page * page;
        if (usable == 0)
        {
            usable = page;
        }
        // The mapping is page-aligned, so its own address is 0 modulo every
        // alignment this format has. Pick the LARGEST offset whose residue is
        // the caller's `lead` — that is the base a 64-byte-aligned allocation
        // plus `lead` would have had, and it puts the buffer's end within
        // `alignment - 1` bytes of the guard.
        long offMax = usable - claim;
        long off = offMax - ((offMax - lead) % alignment + alignment) % alignment;
        if (off < 0)
        {
            off = lead % alignment;
        }
        nuint total = (nuint)(usable + page);
        IntPtr mapping = Mmap(IntPtr.Zero, total, ProtRead | ProtWrite, MapPrivate | MapAnon, -1, 0);
        if (mapping == IntPtr.Zero || mapping == new IntPtr(-1))
        {
            throw new InvalidOperationException("mmap failed for a " + claim + "-byte cook");
        }
        if (Mprotect(mapping + (int)usable, (nuint)page, ProtNone) != 0)
        {
            throw new InvalidOperationException("mprotect failed on the guard page");
        }
        File file;
        file.Mapping = mapping;
        file.MappingBytes = total;
        file.Base = (byte*)mapping + off;
        file.Length = claim;
        long copy = claim < source.Length ? claim : source.Length;
        for (long i = 0; i < copy; i++)
        {
            file.Base[i] = source[i];
        }
        for (long i = copy; i < claim; i++)
        {
            file.Base[i] = 0;
        }
        return file;
    }
}
