// THE C# PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md).
//
// One raw input per manifest line, the port's generated reader underneath, and
// the Go engine's committed verdict as the oracle. The corpus is read as files:
// no network, no clock read by any reader. The runner prints one PASS line per
// case it agrees with, one FIX-CARD line per carried disagreement, one SKIP
// line per case whose carrier the port has no reader for, and fails on a
// disagreement no FIX-CARD names (or a FIX-CARD whose case has gone green).
//
// usage: cshostile <corpus-dir> <known-findings.tsv>
//
// A stack overflow cannot be caught in .NET and ends the process, so every
// case runs in a pooled worker process (cshostile --worker): the parent hands
// each worker one case at a time, a worker that dies or overruns the case
// budget is a FAIL for that case and is replaced, and the rest still run. In
// the worker each case runs on its own thread with a 512 KiB stack. The budget
// is 5 s per case (HOSTILE_CASE_BUDGET_MS overrides); HOSTILE_WORKERS sets the
// pool size.
//
// The per-carrier calls live in the generated Dispatch.cs (test/cs-hostile/gen):
//   wire              <T>Load(value, bytes, report), and for a variable root
//                     the native region <T>Load(region, capacity, bytes,
//                     report) too; both must answer the engine's verdict
//   wire_retain       the native retaining <T>LoadRetain (variable roots)
//   text              <T>FromJson
//   message           Schema.AnnounceRead of the unit's own announcement, then
//                     <T>LoadMessages into one value
//   message_novocab   <T>LoadMessages with no vocabulary
//   announce*         Schema.AnnounceRead on a fresh vocabulary (MaxBytes 8,
//                     MaxEntries 1, or a second read, per carrier)
//   packet            Read<T> over the raw bytes, Write<T> of the decoded value
// After an accepted read the value's Measure, Save and ToJson run; the digest
// is SHA-256 of the ToJson text with the port's trailing newline removed (the
// engine's canonical text, internal/tabletext.Model.Write, carries none). For a
// packet the digest is SHA-256 of the bytes the port's own writer emits.

using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Security.Cryptography;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

static class H
{
    internal static string Accept(string digest) { return "accept " + digest; }
    internal static string Refuse(string reason) { return "refuse " + reason; }

    // A read's verdict off its report: a named refusal is its reason, a
    // malformed or failed read (no name) is the corpus's `damaged`, and a clean
    // read answers null so the caller goes on to digest the value. The port
    // names a refusal and may follow the name with detail (non_finite_float:
    // ShipEntry.health); the name is the reason the corpus compares.
    internal static string Read(bool ok, bool refused, string reason, bool malformed)
    {
        if (refused)
        {
            if (string.IsNullOrEmpty(reason)) { return Refuse("damaged"); }
            int detail = reason.IndexOf(": ", StringComparison.Ordinal);
            return Refuse(detail > 0 ? reason.Substring(0, detail) : reason);
        }
        if (!ok || malformed) { return Refuse("damaged"); }
        return null;
    }

    internal static string Sha(ReadOnlySpan<byte> bytes)
    {
        return Convert.ToHexString(SHA256.HashData(bytes)).ToLowerInvariant();
    }

    internal static string Digest(byte[] text)
    {
        int n = text.Length;
        while (n > 0 && text[n - 1] == '\n') { n--; }
        return Sha(text.AsSpan(0, n));
    }

    // A region the load measure asks for that dwarfs its input is reported
    // rather than allocated: a gigabyte for a corpus input is the finding.
    internal static string Oversized(long total, int inputBytes)
    {
        const long limit = 1L << 30;
        if (total > limit) { return "error: the load measure asks " + total + " region bytes for " + inputBytes + " input bytes"; }
        return null;
    }

    // Two reads of one input must agree; when they do not, both are named.
    internal static string Both(string a, string got, string b, string other)
    {
        return got == other ? got : a + ": " + got + "; " + b + ": " + other;
    }
}

static class Runner
{
    const int StackBytes = 512 * 1024;

    static int Main(string[] args)
    {
        if (args.Length == 2 && args[0] == "--worker") { return Worker(args[1]); }
        if (args.Length != 2)
        {
            Console.Error.WriteLine("usage: cshostile <corpus-dir> <known-findings.tsv>");
            return 2;
        }
        return Parent(args[0], args[1]);
    }

    // ---- the worker: one case per stdin line, one verdict per stdout line ----

    static int Worker(string corpus)
    {
        var stdout = Console.Out;
        stdout.WriteLine("ready");
        stdout.Flush();
        string line;
        while ((line = Console.In.ReadLine()) != null)
        {
            string[] f = line.Split('\t');
            string got = RunOne(corpus, f[0], f[1], f[2], f[3]);
            stdout.WriteLine(got.Replace('\n', ' ').Replace('\r', ' '));
            stdout.Flush();
        }
        return 0;
    }

    static string RunOne(string corpus, string carrier, string schema, string table, string input)
    {
        byte[] payload;
        try { payload = File.ReadAllBytes(Path.Combine(corpus, input)); }
        catch (Exception e) { return "runner error: " + e.Message; }
        string got = null;
        var thread = new Thread(() =>
        {
            try { got = Dispatch.Run(carrier, schema, table, payload); }
            catch (Exception e) { got = "raised " + e.GetType().FullName + ": " + e.Message; }
        }, StackBytes);
        thread.Start();
        thread.Join();
        return got ?? "error: the case answered nothing";
    }

    // ---- the parent: the pool, the budget, the comparison ----

    sealed class Case
    {
        public int Index;
        public string Name, Carrier, Schema, Table, Input, Verdict, Reason, Digest;
        public string Got;
    }

    sealed class WorkerProcess
    {
        Process process;
        readonly string corpus;
        readonly Queue<string> stderr = new Queue<string>();

        public WorkerProcess(string corpus) { this.corpus = corpus; }

        void Start()
        {
            var info = new ProcessStartInfo();
            string self = Environment.ProcessPath;
            if (Path.GetFileNameWithoutExtension(self) == "dotnet")
            {
                info.FileName = self;
                info.ArgumentList.Add(typeof(Runner).Assembly.Location);
            }
            else
            {
                info.FileName = self;
            }
            info.ArgumentList.Add("--worker");
            info.ArgumentList.Add(corpus);
            info.RedirectStandardInput = true;
            info.RedirectStandardOutput = true;
            info.RedirectStandardError = true;
            info.UseShellExecute = false;
            lock (stderr) { stderr.Clear(); }
            process = Process.Start(info);
            process.ErrorDataReceived += (_, e) =>
            {
                if (e.Data == null) { return; }
                lock (stderr) { if (stderr.Count < 3) { stderr.Enqueue(e.Data); } }
            };
            process.BeginErrorReadLine();
            string ready = process.StandardOutput.ReadLine();
            if (ready != "ready") { throw new InvalidOperationException("a worker did not start: " + ready); }
        }

        void Stop()
        {
            if (process == null) { return; }
            try { if (!process.HasExited) { process.Kill(true); } process.WaitForExit(); }
            catch (Exception) { }
            process.Dispose();
            process = null;
        }

        string Tail()
        {
            lock (stderr) { return stderr.Count == 0 ? "" : " (stderr: " + string.Join(" | ", stderr) + ")"; }
        }

        public string Run(Case c, int budgetMs)
        {
            if (process == null) { Start(); }
            process.StandardInput.WriteLine(c.Carrier + "\t" + c.Schema + "\t" + c.Table + "\t" + c.Input);
            process.StandardInput.Flush();
            Task<string> reply = process.StandardOutput.ReadLineAsync();
            if (!reply.Wait(budgetMs))
            {
                Stop();
                return "over budget: no verdict within " + budgetMs + " ms";
            }
            string got = reply.Result;
            if (got == null)
            {
                process.WaitForExit();
                Thread.Sleep(50);
                string why = "crashed: the worker exited with code " + process.ExitCode + Tail();
                Stop();
                return why;
            }
            return got;
        }

        public void Close()
        {
            if (process == null) { return; }
            try { process.StandardInput.Close(); process.WaitForExit(2000); }
            catch (Exception) { }
            Stop();
        }
    }

    static List<string[]> Rows(string path, int fields)
    {
        var rows = new List<string[]>();
        foreach (string line in File.ReadAllLines(path, Encoding.UTF8))
        {
            if (line.Length == 0 || line.StartsWith('#')) { continue; }
            string[] f = line.Split('\t', fields);
            if (f.Length != fields) { throw new InvalidDataException(path + ": line has " + f.Length + " fields, want " + fields + ": " + line); }
            rows.Add(f);
        }
        return rows;
    }

    static int EnvInt(string name, int fallback)
    {
        string v = Environment.GetEnvironmentVariable(name);
        return int.TryParse(v, out int n) && n > 0 ? n : fallback;
    }

    static int Parent(string corpus, string findingsPath)
    {
        var findings = new Dictionary<string, string[]>();
        if (File.Exists(findingsPath))
        {
            foreach (string[] f in Rows(findingsPath, 4)) { findings[f[0]] = f; }
        }
        var cases = new List<Case>();
        foreach (string[] f in Rows(Path.Combine(corpus, "manifest.tsv"), 9))
        {
            cases.Add(new Case
            {
                Index = cases.Count, Name = f[0], Carrier = f[1], Schema = f[2], Table = f[3], Input = f[4],
                Verdict = f[6], Reason = f[7], Digest = f[8],
            });
        }

        int budget = EnvInt("HOSTILE_CASE_BUDGET_MS", 5000);
        int workers = Math.Min(EnvInt("HOSTILE_WORKERS", Math.Max(1, Math.Min(4, Environment.ProcessorCount / 2))), Math.Max(1, cases.Count));
        var queue = new ConcurrentQueue<Case>(cases);
        var threads = new List<Thread>();
        for (int i = 0; i < workers; i++)
        {
            var t = new Thread(() =>
            {
                var w = new WorkerProcess(corpus);
                while (queue.TryDequeue(out Case c))
                {
                    try { c.Got = w.Run(c, budget); }
                    catch (Exception e) { c.Got = "runner error: " + e.Message; }
                }
                w.Close();
            });
            t.Start();
            threads.Add(t);
        }
        foreach (Thread t in threads) { t.Join(); }

        int passed = 0, carried = 0, skipped = 0;
        var bad = new List<string>();
        var stale = new List<string>();
        foreach (Case c in cases)
        {
            // A packet refusal compares by verdict only: ports name packet
            // refusals differently, so the runner names them all packet_refused.
            string want = c.Verdict == "accept" ? H.Accept(c.Digest) : H.Refuse(c.Carrier == "packet" ? "packet_refused" : c.Reason);
            findings.TryGetValue(c.Name, out string[] finding);
            if (c.Got.StartsWith("skip: ", StringComparison.Ordinal))
            {
                if (finding != null)
                {
                    Console.WriteLine("STALE " + c.Name + ": FIX-CARD " + finding[1] + " names a case this port skips");
                    stale.Add(c.Name);
                }
                else
                {
                    Console.WriteLine("SKIP " + c.Name + " carrier=" + c.Carrier + ": " + c.Got.Substring(6));
                    skipped++;
                }
            }
            else if (c.Got == want)
            {
                if (finding != null)
                {
                    Console.WriteLine("STALE " + c.Name + ": FIX-CARD " + finding[1] + " no longer reproduces");
                    stale.Add(c.Name);
                }
                else
                {
                    Console.WriteLine("PASS " + c.Name);
                    passed++;
                }
            }
            else if (finding != null)
            {
                Console.WriteLine("FIX-CARD: " + finding[1] + ": " + finding[2] + " " + finding[3]);
                carried++;
            }
            else
            {
                Console.WriteLine("FAIL " + c.Name + " carrier=" + c.Carrier + " got=" + c.Got + " want=" + want);
                bad.Add(c.Name);
            }
        }
        foreach (string name in findings.Keys)
        {
            if (!cases.Exists(c => c.Name == name))
            {
                Console.WriteLine("STALE " + name + ": FIX-CARD names a case the manifest no longer carries");
                stale.Add(name);
            }
        }
        Console.WriteLine(cases.Count + " cases: " + passed + " pass, " + carried + " carried as FIX-CARD, " + skipped + " skipped, "
            + bad.Count + " unexplained, " + stale.Count + " stale");
        return bad.Count == 0 && stale.Count == 0 ? 0 : 1;
    }
}
