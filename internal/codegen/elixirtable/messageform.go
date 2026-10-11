// THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3), form byte 2, in Elixir: the
// ANNOUNCEMENT a peer sends once and every body then rides under. A message is
// a batch of fixed tables packed through the type's own codec under ONE
// announced vocabulary, so the announcement is the whole of the connection's
// framing.
//
// THE ANNOUNCEMENT IS AN ORDINARY FORM-1 FILE, and every byte of it is settled
// by the compiler, so it is EMITTED AS A CONSTANT rather than walked at load
// time (ir.TableAnnouncement). This module is that constant and its length;
// the surface a caller reaches for is `announce/0` and `announce_measure/0`.
//
// THE FORM IS A FIXED TABLE'S, and this emitter writes it for a unit whose
// `fixed table` roots exist — the same roots fixedRoots answers, the ONE
// closure test this leg holds no private copy of.
package elixirtable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageFormModule is the module the announcement surface lives in. It is a
// generated file's basename as well as a module name.
const MessageFormModule = "MessageForm"

// generateMessageForm emits the announcement module for a unit with fixed
// table roots, and nothing for a unit without one: a unit that declares no
// fixed table has no message form (docs/SPEC-TABLES.md §3.3).
func generateMessageForm(u *ir.Unit, ns string) map[string][]byte {
	if len(fixedRoots(u)) == 0 {
		return nil
	}
	announcement := ir.TableAnnouncement(u)
	var b strings.Builder
	b.WriteString(header(MessageFormModule, u.Package, "the MESSAGE FORM's announcement (docs/SPEC-TABLES.md §3.3)"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "defmodule %s.%s do\n", ns, MessageFormModule)
	b.WriteString("  @moduledoc \"\"\"\n")
	b.WriteString("  The MESSAGE FORM (docs/SPEC-TABLES.md §3.3): a batch of fixed tables\n")
	b.WriteString("  under one announced vocabulary. The announcement is an ordinary form-1\n")
	b.WriteString("  file the compiler settles, so it is a constant here and not a walk.\n")
	b.WriteString("  \"\"\"\n\n")
	fmt.Fprintf(&b, "  @announcement <<%s>>\n\n", elixirByteLiteral(announcement))
	b.WriteString("  def announce, do: @announcement\n")
	b.WriteString("  def announce_measure, do: byte_size(@announcement)\n")
	b.WriteString("end\n")
	return map[string][]byte{MessageFormModule + ".ex": []byte(b.String())}
}

// elixirByteLiteral spells a byte run as the contents of an Elixir binary
// literal, <<0x01, 0x02, ...>>.
func elixirByteLiteral(b []byte) string {
	var s strings.Builder
	for i, v := range b {
		if i > 0 {
			s.WriteString(", ")
		}
		fmt.Fprintf(&s, "0x%02X", v)
	}
	return s.String()
}
