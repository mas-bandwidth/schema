(* Unit tests: no time, no sockets, no network, no files. *)

let fails = ref 0
let check name ok = if not ok then (incr fails; Printf.printf "FAIL %s\n" name)

let refuses f = match f () with exception Jsonr.Refuse _ -> true | _ -> false

let () =
  (* ids, SPEC-TABLES §3 *)
  check "fnv key" (Model.fnv1a64 "key" = 0x3DC94A19365B10ECL);
  check "fnv value" (Model.fnv1a64 "value" = 0x7CE4FD9430E80CEAL);
  (* big ints: past 63 bits *)
  let m = Big.pred (Big.pow2 64) in
  check "u64 max text" (Big.to_string m = "18446744073709551615");
  check "u64 round trip" (Big.equal (Big.of_le_bytes (Big.to_le_bytes m 8) 0 8) m);
  check "i64 min" (Big.equal (Big.of_le_bytes ~signed:true (Big.to_le_bytes (Big.neg (Big.pow2 63)) 8) 0 8) (Big.neg (Big.pow2 63)));
  (* LEB128 *)
  let b = Buffer.create 4 in Wirecommon.leb_put b 300;
  check "leb 300" (Buffer.contents b = "\xac\x02");
  (* W12 float spelling *)
  check "f32 0.1" (Num.float_text ~is32:true (Model.to_f32 0.1) = "0.1");
  check "f64 0.1" (Num.float_text ~is32:false 0.1 = "0.1");
  check "f32 max" (Num.f32_of_token "3.4028235e38" <> None);
  check "f32 past range" (Num.f32_of_token "1e39" = None);
  check "f32 ties to even" (Num.f32_of_token "16777217" = Some 16777216.0);
  (* W11 fixed *)
  check "fixed 1.5" (Num.fixed_text (Big.of_int 98304) 16 = "1.5");
  check "fixed exact" (Num.fixed_raw_of_token "0.1" 16 = None);
  (* base64 *)
  check "b64" (Num.b64_encode "A" = "QQ==");
  check "b64 noncanon" (Num.b64_decode "QR==" = `Noncanonical);
  (* timestamps *)
  check "rfc3339 zero" (Timefmt.to_rfc3339 Big.zero = "1970-01-01T00:00:00Z");
  check "rfc3339 leap 60" (Timefmt.of_rfc3339 "2026-01-01T00:00:60Z" = None);
  (* the text grammar *)
  check "trailing comma" (not (refuses (fun () -> Jsonr.parse "{\"a\":1,}")));
  check "comment" (not (refuses (fun () -> Jsonr.parse "// c\n{}")));
  check "repeated key" (refuses (fun () -> Jsonr.parse "{\"a\":1,\"a\":2}"));
  check "data after root" (refuses (fun () -> Jsonr.parse "{} 1"));
  check "lone surrogate" (refuses (fun () -> Jsonr.parse "{\"a\":\"\\ud800\"}"));
  check "zero" (refuses (fun () -> Jsonr.parse "{\"a\":\"\\u0000\"}"));
  let deep k = String.make k '[' ^ String.make k ']' in
  check "depth 129 reads" (not (refuses (fun () -> Jsonr.parse ("{\"a\":" ^ deep 128 ^ "}"))));
  check "depth 130 refuses" (refuses (fun () -> Jsonr.parse ("{\"a\":" ^ deep 129 ^ "}")));
  if !fails = 0 then print_endline "ok" else exit 1
