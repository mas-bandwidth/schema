using Serialize;

namespace Packettext
{
    public static partial class Schema
    {
        // The content read, without the one-packet end check. This harness
        // agrees with the C++ oracle on a buffer that may hold bytes past the
        // value. The public Read checks the end.
        public static bool ReadNarrowContent(ReadStream stream, ref Narrow value)
        {
            return ReadNarrowBody(stream, ref value);
        }
    }
}
