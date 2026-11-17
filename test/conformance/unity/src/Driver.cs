// The Unity leg's conformance driver frame (test/conformance/README.md, "The
// shape"; docs/UNITY.md). One .NET host process per surface run. Each manifest
// unit is compiled in the Unity dialect as its own schemaunit.dll
// (unit/schemaunit.csproj) under <units>/<unit>/, where <units> is
// UNITY_CONFORMANCE_UNITS (default build/conformance-unity), and the driver
// loads it and finds a root's generated Schema.<Root>Load, <Root>Measure and
// <Root>Save by name. Reflection is the HOST's: the generated code itself uses
// none (docs/UNITY.md).
//
// usage: unityconformance <manifest> list
//        unityconformance <manifest> <surface> <outdir>
//        unityconformance --verdicts <units> <list> <out>
//
// The table core answers the `wire` surface and every other surface ABSENT
// (exit 2). A case whose unit is not generated, or whose root the runtime does
// not carry yet (no <Root>Load: a field no codec carries), is <case>.absent.
// --verdicts is the oracle probe the Go tests drive: each line of <list> is
// `<unit> <root> <wire>`, and each line of <out> is `refused` or
// `read <unknown> <widened> <hex of the re-saved bytes>`.

using System;
using System.Collections.Generic;
using System.IO;
using System.Reflection;
using System.Text;

namespace SchemaUnityConformance
{
    internal delegate bool LoadFn<T, R>(ref T value, ReadOnlySpan<byte> bytes, ref R report);
    internal delegate int SaveFn<T>(ref T value, Span<byte> buffer);
    internal delegate int MeasureFn<T>(ref T value);

    internal sealed class Outcome
    {
        public bool Refused;
        public int Unknown;
        public int Widened;
        public int Reason;
        public byte[] Saved;
        public string Fault;
    }

    // CarriedRoot is one root's three generated entries, found by name.
    internal sealed class CarriedRoot
    {
        private MethodInfo load;
        private MethodInfo save;
        private MethodInfo measure;
        private Type valueType;
        private Type reportType;

        public static CarriedRoot Find(Assembly unit, string root)
        {
            foreach (Type t in unit.GetTypes())
            {
                if (t.Name != "Schema" || !t.IsAbstract || !t.IsSealed)
                {
                    continue;
                }
                MethodInfo load = t.GetMethod(root + "Load", BindingFlags.Public | BindingFlags.Static);
                MethodInfo save = t.GetMethod(root + "Save", BindingFlags.Public | BindingFlags.Static);
                MethodInfo measure = t.GetMethod(root + "Measure", BindingFlags.Public | BindingFlags.Static);
                if (load == null || save == null || measure == null)
                {
                    continue;
                }
                ParameterInfo[] p = load.GetParameters();
                return new CarriedRoot
                {
                    load = load,
                    save = save,
                    measure = measure,
                    valueType = p[0].ParameterType.GetElementType(),
                    reportType = p[2].ParameterType.GetElementType(),
                };
            }
            return null;
        }

        public Outcome Run(byte[] wire)
        {
            MethodInfo typed = typeof(CarriedRoot).GetMethod(nameof(RunTyped), BindingFlags.NonPublic | BindingFlags.Static)
                .MakeGenericMethod(valueType, reportType);
            return (Outcome)typed.Invoke(null, new object[] { load, save, measure, wire });
        }

        private static int Field<R>(R report, string name)
        {
            object v = typeof(R).GetField(name).GetValue(report);
            return v is bool b ? (b ? 1 : 0) : (int)v;
        }

        private static Outcome RunTyped<T, R>(MethodInfo load, MethodInfo save, MethodInfo measure, byte[] wire)
            where T : struct
            where R : struct
        {
            LoadFn<T, R> loadFn = (LoadFn<T, R>)load.CreateDelegate(typeof(LoadFn<T, R>));
            SaveFn<T> saveFn = (SaveFn<T>)save.CreateDelegate(typeof(SaveFn<T>));
            MeasureFn<T> measureFn = (MeasureFn<T>)measure.CreateDelegate(typeof(MeasureFn<T>));
            T value = default;
            R report = default;
            bool ok = loadFn(ref value, wire, ref report);
            Outcome o = new Outcome
            {
                Refused = Field(report, "Refused") != 0,
                Unknown = Field(report, "Unknown"),
                Widened = Field(report, "Widened"),
                Reason = Field(report, "Reason"),
            };
            if (ok == o.Refused)
            {
                o.Fault = "Load's answer and report.Refused disagree";
                return o;
            }
            if (!ok)
            {
                return o;
            }
            int size = measureFn(ref value);
            byte[] buffer = new byte[size + 16];
            int n = saveFn(ref value, buffer);
            if (n != size)
            {
                o.Fault = "Measure said " + size + " and Save wrote " + n;
                return o;
            }
            o.Saved = new byte[n];
            Array.Copy(buffer, o.Saved, n);
            return o;
        }
    }

    internal static class Driver
    {
        private static readonly Dictionary<string, Assembly> units = new Dictionary<string, Assembly>();

        private static Assembly Unit(string root, string key)
        {
            if (units.TryGetValue(key, out Assembly have))
            {
                return have;
            }
            string path = Path.GetFullPath(Path.Combine(root, key, "schemaunit.dll"));
            Assembly unit = File.Exists(path) ? Assembly.LoadFrom(path) : null;
            units[key] = unit;
            return unit;
        }

        private static List<string[]> Rows(string manifest, string kind)
        {
            List<string[]> rows = new List<string[]>();
            foreach (string raw in File.ReadAllLines(manifest))
            {
                string text = raw.Trim();
                if (text.Length == 0 || text.StartsWith("#", StringComparison.Ordinal))
                {
                    continue;
                }
                string[] fields = text.Split((char[])null, StringSplitOptions.RemoveEmptyEntries);
                if (fields[0] == kind)
                {
                    rows.Add(fields);
                }
            }
            return rows;
        }

        private static void Absent(string outdir, string name, string why)
        {
            File.WriteAllText(Path.Combine(outdir, name + ".absent"), why);
        }

        // Wire is the `wire` surface: Load the wire file, Save, the bytes.
        private static int Wire(string manifest, string outdir)
        {
            string root = Environment.GetEnvironmentVariable("UNITY_CONFORMANCE_UNITS");
            if (string.IsNullOrEmpty(root))
            {
                root = "build/conformance-unity";
            }
            Directory.CreateDirectory(outdir);
            int failed = 0;
            foreach (string[] row in Rows(manifest, "instance"))
            {
                string name = row[1], key = row[2], rootName = row[3], wire = row[4];
                Assembly unit = Unit(root, key);
                if (unit == null)
                {
                    Absent(outdir, name, "unit not generated for unity");
                    continue;
                }
                CarriedRoot carried = CarriedRoot.Find(unit, rootName);
                if (carried == null)
                {
                    Absent(outdir, name, "the unity table runtime does not carry this root yet");
                    continue;
                }
                Outcome o = carried.Run(File.ReadAllBytes(wire));
                if (o.Fault != null || o.Refused)
                {
                    Console.Error.WriteLine("FAILED: " + name + ": " + (o.Fault ?? "refused, reason " + o.Reason));
                    failed++;
                    continue;
                }
                File.WriteAllBytes(Path.Combine(outdir, name), o.Saved);
            }
            return failed == 0 ? 0 : 1;
        }

        // Verdicts is the oracle probe: one answer line per wire.
        private static int Verdicts(string root, string list, string output)
        {
            StringBuilder b = new StringBuilder();
            foreach (string raw in File.ReadAllLines(list))
            {
                string[] f = raw.Split(' ');
                if (f.Length != 3)
                {
                    continue;
                }
                Assembly unit = Unit(root, f[0]);
                CarriedRoot carried = unit == null ? null : CarriedRoot.Find(unit, f[1]);
                if (carried == null)
                {
                    b.Append("absent\n");
                    continue;
                }
                Outcome o = carried.Run(File.ReadAllBytes(f[2]));
                if (o.Fault != null)
                {
                    b.Append("fault ").Append(o.Fault.Replace(' ', '_')).Append('\n');
                }
                else if (o.Refused)
                {
                    b.Append("refused\n");
                }
                else
                {
                    b.Append("read ").Append(o.Unknown).Append(' ').Append(o.Widened).Append(' ')
                        .Append(Convert.ToHexString(o.Saved).ToLowerInvariant()).Append('\n');
                }
            }
            File.WriteAllText(output, b.ToString());
            return 0;
        }

        private static int Main(string[] args)
        {
            if (args.Length == 4 && args[0] == "--verdicts")
            {
                return Verdicts(args[1], args[2], args[3]);
            }
            if (args.Length == 2 && args[1] == "list")
            {
                Console.WriteLine("wire");
                return 0;
            }
            if (args.Length == 3)
            {
                if (args[1] == "wire")
                {
                    return Wire(args[0], args[2]);
                }
                return 2;
            }
            Console.Error.WriteLine("usage: unityconformance <manifest> list | <manifest> <surface> <outdir> | --verdicts <units> <list> <out>");
            return 2;
        }
    }
}
