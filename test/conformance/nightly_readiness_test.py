import contextlib
import importlib.machinery
import importlib.util
import io
import json
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
    pkg = "github.com/mas-bandwidth/schema/v2/" + m.package_path(package)
    return "\n".join(json.dumps(x) for x in [
        {"Package":pkg,"Action":"run","Test":name},
        {"Package":pkg,"Action":"output","Test":name,"Output":output},
        {"Package":pkg,"Action":action,"Test":name},
        {"Package":pkg,"Action":"pass"}])


def fuzz(arm="plain", seed=1, lang="", builder=False):
    prefix = "wire-fuzz" + ("-builder" if builder else "-mixed-builder" if lang == "dart" else "") + (" retain" if arm == "retain" else "")
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
        elif name.startswith("packet-"):
            text = "OK"
            if name == "packet-schema_test_random":
                text = "OK (50000 iterations, seed 42)"
            elif self.lang == "dart" and name.endswith("-1"):
                text = "Generated: /tmp/mock-packet"
        elif name.endswith("-plant"):
            Path(argv[argv.index("-out")+1]).write_text("mock planted source\n")
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
                    if lang != "dart":
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

    def test_full_mock_plan_all_nine_and_timeout_commands(self):
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
                for name,argv,cwd in runner.calls:
                    if "test" in argv and ("go" in argv):
                        self.assertIn("-timeout=0",argv,(name,argv))
                    if name.startswith("wire-plain-") or (name.startswith("wire-retain-") and name[-1:].isdigit()):
                        self.assertNotIn("--finite-owed",argv)
                        self.assertEqual("--mixed-builder" in argv, lang == "dart")
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
                self.assertIn("wire-fuzz-region", argv[argv.index("--driver")+1])
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
        self.assertEqual(sum(map(len,m.RETIRED_NATIVE.values())),51)
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

    def test_go_packet_overlay_only_removes_outer_suite_limit(self):
        runner=FakeRunner(ROOT,"go")
        original=(ROOT/"make/go.mk").read_text()
        before="go test -count=1 ."
        after="go test -timeout=0 -count=1 -v ."
        path=m.go_packet_makefile(runner)
        self.assertIsNotNone(path)
        text=path.read_text()
        self.assertTrue(text.startswith("include Makefile\npacket-wide-go:\n"))
        recipe=m.re.search(r"^packet-wide-go:[^\n]*\n((?:\t[^\n]*\n)+)",original,m.re.MULTILINE)[1]
        self.assertEqual(text,"include Makefile\npacket-wide-go:\n"+recipe.replace(before,after))
        runner.temporary.cleanup()
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);(root/"make").mkdir()
            (root/"make/go.mk").write_text(original.replace(before,"changed"))
            runner=FakeRunner(root,"go")
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertIsNone(m.go_packet_makefile(runner))
            self.assertTrue(runner.results[-1]["errors"])
            runner.temporary.cleanup()

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
        targets.update(("bin/schema","build/conformance-harness","build/c-collections-fuzz","build/cpp-collections-fuzz"))
        for lang in m.LANGUAGES:
            if lang!="cpp":
                for feature in ("defaults","utf8","wide"):
                    for tail in ("","-negative-control"):
                        targets.add("packet-"+feature+"-"+lang+tail)
        self.assertFalse(targets-set(blocks),sorted(targets-set(blocks)))
        self.assertEqual(m.WIRE_CONTROLS["cs"],
                         "tables-cs-pack-negative-control tables-cs-retain-negative-control")
        outer={name for name in targets if any(m.re.search(r"\bgo test\b|test/slowgate/proof",line)
                                                for line in blocks[name])}
        self.assertEqual(outer,{"tables-view","tables-fixed-twin","packet-wide-go"})

    def test_real_runner_nonzero_cannot_be_masked_by_valid_summary(self):
        runner=self.runner("go")
        runner.env={}
        output=io.StringIO()
        with contextlib.redirect_stdout(output):
            runner.run("probe",[m.sys.executable,"-c","print('valid-looking'); raise SystemExit(7)"])
        self.assertTrue(runner.results[-1]["errors"])
        self.assertEqual(runner.results[-1]["exit"],7)


if __name__=="__main__":
    unittest.main(verbosity=2)
