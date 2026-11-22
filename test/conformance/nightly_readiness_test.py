import contextlib
import importlib.machinery
import importlib.util
import io
import json
import re
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
loader = importlib.machinery.SourceFileLoader("readiness", str(ROOT/"test/conformance/nightly-readiness"))
spec = importlib.util.spec_from_loader(loader.name, loader)
m = importlib.util.module_from_spec(spec)
loader.exec_module(m)


def native(package="gotable", name="TestReady", action="pass", output=""):
    pkg = "github.com/mas-bandwidth/schema/v3/" + m.package_path(package)
    return "\n".join(json.dumps(x) for x in [
        {"Package":pkg,"Action":"run","Test":name},
        {"Package":pkg,"Action":"output","Test":name,"Output":output},
        {"Package":pkg,"Action":action,"Test":name},
        {"Package":pkg,"Action":"pass"}])


def fuzz(arm="plain", seed=1, lang="", builder=False):
    prefix = "wire-fuzz" + ("-builder" if builder else "-mixed-builder" if lang in m.MIXED_BUILDER else "") + (" retain" if arm == "retain" else "")
    return f"{prefix}: seed {seed}, 41 seeds over 20 roots, 30 enumerated + 100000 random = 100030 mutants, 0 divergences, 1.0 s, 100030 mutants/s"


class FakeRunner(m.Runner):
    instances = []
    def __init__(self, root, lang):
        self.root = root
        self.temporary = tempfile.TemporaryDirectory()
        self.out = Path(self.temporary.name)
        self.results = []
        self.native_passes = set()
        self.calls = []
        self.lang = lang
        self.instances.append(self)
    def run(self, name, argv, cwd=None, validate=None, expected=0):
        self.calls.append((name,argv,cwd))
        text, status = "mock native success", 0
        if name == "readiness-contracts":
            text = "Ran 25 tests in 0.1s\nOK\n"
        elif name.startswith("native-list-"):
            text = "TestReady\n"
        elif name.startswith("native-"):
            text = native(name[len("native-"):])
        elif name == "implementation":
            text = f"IMPLEMENTATION_PREREQUISITES_PASS language={self.lang} constructs=11 witnesses=20"
        elif name.startswith("wire-plain-") or (name.startswith("wire-retain-") and name[-1:].isdigit()):
            _,arm,seed = name.split("-")
            text = fuzz(arm,int(seed),self.lang)
        elif name.startswith("wire-builder-"):
            text = fuzz("plain", int(name.rsplit("-", 1)[1]), self.lang, builder=True)
        elif self.lang in ("moo", "python", "ruby") and name in ("packet-normal-0", "packet-ludicrous-0"):
            tests = argv[argv.index("-run")+1][2:-2].split("|")
            text = "\n".join("\n".join(native(self.lang,t).splitlines()[:-1]) for t in tests)
            text += "\n" + native(self.lang).splitlines()[-1]
        elif name.startswith("packet-"):
            text = "OK"
            if name == "packet-schema_test_random":
                text = "OK (50000 iterations, seed 42)"
            elif self.lang == "dart" and name.endswith("-1"):
                text = "Generated: /tmp/mock-packet"
        elif name.endswith("-plant"):
            Path(argv[argv.index("-out")+1]).write_text("mock planted source\n")
        elif name.startswith("retain-goose-"):
            tests = argv[argv.index("-run")+1][2:-2].split("|")
            text = "\n".join("\n".join(native("goosetable",t).splitlines()[:-1]) for t in tests)
            text += "\n" + native("goosetable").splitlines()[-1]
        elif name.startswith("retain-moo-"):
            tests = argv[argv.index("-run")+1][2:-2].split("|")
            text = "\n".join("\n".join(native("mootable",t).splitlines()[:-1]) for t in tests)
            text += "\n" + native("mootable").splitlines()[-1]
        elif name.startswith("retain-python-"):
            tests = argv[argv.index("-run")+1][2:-2].split("|")
            text = "\n".join("\n".join(native("pythontable",t).splitlines()[:-1]) for t in tests)
            text += "\n" + native("pythontable").splitlines()[-1]
        elif name.startswith("retain-ruby-"):
            tests = argv[argv.index("-run")+1][2:-2].split("|")
            text = "\n".join("\n".join(native("rubytable",t).splitlines()[:-1]) for t in tests)
            text += "\n" + native("rubytable").splitlines()[-1]
        elif name.startswith("retain-"):
            test = argv[argv.index("-run")+1][1:-1]
            package = argv[-1].split("/")[-1]
            reason = {"retain-c":"report.retained==13",
                      "retain-go-file-count-floor":"count expanded past available resolved capacity",
                      "retain-go-unknown-node":"unplaceable node retention",
                      "retain-rust":"retain=true): the leg reports"}[name]
            text, status = native(package,test,"fail",reason), 1
        errors = [] if status == expected else [f"exit {status}, expected {expected}"]
        if validate:
            errors += validate(text)
        self.results.append({"phase":name,"exit":status,"errors":errors})
        return text,status


class Contracts(unittest.TestCase):
    def test_exact_cli(self):
        for args in ([],["go","--skip"],["lua"],["cpp","c"]):
            with contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(m.main(args),2)

    def test_implementation_positive(self):
        self.assertEqual(m.implementation_result(
            "IMPLEMENTATION_PREREQUISITES_PASS language=go constructs=11 witnesses=2","go"),[])

    def test_implementation_nonproof_results(self):
        good="IMPLEMENTATION_PREREQUISITES_PASS language=go constructs=11 witnesses=2"
        for text in ("", "PASS", good+"\n"+good, good.replace("go","dart"),
                     good.replace("witnesses=2","witnesses=0"),good.replace("constructs=11","constructs=0")):
            with self.subTest(text=text):
                self.assertTrue(m.implementation_result(text,"go"))

    def test_native_positive(self):
        self.assertEqual(m.native_result(native(),["TestReady"],"gotable"),[])

    def test_native_missing_wrong_and_failed(self):
        for text,listed in (("",[]),(native(),[]),(native(),["TestOther"]),
                            (native(package="ctable"),["TestReady"]),
                            (native(action="fail"),["TestReady"]),
                            (native(action="skip",output="owed"),["TestReady"]),
                            (native()+"\n"+native(),["TestReady"]),
                            ("not JSON",["TestReady"])):
            with self.subTest(text=text):
                self.assertTrue(m.native_result(text,listed,"gotable"))

    def test_native_package_success_required(self):
        text="\n".join(native().splitlines()[:-1])
        self.assertTrue(m.native_result(text,["TestReady"],"gotable"))

    def test_retired_exact_scope(self):
        name="TestFixedFormPlanPath"
        text="\n".join(native(name=name,action="skip",output=m.RETIRED[name][0]).splitlines()[:-1])+"\n"+native()
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(m.native_result(text,[name,"TestReady"],"gotable",set(m.RETIRED[name][1])),[])
            self.assertTrue(m.native_result(native(name=name,action="skip",output="owed"),[name,"TestReady"],"gotable"))
            self.assertTrue(m.native_result(text.replace("gotable","ctable"),[name,"TestReady"],"ctable"))

    def test_fuzz_positive_all_seeds_arms(self):
        for arm in ("plain","retain"):
            for seed in (1,2,3):
                self.assertEqual(m.fuzz_result(fuzz(arm,seed),arm,seed),[])

    def test_dart_mixed_builder_profile_is_required_and_not_accepted_for_other_languages(self):
        for arm in ("plain", "retain"):
            for seed in (1, 2, 3):
                builder = fuzz(arm, seed, "dart")
                self.assertEqual(m.fuzz_result(builder, arm, seed, "dart"), [])
                self.assertTrue(m.fuzz_result(fuzz(arm, seed), arm, seed, "dart"))
                self.assertTrue(m.fuzz_result(builder.replace("-mixed-builder", "-builder"), arm, seed, "dart"))
                for lang in m.LANGUAGES:
                    if lang not in m.MIXED_BUILDER:
                        self.assertTrue(m.fuzz_result(builder, arm, seed, lang))
                for text in ("", builder + "\n" + builder,
                             builder.replace("41 seeds", "0 seeds"),
                             builder.replace("20 roots", "0 roots"),
                             builder.replace("30 enumerated", "0 enumerated"),
                             builder.replace("100000 random", "0 random"),
                             builder.replace("100030 mutants", "100029 mutants"),
                             builder.replace("20 roots,", "20 roots, 1 seeds absent,"),
                             builder.replace("20 roots,", "20 roots, 1 non-finite mutants not compared,")):
                    self.assertTrue(m.fuzz_result(text, arm, seed, "dart"))

    def test_dart_mixed_profile_requires_full_plain_roster(self):
        for seed in (1, 2, 3):
            plain = fuzz("plain", seed, "dart").replace("20 roots,", "20 roots, 4 seeds outside the arm,")
            self.assertTrue(m.fuzz_result(plain, "plain", seed, "dart"))
            retain = fuzz("retain", seed, "dart").replace("20 roots,", "20 roots, 4 seeds outside the arm,")
            self.assertEqual(m.fuzz_result(retain, "retain", seed, "dart"), [])

    def test_javascript_mixed_profile_keeps_full_roster_and_exact_counts(self):
        for arm in ("plain", "retain"):
            for seed in (1, 2, 3):
                text = fuzz(arm, seed, "js")
                self.assertEqual(m.fuzz_result(text, arm, seed, "js"), [])
                for broken in (fuzz(arm, seed), text.replace("-mixed-builder", "-builder"),
                               text.replace("100000 random", "0 random"),
                               text.replace("20 roots", "0 roots")):
                    self.assertTrue(m.fuzz_result(broken, arm, seed, "js"))
                if arm == "plain":
                    self.assertTrue(m.fuzz_result(text.replace("20 roots,", "20 roots, 1 seeds outside the arm,"), arm, seed, "js"))

    def test_retaining_outside_domain_is_not_missing_codec(self):
        text=fuzz("retain").replace("20 roots,", "20 roots, 4 seeds outside the arm,")
        self.assertEqual(m.fuzz_result(text,"retain",1),[])

    def test_fuzz_absence_exemption_empty_and_accounting_rejected(self):
        good=fuzz()
        damaged = ("",good+"\n"+good,
            good.replace("20 roots,","20 roots, 1 seeds absent (roots the leg has no codec for),"),
            good.replace("20 roots,","20 roots, 0 non-finite mutants not compared (owed),"),
            good.replace("20 roots,","20 roots, 4 seeds outside the arm,"),
            good.replace("100000 random","0 random"),good.replace("30 enumerated","0 enumerated"),
            good.replace("100030 mutants","100029 mutants"),good.replace("41 seeds","0 seeds"),
            good.replace("20 roots","0 roots"),good.replace("0 divergences","1 divergences"),
            good.replace("seed 1","seed 2"))
        for text in damaged:
            with self.subTest(text=text):
                self.assertTrue(m.fuzz_result(text,"plain",1))

    def runner(self, lang):
        runner=m.Runner.__new__(m.Runner)
        runner.root=ROOT
        runner.temporary=tempfile.TemporaryDirectory()
        runner.out=Path(runner.temporary.name)
        runner.results=[]
        self.addCleanup(runner.temporary.cleanup)
        return runner

    def test_phase_contract_each_missing_and_duplicate(self):
        for lang in m.LANGUAGES:
            required=m.required_phases(lang)
            self.assertGreaterEqual(len(required),21)
            for missing in required:
                runner=self.runner(lang)
                runner.results=[{"phase":p,"errors":[]} for p in required if p!=missing]
                with contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(runner.finish(lang),1,(lang,missing))
            runner=self.runner(lang)
            runner.results=[{"phase":p,"errors":[]} for p in required]
            runner.results.append(runner.results[0].copy())
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(runner.finish(lang),1)

    def test_empty_results_and_failed_results_never_emit_marker(self):
        runner=self.runner("go")
        for results in ([],[{"phase":p,"errors":["failure"] if p=="implementation" else []}
                            for p in m.required_phases("go")]):
            runner.results=results
            output=io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(runner.finish("go"),1)
            self.assertNotIn("NIGHTLY_READINESS_PASS",output.getvalue())

    def test_complete_results_emit_exact_single_marker(self):
        for lang in m.LANGUAGES:
            runner=self.runner(lang)
            runner.results=[{"phase":p,"errors":[]} for p in m.required_phases(lang)]
            output=io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(runner.finish(lang),0)
            self.assertEqual(sum(line.startswith("NIGHTLY_READINESS_PASS language="+lang+" phases=")
                                 for line in output.getvalue().splitlines()),1)

    def test_full_mock_plan_every_language_and_timeout_commands(self):
        real=m.Runner
        m.Runner=FakeRunner
        try:
            for lang in m.LANGUAGES:
                output=io.StringIO()
                with contextlib.redirect_stdout(output):
                    self.assertEqual(m.main([lang]),0,output.getvalue())
                runner=FakeRunner.instances[-1]
                self.assertEqual({row["phase"] for row in runner.results},m.required_phases(lang))
                for package,path in {"tabletext":"internal/tabletext", "tablewire":"internal/tablewire",
                                     "tablepack":"internal/tablepack", "fixedtwin":"tools/fixedtwin"}.items():
                    calls=[argv for name,argv,cwd in runner.calls if name=="native-"+package]
                    self.assertEqual(len(calls),1 if lang=="cpp" else 0)
                    if calls:
                        self.assertIn("./"+path,calls[0])
                        self.assertNotIn("-run",calls[0])
                        self.assertIn("native-"+package,m.required_phases("cpp"))
                harness_calls=[argv for name,argv,cwd in runner.calls if name=="native-harness"]
                self.assertEqual(len(harness_calls),1 if lang=="cpp" else 0)
                if harness_calls:
                    self.assertIn("./test/conformance/harness",harness_calls[0])
                    self.assertNotIn("-run",harness_calls[0])
                    self.assertIn("native-harness",m.required_phases("cpp"))
                    prerequisites=next(argv for name,argv,cwd in runner.calls if name=="prerequisites")
                    self.assertIn("build/c-collections-fuzz",prerequisites)
                    self.assertIn("build/cpp-collections-fuzz",prerequisites)
                if lang == "dart":
                    prerequisite_index = next(i for i, call in enumerate(runner.calls) if call[0] == "prerequisites")
                    prerequisites = runner.calls[prerequisite_index][1]
                    self.assertIn("build/tables-generated-dart/.stamp", prerequisites)
                    self.assertIn("cards-dart-generate", prerequisites)
                    native_index = next(i for i, call in enumerate(runner.calls) if call[0] == "native-list-darttable")
                    self.assertLess(prerequisite_index, native_index)
                if lang == "elixir":
                    prerequisite_index = next(i for i, call in enumerate(runner.calls) if call[0] == "prerequisites")
                    prerequisites = runner.calls[prerequisite_index][1]
                    self.assertIn("build/elixir-tables-ebin/.stamp", prerequisites)
                    native_index = next(i for i, call in enumerate(runner.calls) if call[0] == "native-list-elixirtable")
                    self.assertLess(prerequisite_index, native_index)
                prerequisite_index = next(i for i, call in enumerate(runner.calls) if call[0] == "prerequisites")
                self.assertIn("build/fixedform-corpus/.stamp", runner.calls[prerequisite_index][1])
                self.assertLess(prerequisite_index, next(i for i, call in enumerate(runner.calls) if call[0].startswith("native-")))
                for name,argv,cwd in runner.calls:
                    if "test" in argv and ("go" in argv):
                        self.assertIn("-timeout=0",argv,(name,argv))
                    if name.startswith("wire-plain-") or (name.startswith("wire-retain-") and name[-1:].isdigit()):
                        self.assertNotIn("--finite-owed",argv)
                        self.assertEqual("--mixed-builder" in argv, lang in m.MIXED_BUILDER)
                        self.assertNotIn("--builder", argv)
                        self.assertEqual(argv[argv.index("--n")+1],"100000")
                runner.temporary.cleanup()
        finally:
            m.Runner=real

    def test_cs_profiles_are_distinct_and_both_mandatory(self):
        runner = FakeRunner(ROOT, "cs")
        try:
            m.run_fuzz(runner, "cs")
            region = [(name, argv) for name, argv, _ in runner.calls
                      if name.startswith(("wire-plain-", "wire-retain-"))]
            builder = [(name, argv) for name, argv, _ in runner.calls
                       if name.startswith("wire-builder-")]
            self.assertEqual(len(region), 6)
            self.assertEqual(len(builder), 3)
            for name, argv in region:
                self.assertTrue(argv[argv.index("--driver")+1].endswith(" wire-fuzz"))
                self.assertNotIn("--builder", argv)
                self.assertEqual("--retain" in argv, name.startswith("wire-retain-"))
            for name, argv in builder:
                self.assertIn("wire-fuzz-builder", argv[argv.index("--driver")+1])
                self.assertIn("--builder", argv)
                self.assertNotIn("--retain", argv)
                self.assertEqual(argv[argv.index("--n")+1], "100000")
                self.assertIn(name, m.required_phases("cs"))
            for seed in m.SEEDS:
                region_text = fuzz("plain", seed)
                builder_text = fuzz("plain", seed, builder=True)
                self.assertEqual(m.fuzz_result(builder_text, "plain", seed, "cs", builder=True), [])
                self.assertTrue(m.fuzz_result(region_text, "plain", seed, "cs", builder=True))
                self.assertTrue(m.fuzz_result(builder_text, "plain", seed, "cs"))
                outside = builder_text.replace("20 roots,", "20 roots, 120 seeds outside the arm,")
                self.assertEqual(m.fuzz_result(outside, "plain", seed, "cs", builder=True), [])
                self.assertTrue(m.fuzz_result(outside, "plain", seed, "cs"))
                for text in ("", builder_text+"\n"+builder_text,
                             builder_text.replace("100000 random", "0 random"),
                             builder_text.replace("20 roots,", "20 roots, 1 seeds absent,"),
                             builder_text.replace("20 roots,", "20 roots, 1 non-finite mutants not compared,")):
                    self.assertTrue(m.fuzz_result(text, "plain", seed, "cs", builder=True))
        finally:
            runner.temporary.cleanup()

    def test_omitted_native_fuzz_or_controls_fail_actual_main_contract(self):
        real_runner=m.Runner
        m.Runner=FakeRunner
        try:
            for function in ("run_native","run_fuzz","run_controls"):
                real=getattr(m,function)
                setattr(m,function,lambda *_:None)
                try:
                    output=io.StringIO()
                    with contextlib.redirect_stdout(output):
                        self.assertEqual(m.main(["go"]),1)
                    self.assertNotIn("NIGHTLY_READINESS_PASS",output.getvalue())
                    FakeRunner.instances[-1].temporary.cleanup()
                finally:
                    setattr(m,function,real)
        finally:
            m.Runner=real_runner

    def test_cpp_outer_timeout_copy_is_exact_and_drift_rejected(self):
        runner=FakeRunner(ROOT,"cpp")
        m.run_external(runner,"cpp")
        copied=(runner.out/"Makefile.uncapped").read_text()
        receipt=json.loads((runner.out/"Makefile.uncapped.json").read_text())
        original=(ROOT/"Makefile").read_text()
        expected=original
        for before,after in receipt["replacements"].items():
            self.assertEqual(expected.count(before),1)
            expected=expected.replace(before,after)
        self.assertEqual(copied,expected)
        self.assertEqual(len(receipt["replacements"]),2)
        runner.temporary.cleanup()
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            (root/"Makefile").write_text(original.replace("go test ./tools/fixedtwin -count=1","changed recipe"))
            runner=FakeRunner(root,"cpp")
            with contextlib.redirect_stdout(io.StringIO()):
                m.run_external(runner,"cpp")
            self.assertTrue(runner.results[-1]["errors"])
            self.assertFalse(runner.calls)
            runner.temporary.cleanup()

    def test_packet_markers_and_noops(self):
        self.assertEqual(m.packet_result("OK\n"),[])
        self.assertEqual(m.packet_result("OK (10 iterations, seed 2)\n",random=True),[])
        self.assertEqual(m.packet_result("Generated: /tmp/exe\n",compilation=True),[])
        for text in ("", "success", "OK\nOK\n"):
            self.assertTrue(m.packet_result(text))
        self.assertTrue(m.packet_result("OK (0 iterations, seed 2)",random=True))

    def test_every_native_retirement_requires_exact_marker_and_current_replacement(self):
        self.assertEqual(sum(map(len,m.RETIRED_NATIVE.values())),68)
        for lang, entries in m.RETIRED_NATIVE.items():
            for marker, required in entries.items():
                with self.subTest(lang=lang,marker=marker), contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(m.native_retirement_errors(marker,lang,required),[])
                    self.assertTrue(m.native_retirement_errors(marker,lang,set()))
                    self.assertTrue(m.native_retirement_errors(marker+" changed",lang,required))
                    for missing in required:
                        self.assertTrue(m.native_retirement_errors(marker,lang,required-{missing}))
        self.assertTrue(m.native_retirement_errors("SKIP: unknown future test","go",set()))
        self.assertTrue(m.native_retirement_errors("SKIPPED: owed live ruling","elixir",set()))

    def test_js_retirement_inventory_is_exact(self):
        source = (ROOT / "test/js-tables/fixedform.mjs").read_text()
        calls = re.findall(r'  retired\("([^"]+)", "([^"]+)"\);', source)
        markers = {"RETIRED (docs/FIXED-FORM-ALGORITHM.md §5.6): " + name + " — " + reason
                   for name, reason in calls}
        self.assertEqual(len(calls), 9)
        self.assertEqual(markers, set(m.RETIRED_NATIVE["js"]))

    def test_java_retirement_inventory_is_exact(self):
        main_java = (ROOT/"test/java-fixedform/src/Main.java").read_text()
        calls = m.re.findall(r'retired\(\s*"([^"]+)"\s*,\s*"([^"]+)"\s*\)', main_java)
        expected = {f"RETIRED by §5.6: {fn} — {owed}" for fn, owed in calls}
        self.assertEqual(len(expected), 7)
        self.assertEqual(set(m.RETIRED_NATIVE["java"]), expected)

    def test_go_retirement_without_replacement_is_incomplete(self):
        for name,(reason,required) in m.RETIRED.items():
            text="\n".join(native(name=name,action="skip",output=reason).splitlines()[:-1])+"\n"+native()
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertTrue(m.native_result(text,[name,"TestReady"],"gotable",set()))
                self.assertEqual(m.native_result(text,[name,"TestReady"],"gotable",set(required)),[])

    def test_inherited_noop_skip_and_update_flags_are_removed(self):
        unsafe={"MAKEFLAGS":"-n","MFLAGS":"-n","GNUMAKEFLAGS":"-n","MAKEFILES":"/tmp/injected.mk","MAKEOVERRIDES":"SKIP=1",
                "SCHEMA_SKIP_LEGS":"go","SCHEMA_UPDATE_WIRE_GOLDENS":"1",
                "SCHEMA_UPDATE_HEADER_GOLDEN":"1","GOFLAGS":"-run=Nothing",
                "JAVA":"/approved/tool/java"}
        with tempfile.TemporaryDirectory() as tmp,patch.dict(m.os.environ,unsafe):
            runner=m.Runner(Path(tmp),"go")
            for key in unsafe:
                if key not in ("JAVA","GOFLAGS"):
                    self.assertNotIn(key,runner.env)
            self.assertEqual(runner.env["GOFLAGS"],"-p=2")
            self.assertEqual(runner.env["JAVA"],"/approved/tool/java")
            self.assertEqual(runner.env["SCHEMA_SLOW"],"1")
            self.assertEqual(runner.env["SCHEMA_REQUIRE_CORPUS"],"1")
            self.assertEqual(runner.env["SCHEMA_C_COLLECTIONS_DRIVER"],str(runner.root/"build/c-collections-fuzz"))
            self.assertEqual(runner.env["SCHEMA_CPP_COLLECTIONS_DRIVER"],str(runner.root/"build/cpp-collections-fuzz"))

    def test_packet_features_keep_current_controls_after_wstring_retirement(self):
        for lang in m.LANGUAGES:
            runner=FakeRunner(ROOT,lang)
            m.run_packets(runner,lang)
            calls={name:argv for name,argv,_ in runner.calls}
            features=calls["packet-features"]
            if lang=="cpp":
                self.assertIn("packet-defaults-cpp",features)
                self.assertNotIn("build/schema_test_wide",calls["packet-build"])
                self.assertNotIn("packet-schema_test_wide",m.required_phases(lang))
            else:
                expected=["packet-"+feature+"-"+lang+tail
                          for feature in ("defaults","utf8")
                          for tail in ("","-negative-control")]
                if lang in ("go","java","dart","elixir","goose","moo","python","ruby"):
                    expected += ["packet-finite-"+lang,"packet-finite-"+lang+"-negative-control"]
                targets=features[features.index("-j2")+1:]
                if lang in ("python","ruby"):
                    # through the uncapped copy of the reviewed recipes
                    self.assertEqual(targets[:2],["-f",str(runner.out/f"Makefile.{lang}-uncapped-packet-features")])
                    targets=targets[2:]
                self.assertEqual(targets,expected)
            self.assertFalse(any("packet-wide-" in word or word.startswith("wide-") for word in features))
            runner.temporary.cleanup()
        self.assertEqual(len(m.required_phases("js")),23)

    def test_existing_target_catalog_and_outer_go_mappings(self):
        files=[ROOT/"Makefile"]+list((ROOT/"make").rglob("*.mk"))
        blocks={}
        for file in files:
            current=[]
            for line in file.read_text().splitlines():
                if line.startswith("\t"):
                    for name in current:blocks[name].append(line)
                elif line and not line.startswith(("#"," ")):
                    current=[]
                    match=m.re.match(r"^([^:=]+):(?!=)(.*)$",line)
                    if match:
                        for name in match[1].split():
                            if not name.startswith("."):
                                blocks.setdefault(name,[]);current.append(name)
        targets=set()
        for text in [*m.FIXED_GATES.values(),*m.DRIVER_BUILD.values(),*m.WIRE_CONTROLS.values()]:
            targets.update(text.split())
        targets.update(("bin/schema","build/conformance-harness","build/c-collections-fuzz","build/cpp-collections-fuzz",
                        "goose-toolchain-ready","build/conformance-harness-goose","build/goose-packet/.stamp",
                        "packet-finite-goose","packet-finite-goose-negative-control",
                        "moo-toolchain-ready","build/conformance-harness-moo","build/schema-moo","moo-packet-driver",
                        "packet-finite-moo","packet-finite-moo-negative-control",
                        "dist-python","python-toolchain-check","build/conformance-harness-python","build/schema-python",
                        "packet-finite-python","packet-finite-python-negative-control",
                        "dist-ruby","ruby-toolchain-check","build/conformance-harness-ruby","build/schema-ruby",
                        "packet-finite-ruby","packet-finite-ruby-negative-control"))
        for lang in m.LANGUAGES:
            if lang!="cpp":
                for feature in ("defaults","utf8"):
                    for tail in ("","-negative-control"):
                        targets.add("packet-"+feature+"-"+lang+tail)
        self.assertFalse(targets-set(blocks),sorted(targets-set(blocks)))
        self.assertEqual(m.WIRE_CONTROLS["cs"],
                         "tables-cs-pack-negative-control tables-cs-retain-negative-control")
        outer={name for name in targets if any(m.re.search(r"\bgo test\b|test/slowgate/proof",line)
                                                for line in blocks[name])}
        # Goose's gates run go test in their own recipes; the readiness run
        # executes them through goose_uncapped_makefile, which uncaps each.
        goose_outer={name for name in outer if name.startswith("tables-goose-")}
        # MOO's likewise, through moo_uncapped_makefile.
        moo_outer={name for name in outer if name.startswith("tables-moo-")}
        # Python's likewise, through python_uncapped_makefile, in each phase
        # that makes them: the packet features, the external gates and the
        # verdict-pipe controls.
        python_outer={name for name in outer if "python" in name}
        ruby_outer={name for name in outer if "ruby" in name}
        self.assertEqual(outer-goose_outer-moo_outer-python_outer-ruby_outer,{"tables-view","tables-fixed-twin"})
        self.assertEqual(moo_outer,set(m.FIXED_GATES["moo"].split()))
        self.assertEqual(goose_outer,set(m.FIXED_GATES["goose"].split()))
        runner=FakeRunner(ROOT,"python")
        uncapped=set()
        for phase,targets in (("packet-features",m.PYTHON_PACKET_FEATURES),
                              ("external-table-gates",m.FIXED_GATES["python"]),
                              ("wire-retain-controls",m.WIRE_CONTROLS["python"])):
            self.assertIsNotNone(m.python_uncapped_makefile(runner,targets,phase))
            receipt=json.loads((runner.out/("Makefile.python-uncapped-"+phase+".json")).read_text())
            self.assertLessEqual(set(receipt),set(targets.split()))
            uncapped|=set(receipt)
        self.assertEqual(uncapped,python_outer)
        self.assertEqual(python_outer,{"packet-defaults-python","packet-defaults-python-negative-control",
                                       "packet-finite-python-negative-control",
                                       "python-fixed-writer-lineage","python-fixed-writer-lineage-negative-control",
                                       "tables-python-wire-fuzz-negative-control",
                                       "tables-python-wire-fuzz-retain-negative-control"})
        runner.temporary.cleanup()
        runner=FakeRunner(ROOT,"goose")
        m.goose_uncapped_makefile(runner,m.FIXED_GATES["goose"])
        receipt=json.loads((runner.out/"Makefile.goose-uncapped.json").read_text())
        self.assertEqual(set(receipt),goose_outer)
        runner.temporary.cleanup()

    def test_goose_plan_is_the_expanded_shape(self):
        required=m.required_phases("goose")
        for phase in ("prerequisites","implementation","native-goose","native-goosetable","native-lockfile",
                      "packet-build","packet-normal-0","packet-normal-1","packet-ludicrous-0","packet-ludicrous-1",
                      "packet-features","external-table-gates","wire-driver-build","wire-retain-controls",
                      "retain-goose-tablewire","retain-goose-fuzz",
                      *("wire-"+arm+"-"+str(seed) for arm in ("plain","retain") for seed in (1,2,3))):
            self.assertIn(phase,required)
        # no §5.6 retirement is admitted for Goose
        self.assertNotIn("goose",m.RETIRED_NATIVE)
        self.assertTrue(m.native_retirement_errors("RETIRED (docs/FIXED-FORM-ALGORITHM.md §5.6): anything","goose",set()))
        skip="\n".join(native("goosetable","TestFixedFormPlanPath","skip",m.RETIRED["TestFixedFormPlanPath"][0]).splitlines()[:-1])+"\n"+native("goosetable")
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(m.native_result(skip,["TestFixedFormPlanPath","TestReady"],"goosetable",
                                            set(m.RETIRED["TestFixedFormPlanPath"][1])))
        self.assertEqual(m.driver_command(ROOT,"goose"),"build/goose-wirefuzz-dispatch build/goose-wirefuzz-legs/legs")
        self.assertIn("goose",m.MIXED_BUILDER)

    def test_goose_main_uses_tagged_harness_and_dispatcher(self):
        real=m.Runner
        m.Runner=FakeRunner
        try:
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(m.main(["goose"]),0)
            runner=FakeRunner.instances[-1]
            calls={name:(argv,cwd) for name,argv,cwd in runner.calls}
            self.assertIn("build/conformance-harness-goose",calls["prerequisites"][0])
            self.assertIn("goose-toolchain-ready",calls["prerequisites"][0])
            self.assertEqual(calls["implementation"][0][0],"./build/conformance-harness-goose")
            self.assertIn("build/conformance-goose-wirefuzz",calls["wire-driver-build"][0])
            for arm in ("plain","retain"):
                for seed in m.SEEDS:
                    argv=calls["wire-"+arm+"-"+str(seed)][0]
                    self.assertEqual(argv[0],"./build/conformance-harness")
                    self.assertEqual(argv[argv.index("--driver")+1],m.driver_command(ROOT,"goose"))
            for label in ("normal","ludicrous"):
                self.assertEqual(calls["packet-"+label+"-0"][1],ROOT/"build/goose-packet"/label)
                self.assertIn("--debug",calls["packet-"+label+"-1"][0])
            self.assertIn("-f",calls["external-table-gates"][0])
            for name in ("retain-goose-tablewire","retain-goose-fuzz"):
                self.assertIn("-timeout=0",calls[name][0])
            runner.temporary.cleanup()
        finally:
            m.Runner=real

    def test_goose_dist_matches_the_go_pin(self):
        source=(ROOT/"internal/codegen/goose/gooserun").glob("*.go")
        text="".join(p.read_text() for p in source)
        version=m.re.search(r'Version\s*=\s*"([^"]+)"',text)[1]
        self.assertEqual(m.GOOSE_DIST,"dist/goose-"+version+"-linux-x64")

    def test_goose_uncapped_makefile_is_exact_and_drift_rejected(self):
        runner=FakeRunner(ROOT,"goose")
        path=m.goose_uncapped_makefile(runner,m.FIXED_GATES["goose"])
        text=path.read_text()
        self.assertTrue(text.startswith("include Makefile\n"))
        self.assertNotIn("-timeout 600s",text)
        for target in m.FIXED_GATES["goose"].split():
            block=m.re.search(r"^"+m.re.escape(target)+r":\n((?:\t[^\n]*\n)+)",text,m.re.MULTILINE)
            self.assertIsNotNone(block,target)
            self.assertNotIn("go test -count",block[1])
            self.assertIn("go test -timeout=0 ",block[1])
        runner.temporary.cleanup()
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);(root/"make").mkdir()
            for f in (ROOT/"make").glob("goose*.mk"):
                (root/"make"/f.name).write_text(f.read_text().replace("go test -count=1 -timeout 600s","go test -count=1 -timeout 900s"))
            runner=FakeRunner(root,"goose")
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertIsNone(m.goose_uncapped_makefile(runner,m.FIXED_GATES["goose"]))
            self.assertTrue(runner.results[-1]["errors"])
            runner.temporary.cleanup()

    def test_moo_plan_is_the_expanded_shape(self):
        required=m.required_phases("moo")
        for phase in ("prerequisites","implementation","native-moo","native-mootable","native-lockfile",
                      "packet-build","packet-normal-0","packet-ludicrous-0",
                      "packet-features","external-table-gates","wire-driver-build","wire-retain-controls",
                      "retain-moo-save","retain-moo-refusal",
                      *("wire-"+arm+"-"+str(seed) for arm in ("plain","retain") for seed in (1,2,3))):
            self.assertIn(phase,required)
        self.assertNotIn("packet-normal-1",required)
        # no §5.6 retirement is admitted for MOO
        self.assertNotIn("moo",m.RETIRED_NATIVE)
        self.assertTrue(m.native_retirement_errors("RETIRED (docs/FIXED-FORM-ALGORITHM.md §5.6): anything","moo",set()))
        # one process over the whole roster, answering the region measure
        # itself: the plain profile, no dispatcher
        self.assertEqual(m.driver_command(ROOT,"moo"),"test/conformance/moo/wirefuzz-driver")
        self.assertNotIn("moo",m.MIXED_BUILDER)
        self.assertEqual(m.DRIVER_BUILD["moo"],"moo-wire-fuzz-ready")

    def test_moo_main_uses_tagged_harness_and_its_probes(self):
        real=m.Runner
        m.Runner=FakeRunner
        try:
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(m.main(["moo"]),0)
            runner=FakeRunner.instances[-1]
            calls={name:(argv,cwd) for name,argv,cwd in runner.calls}
            self.assertIn("build/conformance-harness-moo",calls["prerequisites"][0])
            self.assertIn("moo-toolchain-ready",calls["prerequisites"][0])
            self.assertEqual(calls["implementation"][0][0],"./build/conformance-harness-moo")
            for arm in ("plain","retain"):
                for seed in m.SEEDS:
                    argv=calls["wire-"+arm+"-"+str(seed)][0]
                    self.assertEqual(argv[0],"./build/conformance-harness")
                    self.assertEqual(argv[argv.index("--driver")+1],"test/conformance/moo/wirefuzz-driver")
                    self.assertNotIn("--mixed-builder",argv)
            for label,tests in m.MOO_PACKET_PROBES.items():
                argv=calls["packet-"+label+"-0"][0]
                self.assertEqual(argv[-1],"./internal/codegen/moo")
                self.assertEqual(argv[argv.index("-run")+1],"^("+"|".join(tests)+")$")
            for feature in ("defaults","utf8","finite"):
                self.assertIn("packet-"+feature+"-moo-negative-control",calls["packet-features"][0])
            self.assertIn("-f",calls["external-table-gates"][0])
            for name in ("retain-moo-save","retain-moo-refusal"):
                self.assertIn("-timeout=0",calls[name][0])
            runner.temporary.cleanup()
        finally:
            m.Runner=real

    def test_moo_probes_fail_on_a_missing_or_failed_probe(self):
        tests=list(m.MOO_PACKET_PROBES["normal"])
        partial="\n".join(native("moo",tests[0]).splitlines())
        self.assertTrue(m.native_result(partial,tests,"moo"))
        failed="\n".join(native("moo",tests[0],"fail").splitlines())
        self.assertTrue(m.native_result(failed,tests[:1],"moo"))

    def test_moo_uncapped_makefile_is_exact_and_drift_rejected(self):
        runner=FakeRunner(ROOT,"moo")
        path=m.moo_uncapped_makefile(runner,m.FIXED_GATES["moo"])
        text=path.read_text()
        self.assertTrue(text.startswith("include Makefile\n"))
        for target in m.FIXED_GATES["moo"].split():
            block=m.re.search(r"^"+m.re.escape(target)+r":\n((?:\t[^\n]*\n)+)",text,m.re.MULTILINE)
            self.assertIsNotNone(block,target)
            self.assertNotIn("go test -count",block[1])
            self.assertIn("go test -timeout=0 ",block[1])
        runner.temporary.cleanup()
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);(root/"make").mkdir()
            for f in (ROOT/"make").glob("moo*.mk"):
                (root/"make"/f.name).write_text(f.read_text().replace("go test -count=1 ","go test -count=1 -timeout 600s "))
            runner=FakeRunner(root,"moo")
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertIsNone(m.moo_uncapped_makefile(runner,m.FIXED_GATES["moo"]))
            self.assertTrue(runner.results[-1]["errors"])
            runner.temporary.cleanup()

    def test_python_plan_is_the_expanded_shape(self):
        required=m.required_phases("python")
        for phase in ("prerequisites","implementation","native-python","native-pythontable","native-lockfile",
                      "native-list-python","native-list-pythontable",
                      "packet-build","packet-normal-0","packet-ludicrous-0",
                      "packet-features","external-table-gates","wire-driver-build","wire-retain-controls",
                      "retain-python-save","retain-python-remap",
                      *("wire-"+arm+"-"+str(seed) for arm in ("plain","retain") for seed in (1,2,3))):
            self.assertIn(phase,required)
        self.assertNotIn("packet-normal-1",required)
        # no §5.6 retirement is admitted for Python
        self.assertNotIn("python",m.RETIRED_NATIVE)
        self.assertTrue(m.native_retirement_errors("RETIRED (docs/FIXED-FORM-ALGORITHM.md §5.6): anything","python",set()))
        # one process over every unit under the current pin, the plain profile
        self.assertEqual(m.driver_command(ROOT,"python"),
                         m.PYTHON_DIST+" -W error -I -B test/conformance/python/wirefuzz.py build/conformance-python")
        self.assertNotIn("python",m.MIXED_BUILDER)
        self.assertEqual(m.GO_TAGS,{"python":"schema_leg_python"})

    def test_python_main_uses_tagged_harness_pins_and_tags(self):
        real=m.Runner
        m.Runner=FakeRunner
        try:
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(m.main(["python"]),0)
            runner=FakeRunner.instances[-1]
            calls={name:(argv,cwd) for name,argv,cwd in runner.calls}
            for target in ("dist-python","python-toolchain-check","build/conformance-harness-python"):
                self.assertIn(target,calls["prerequisites"][0])
            self.assertEqual(calls["implementation"][0],
                             ["./build/conformance-harness-python","implementation","--lang","python"])
            for package in ("lockfile","python","pythontable"):
                for name in ("native-list-"+package,"native-"+package):
                    argv=calls[name][0]
                    self.assertEqual(argv[argv.index("-tags")+1],"schema_leg_python",name)
            for name in ("native-list-gotable","native-gotable"):
                self.assertNotIn(name,calls)
            for arm in ("plain","retain"):
                for seed in m.SEEDS:
                    argv=calls["wire-"+arm+"-"+str(seed)][0]
                    self.assertEqual(argv[0],"./build/conformance-harness")
                    self.assertEqual(argv[argv.index("--driver")+1],m.driver_command(ROOT,"python"))
                    self.assertNotIn("--mixed-builder",argv)
            for label,tests in m.PYTHON_PACKET_PROBES.items():
                argv=calls["packet-"+label+"-0"][0]
                self.assertEqual(argv[-1],"./internal/codegen/python")
                self.assertEqual(argv[argv.index("-tags")+1],"schema_leg_python")
                self.assertEqual(argv[argv.index("-run")+1],"^("+"|".join(tests)+")$")
            for name in ("retain-python-save","retain-python-remap"):
                argv=calls[name][0]
                self.assertIn("-timeout=0",argv)
                self.assertEqual(argv[-1],"./internal/codegen/pythontable")
            for phase in ("packet-features","external-table-gates","wire-retain-controls"):
                argv=calls[phase][0]
                self.assertEqual(argv[argv.index("-f")+1],str(runner.out/("Makefile.python-uncapped-"+phase)))
            self.assertIn("-f",calls["external-table-gates"][0])
            # every interpreter the plan starts is a pin, never a bare name
            for name,argv,cwd in runner.calls:
                for i,word in enumerate(argv):
                    if i and argv[i-1]=="--lang":
                        continue  # a language name, not a command word
                    for token in word.split():
                        self.assertIsNone(m.re.fullmatch(r"python[0-9.]*",token),(name,argv))
            runner.temporary.cleanup()
        finally:
            m.Runner=real

    def test_python_dist_matches_the_make_pin(self):
        mk=(ROOT/"make/python-toolchain.mk").read_text()
        version=m.re.search(r"^PYTHON_CURRENT_VERSION\s*:=\s*(\S+)$",mk,m.re.MULTILINE)[1]
        exe=m.re.search(r"^PYTHON\s*\?=\s*\$\(CURDIR\)/dist/python-\$\(PYTHON_CURRENT_VERSION\)/bin/(\S+)$",mk,m.re.MULTILINE)[1]
        self.assertEqual(m.PYTHON_DIST,"dist/python-"+version+"/bin/"+exe)

    def test_python_uncapped_makefile_is_exact_and_drift_rejected(self):
        runner=FakeRunner(ROOT,"python")
        for phase,targets in (("packet-features",m.PYTHON_PACKET_FEATURES),
                              ("external-table-gates",m.FIXED_GATES["python"]),
                              ("wire-retain-controls",m.WIRE_CONTROLS["python"])):
            text=m.python_uncapped_makefile(runner,targets,phase).read_text()
            self.assertTrue(text.startswith("include Makefile\n"))
            self.assertNotIn("-timeout 600s",text)
            receipt=json.loads((runner.out/("Makefile.python-uncapped-"+phase+".json")).read_text())
            self.assertTrue(receipt,phase)
            for target in receipt:
                block=m.re.search(r"^"+m.re.escape(target)+r":\n((?:\t[^\n]*\n)+)",text,m.re.MULTILINE)
                self.assertIsNotNone(block,target)
                for line in block[1].splitlines():
                    if "go test " in line or "test/slowgate/proof " in line:
                        self.assertRegex(line,r"-timeout=0(\s|$)")
        runner.temporary.cleanup()
        for before,after in (("go test -count=1 -timeout 600s","go test -count=1 -timeout 900s"),
                             ("-count=1 -timeout 600s -tags","-count=1 -timeout=10m -tags")):
            with tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp);(root/"make").mkdir()
                for f in (ROOT/"make").glob("python*.mk"):
                    (root/"make"/f.name).write_text(f.read_text().replace(before,after))
                runner=FakeRunner(root,"python")
                with contextlib.redirect_stdout(io.StringIO()):
                    self.assertIsNone(m.python_uncapped_makefile(runner,m.PYTHON_PACKET_FEATURES,"packet-features"))
                self.assertTrue(runner.results[-1]["errors"])
                runner.temporary.cleanup()
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);(root/"make").mkdir()
            for f in (ROOT/"make").glob("python*.mk"):
                (root/"make"/f.name).write_text(f.read_text())
            (root/"make"/"python-zz-duplicate.mk").write_text("packet-defaults-python:\n\techo shadow\n")
            runner=FakeRunner(root,"python")
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertIsNone(m.python_uncapped_makefile(runner,m.PYTHON_PACKET_FEATURES,"packet-features"))
            self.assertTrue(runner.results[-1]["errors"])
            runner.temporary.cleanup()

    def test_real_runner_pwd_matches_each_physical_working_directory(self):
        # Go honours a valid inherited logical PWD while make CURDIR resolves
        # it. Their different source spellings silently miss Go overlays.
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp).resolve()
            root = base / "physical"
            root.mkdir()
            nested = root / "packet"
            nested.mkdir()
            link = base / "logical"
            link.symlink_to(root, target_is_directory=True)
            with patch.dict(m.os.environ, {"PWD": str(link)}):
                runner = m.Runner(root, "goose")
            for name, cwd in (("root", None), ("packet", link / "packet")):
                with self.subTest(name=name), contextlib.redirect_stdout(io.StringIO()):
                    text, status = runner.run(name, [m.sys.executable, "-c",
                        "import os; print(os.environ['PWD']); print(os.getcwd())"], cwd=cwd)
                    actual = (cwd or root).resolve()
                    self.assertEqual(status, 0)
                    self.assertEqual(text.splitlines(), [str(actual), str(actual)])
                    command = json.loads((runner.out / (name + ".command.json")).read_text())
                    self.assertEqual(command["cwd"], str(actual))
                    self.assertEqual(command["PWD"], str(actual))
            self.assertEqual(runner.env["PWD"], str(link))

    def test_real_runner_nonzero_cannot_be_masked_by_valid_summary(self):
        runner=self.runner("go")
        runner.env={}
        output=io.StringIO()
        with contextlib.redirect_stdout(output):
            runner.run("probe",[m.sys.executable,"-c","print('valid-looking'); raise SystemExit(7)"])
        self.assertTrue(runner.results[-1]["errors"])
        self.assertEqual(runner.results[-1]["exit"],7)

    def test_real_runner_parallel_preserves_order_logs_and_errors(self):
        with tempfile.TemporaryDirectory() as tmp:
            runner = m.Runner(Path(tmp), "go")
            specs = [
                {
                    "name": f"par-{i}",
                    "argv": [m.sys.executable, "-c", f"import sys; print('line-{i}'); sys.exit({1 if i == 2 else 0})"],
                    "validate": lambda text, idx=i: [] if f"line-{idx}" in text else ["missing line"],
                }
                for i in range(4)
            ]
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                out = runner.run_parallel(specs)
            self.assertEqual([row["phase"] for row in runner.results], ["par-0", "par-1", "par-2", "par-3"])
            self.assertEqual([status for _, status in out], [0, 0, 1, 0])
            self.assertEqual(runner.results[0]["errors"], [])
            self.assertEqual(runner.results[1]["errors"], [])
            self.assertTrue(runner.results[2]["errors"])
            self.assertEqual(runner.results[3]["errors"], [])
            for i in range(4):
                self.assertTrue((runner.out / f"par-{i}.command.json").exists())
                self.assertIn(f"line-{i}", (runner.out / f"par-{i}.log").read_text())


if __name__=="__main__":
    unittest.main(verbosity=2)
