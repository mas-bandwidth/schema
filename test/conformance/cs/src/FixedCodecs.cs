using System;

static partial class Program
{
    // THE FIXED ROOTS (docs/SPEC-TABLES.md §3.4, §6.5): a root declared
    // `fixed table` is a form-3 file. Its one read is the generated
    // <Root>FixedLoad (a form-1 file is refused as previous_form) and its
    // write is <Root>FixedSave. Rebinding Load/Measure/Save here keeps the
    // variable read only as VariableLoad, which the framing surface alone
    // uses (its vectors are form-1 bytes at a fixed root, written back in
    // the root's own form).
    const int FixedPlanCapacity = 1 << 14;

    static void BindFixed<TValue, TReport, TEntry>(
        string unit, string root,
        Func<TReport> make, Func<TReport, Report> copy,
        Func<TValue, ReadOnlySpan<byte>, Span<TEntry>, TReport, long> load,
        Func<long, long> measure,
        Func<TValue, Span<byte>, long> save)
        where TValue : new()
    {
        Codec c = Find(unit, root);
        if (c == null) { throw new InvalidOperationException("fixed root with no codec row: " + unit + " " + root); }
        c.VariableLoad = c.Load;
        c.Load = (bytes, report) =>
        {
            TValue value = new TValue();
            TReport inner = make();
            long n = load(value, bytes, new TEntry[FixedPlanCapacity], inner);
            Fill(report, copy(inner));
            FillReason(report, inner);
            return n == 1 ? (object)value : null;
        };
        c.Measure = v => measure(1);
        c.Save = (v, buffer) => save((TValue)v, buffer);
    }

    static void RegisterFixed()
    {
        BindFixed<Tabledemo.RootConfig, Tabledemo.TableReport, Tabledemo.TableFixedEntry>("tabledemo", "RootConfig", () => new Tabledemo.TableReport(), Copy, Tabledemo.Schema.RootConfigFixedLoad, Tabledemo.Schema.RootConfigFixedMeasure, Tabledemo.Schema.RootConfigFixedSave);
        BindFixed<Tabledemo.ProfileConfig, Tabledemo.TableReport, Tabledemo.TableFixedEntry>("tabledemo", "ProfileConfig", () => new Tabledemo.TableReport(), Copy, Tabledemo.Schema.ProfileConfigFixedLoad, Tabledemo.Schema.ProfileConfigFixedMeasure, Tabledemo.Schema.ProfileConfigFixedSave);
        BindFixed<Tabledemo.LoadoutConfig, Tabledemo.TableReport, Tabledemo.TableFixedEntry>("tabledemo", "LoadoutConfig", () => new Tabledemo.TableReport(), Copy, Tabledemo.Schema.LoadoutConfigFixedLoad, Tabledemo.Schema.LoadoutConfigFixedMeasure, Tabledemo.Schema.LoadoutConfigFixedSave);
        BindFixed<Tabledemo.ArchiveConfig, Tabledemo.TableReport, Tabledemo.TableFixedEntry>("tabledemo", "ArchiveConfig", () => new Tabledemo.TableReport(), Copy, Tabledemo.Schema.ArchiveConfigFixedLoad, Tabledemo.Schema.ArchiveConfigFixedMeasure, Tabledemo.Schema.ArchiveConfigFixedSave);
        BindFixed<Tabledemo.PackConfig, Tabledemo.TableReport, Tabledemo.TableFixedEntry>("tabledemo", "PackConfig", () => new Tabledemo.TableReport(), Copy, Tabledemo.Schema.PackConfigFixedLoad, Tabledemo.Schema.PackConfigFixedMeasure, Tabledemo.Schema.PackConfigFixedSave);
        BindFixed<Tabledemo.KeyedConfig, Tabledemo.TableReport, Tabledemo.TableFixedEntry>("tabledemo", "KeyedConfig", () => new Tabledemo.TableReport(), Copy, Tabledemo.Schema.KeyedConfigFixedLoad, Tabledemo.Schema.KeyedConfigFixedMeasure, Tabledemo.Schema.KeyedConfigFixedSave);
        BindFixed<Tblv1.Cfg, Tblv1.TableReport, Tblv1.TableFixedEntry>("tblv1", "Cfg", () => new Tblv1.TableReport(), Copy, Tblv1.Schema.CfgFixedLoad, Tblv1.Schema.CfgFixedMeasure, Tblv1.Schema.CfgFixedSave);
        BindFixed<Tblv2.Cfg, Tblv2.TableReport, Tblv2.TableFixedEntry>("tblv2", "Cfg", () => new Tblv2.TableReport(), Copy, Tblv2.Schema.CfgFixedLoad, Tblv2.Schema.CfgFixedMeasure, Tblv2.Schema.CfgFixedSave);
        BindFixed<Tblp1.Chain, Tblp1.TableReport, Tblp1.TableFixedEntry>("tblp1", "Chain", () => new Tblp1.TableReport(), Copy, Tblp1.Schema.ChainFixedLoad, Tblp1.Schema.ChainFixedMeasure, Tblp1.Schema.ChainFixedSave);
        BindFixed<Tblp3.Chain, Tblp3.TableReport, Tblp3.TableFixedEntry>("tblp3", "Chain", () => new Tblp3.TableReport(), Copy, Tblp3.Schema.ChainFixedLoad, Tblp3.Schema.ChainFixedMeasure, Tblp3.Schema.ChainFixedSave);
        BindFixed<Tblr1.Cfg, Tblr1.TableReport, Tblr1.TableFixedEntry>("tblr1", "Cfg", () => new Tblr1.TableReport(), Copy, Tblr1.Schema.CfgFixedLoad, Tblr1.Schema.CfgFixedMeasure, Tblr1.Schema.CfgFixedSave);
        BindFixed<Tblr2.Cfg, Tblr2.TableReport, Tblr2.TableFixedEntry>("tblr2", "Cfg", () => new Tblr2.TableReport(), Copy, Tblr2.Schema.CfgFixedLoad, Tblr2.Schema.CfgFixedMeasure, Tblr2.Schema.CfgFixedSave);
        BindFixed<Tblm1.Msg, Tblm1.TableReport, Tblm1.TableFixedEntry>("tblm1", "Msg", () => new Tblm1.TableReport(), Copy, Tblm1.Schema.MsgFixedLoad, Tblm1.Schema.MsgFixedMeasure, Tblm1.Schema.MsgFixedSave);
        BindFixed<Tblm2.Msg, Tblm2.TableReport, Tblm2.TableFixedEntry>("tblm2", "Msg", () => new Tblm2.TableReport(), Copy, Tblm2.Schema.MsgFixedLoad, Tblm2.Schema.MsgFixedMeasure, Tblm2.Schema.MsgFixedSave);
        BindFixed<Tbla1.Root, Tbla1.TableReport, Tbla1.TableFixedEntry>("tbla1", "Root", () => new Tbla1.TableReport(), Copy, Tbla1.Schema.RootFixedLoad, Tbla1.Schema.RootFixedMeasure, Tbla1.Schema.RootFixedSave);
        BindFixed<Tbla2.Root, Tbla2.TableReport, Tbla2.TableFixedEntry>("tbla2", "Root", () => new Tbla2.TableReport(), Copy, Tbla2.Schema.RootFixedLoad, Tbla2.Schema.RootFixedMeasure, Tbla2.Schema.RootFixedSave);
        BindFixed<Tblk1.Root, Tblk1.TableReport, Tblk1.TableFixedEntry>("tblk1", "Root", () => new Tblk1.TableReport(), Copy, Tblk1.Schema.RootFixedLoad, Tblk1.Schema.RootFixedMeasure, Tblk1.Schema.RootFixedSave);
        BindFixed<Tblk2.Root, Tblk2.TableReport, Tblk2.TableFixedEntry>("tblk2", "Root", () => new Tblk2.TableReport(), Copy, Tblk2.Schema.RootFixedLoad, Tblk2.Schema.RootFixedMeasure, Tblk2.Schema.RootFixedSave);
        BindFixed<Listdemo.Bounded, Listdemo.TableReport, Listdemo.TableFixedEntry>("listdemo", "Bounded", () => new Listdemo.TableReport(), Copy, Listdemo.Schema.BoundedFixedLoad, Listdemo.Schema.BoundedFixedMeasure, Listdemo.Schema.BoundedFixedSave);
        BindFixed<Scalardemo.SimState, Scalardemo.TableReport, Scalardemo.TableFixedEntry>("scalars", "SimState", () => new Scalardemo.TableReport(), Copy, Scalardemo.Schema.SimStateFixedLoad, Scalardemo.Schema.SimStateFixedMeasure, Scalardemo.Schema.SimStateFixedSave);
        BindFixed<Scalardemo2.SimState, Scalardemo2.TableReport, Scalardemo2.TableFixedEntry>("tblscalars2", "SimState", () => new Scalardemo2.TableReport(), Copy, Scalardemo2.Schema.SimStateFixedLoad, Scalardemo2.Schema.SimStateFixedMeasure, Scalardemo2.Schema.SimStateFixedSave);
        BindFixed<Messagedemo.ToolMessage, Messagedemo.TableReport, Messagedemo.TableFixedEntry>("messagedemo", "ToolMessage", () => new Messagedemo.TableReport(), Copy, Messagedemo.Schema.ToolMessageFixedLoad, Messagedemo.Schema.ToolMessageFixedMeasure, Messagedemo.Schema.ToolMessageFixedSave);
        BindFixed<Backenddemo.LoginRequest, Backenddemo.TableReport, Backenddemo.TableFixedEntry>("backenddemo", "LoginRequest", () => new Backenddemo.TableReport(), Copy, Backenddemo.Schema.LoginRequestFixedLoad, Backenddemo.Schema.LoginRequestFixedMeasure, Backenddemo.Schema.LoginRequestFixedSave);
        BindFixed<Backenddemo.MatchResult, Backenddemo.TableReport, Backenddemo.TableFixedEntry>("backenddemo", "MatchResult", () => new Backenddemo.TableReport(), Copy, Backenddemo.Schema.MatchResultFixedLoad, Backenddemo.Schema.MatchResultFixedMeasure, Backenddemo.Schema.MatchResultFixedSave);
        BindFixed<Backenddemo.StorePurchase, Backenddemo.TableReport, Backenddemo.TableFixedEntry>("backenddemo", "StorePurchase", () => new Backenddemo.TableReport(), Copy, Backenddemo.Schema.StorePurchaseFixedLoad, Backenddemo.Schema.StorePurchaseFixedMeasure, Backenddemo.Schema.StorePurchaseFixedSave);
        BindFixed<Backenddemo.Envelope, Backenddemo.TableReport, Backenddemo.TableFixedEntry>("backenddemo", "Envelope", () => new Backenddemo.TableReport(), Copy, Backenddemo.Schema.EnvelopeFixedLoad, Backenddemo.Schema.EnvelopeFixedMeasure, Backenddemo.Schema.EnvelopeFixedSave);
        BindFixed<Vocabdemo.Wide00, Vocabdemo.TableReport, Vocabdemo.TableFixedEntry>("vocabdemo", "Wide00", () => new Vocabdemo.TableReport(), Copy, Vocabdemo.Schema.Wide00FixedLoad, Vocabdemo.Schema.Wide00FixedMeasure, Vocabdemo.Schema.Wide00FixedSave);
        BindFixed<Vocabdemo.Wide09, Vocabdemo.TableReport, Vocabdemo.TableFixedEntry>("vocabdemo", "Wide09", () => new Vocabdemo.TableReport(), Copy, Vocabdemo.Schema.Wide09FixedLoad, Vocabdemo.Schema.Wide09FixedMeasure, Vocabdemo.Schema.Wide09FixedSave);
        BindFixed<Vocab9demo.Wide00, Vocab9demo.TableReport, Vocab9demo.TableFixedEntry>("vocab9demo", "Wide00", () => new Vocab9demo.TableReport(), Copy, Vocab9demo.Schema.Wide00FixedLoad, Vocab9demo.Schema.Wide00FixedMeasure, Vocab9demo.Schema.Wide00FixedSave);
        BindFixed<Vocab9demo.Wide19, Vocab9demo.TableReport, Vocab9demo.TableFixedEntry>("vocab9demo", "Wide19", () => new Vocab9demo.TableReport(), Copy, Vocab9demo.Schema.Wide19FixedLoad, Vocab9demo.Schema.Wide19FixedMeasure, Vocab9demo.Schema.Wide19FixedSave);
        BindFixed<Wide.Caption, Wide.TableReport, Wide.TableFixedEntry>("widedemo", "Caption", () => new Wide.TableReport(), Copy, Wide.Schema.CaptionFixedLoad, Wide.Schema.CaptionFixedMeasure, Wide.Schema.CaptionFixedSave);
        BindFixed<Wide.Stamp, Wide.TableReport, Wide.TableFixedEntry>("widedemo", "Stamp", () => new Wide.TableReport(), Copy, Wide.Schema.StampFixedLoad, Wide.Schema.StampFixedMeasure, Wide.Schema.StampFixedSave);
        // WideBlob is declared fixed past the fixed-form ceiling, so it has
        // no file form at all: every file read of it refuses.
        {
            Codec c = Find("tabledemo", "WideBlob");
            c.VariableLoad = c.Load;
            c.Load = (bytes, report) => { report.Refused = true; return null; };
            c.Measure = v => -1;
            c.Save = (v, buffer) => -1;
        }
    }
}
