package cstable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"testing"
)

// Exercise the generated reader, including text that is skipped rather than
// stored and a NUL beyond the storage bound. Corpus pins remain unchanged.
func TestStoredTextRefusalsCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t15RunCS(t, `package probe
table Child { name string(2) }
table Root {
 name string(2)
 wide wstring(2)
 blob *string
 children []Child
 names map[string(2)]int32
 ids map[int32]int32
 objects map[string(2)]Child
}`, `
string[] texts = {
 "{\"name\":\"ab\\u0000\"}",
 "{\"wide\":\"ab\\u0000\"}",
 "{\"blob\":\"\\u0000\"}",
 "{\"children\":[{\"name\":\"\\u0000\"}]}",
 "{\"names\":{\"long\\u0000\":1}}",
 "{\"ids\":{\"long\\u0000\":1}}",
 "{\"objects\":{\"a\":{\"name\":\"ok\"},\"b\\u0000\":{}}}"
};
string[] fields = { "Root.name", "Root.wide", "Root.blob", "Child.name", "Root.names", "Root.ids", "Root.objects" };
for (int i = 0; i < texts.Length; i++) {
 var value = new Probe.Root();
 var report = new Probe.TableReport { Unknown = 3 };
 bool ok = Probe.Schema.RootFromJson(value, System.Text.Encoding.UTF8.GetBytes(texts[i]), report);
 if (ok || !report.Malformed || report.Reason != "text_holds_zero: " + fields[i] || report.Unknown != 3 || report.Clamped != 0)
  bad += ProbeLog.Fail("stored NUL scope " + i + " reason=" + report.Reason);
 if (!Probe.Schema.RootFromJson(value, System.Text.Encoding.UTF8.GetBytes("{\"name\":\"ok\"}"), report) || report.Refused || report.Reason != null)
  bad += ProbeLog.Fail("prior diagnostic poisoned a clean read");
}
foreach (string text in new[] { "{\"unknown\":\"\\u0000\"}", "{\"\\u0000\":\"ignored\"}", "{\"name\":\"\\u0001\"}" }) {
 var report = new Probe.TableReport();
 if (!Probe.Schema.RootFromJson(new Probe.Root(), System.Text.Encoding.UTF8.GetBytes(text), report) || report.Malformed)
  bad += ProbeLog.Fail("valid skipped/control text refused");
}
foreach (string text in new[] { "{\"name\":\"ab~\"}", "{\"unknown\":\"~\"}", "{\"~\":0}" }) {
 byte[] bytes = System.Text.Encoding.UTF8.GetBytes(text);
 bytes[Array.IndexOf(bytes, (byte)'~')] = 0xff;
 var report = new Probe.TableReport { Unknown = 3 };
 if (Probe.Schema.RootFromJson(new Probe.Root(), bytes, report) || !report.Malformed ||
     report.Reason == null || !report.Reason.StartsWith("string_not_utf8: ") || report.Clamped != 0)
  bad += ProbeLog.Fail("invalid UTF8 must refuse before clamp at every string site");
}
`)
}
