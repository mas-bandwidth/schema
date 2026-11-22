(* Form 3, the fixed form: SPEC-TABLES §3.4 and docs/FIXED-FORM-ALGORITHM.md.
   The identity plan only; another layout hash is read through the lock's
   lineage, which this port does not compile (see SPEC-GAPS.md). *)

open Model
open Wirecommon

exception Refuse of string
let refuse s = raise (Refuse s)

let ord_width n =
  if n < 0x100 then 1 else if n < 0x10000 then 2 else if n < 0x100000000 then 4 else 8

(* C(f): a field's constant size, FIXED-FORM-ALGORITHM §3 *)
let rec csize (t : ty) : int =
  match t with
  | Bool -> 1
  | Int { w; _ } -> w
  | Bits n -> if n <= 32 then 4 else 8
  | I128 _ | U128 -> 16
  | F32 | CF32 _ -> 4
  | F64 -> 8
  | Fixed { i; f; _ } -> (i + f) / 8
  | Flags _ -> 8
  | Enum e -> ord_width (Array.length e.e_vars)
  | Struct s -> Array.fold_left (fun a (f : field_d) -> a + csize f.f_ty) 0 s.s_fields
  | Arr (AFixed n, et) -> n * csize et
  | Arr (ACounted (_, mx), et) -> 4 + mx * csize et
  | Str (Some n) | Bytes n -> 4 + n
  | Union un -> ord_width (Array.length un.u_arms) + arms_widest un
  | Opt t -> 1 + csize t
  | Keyed (en, et) -> Array.length en.e_vars * csize et
  | Timestamp -> 8
  | Str None | Arr (AOpen, _) | Map _ | Ptr _ | Blob _ -> refuse "not a fixed construct"

and arms_widest un =
  Array.fold_left (fun a arm -> max a (match arm.a_ty with None -> 0 | Some t -> csize t)) 0 un.u_arms

(* ---- the layout, §1 ---- *)

let layout_kind (t : ty) =
  match t with
  | Bits n -> bits_kind n
  | Opt _ -> 35
  | Bytes _ -> 14
  | Timestamp -> 5   (* SPEC-GAP: no layout kind for timestamp; gated, never reached *)
  | _ -> kind_of t

let layout (root : struct_d) : string =
  let entries = Buffer.create 256 in
  let count = ref 0 in
  let entry id kind size children =
    incr count;
    let b = Bytes.create 17 in
    Bytes.set_int64_le b 0 id;
    Bytes.set_uint8 b 8 kind;
    Bytes.set_int32_le b 9 (Int32.of_int size);
    Bytes.set_int32_le b 13 (Int32.of_int children);
    Buffer.add_bytes entries b in
  let rec walk id (t : ty) =
    match t with
    | Struct s ->
      entry id 13 (csize t) (Array.length s.s_fields);
      Array.iter (fun (f : field_d) -> walk f.f_id f.f_ty) s.s_fields
    | Arr (_, et) -> entry id 14 (csize t) 1; walk 0L et
    | Bytes _ ->
      entry id 14 (csize t) 1;
      entry 0L 6 1 0
    | Union un ->
      entry id 15 (csize t) (Array.length un.u_arms);
      Array.iter (fun arm ->
          match arm.a_ty with
          | None -> entry arm.a_id 32 0 0
          | Some at -> walk arm.a_id at) un.u_arms
    | Keyed (en, et) ->
      entry id 16 (csize t) 2;
      walk 0L (Enum en);
      walk 0L et
    | Enum e ->
      entry id 30 (csize t) (Array.length e.e_vars);
      Array.iter (fun v -> entry v.v_id 32 0 0) e.e_vars
    | Opt p -> entry id 35 (csize t) 1; walk 0L p
    | _ -> entry id (layout_kind t) (csize t) 0 in
  walk root.s_id (Struct root);
  let b = Bytes.create 4 in
  Bytes.set_int32_le b 0 (Int32.of_int !count);
  Bytes.to_string b ^ Buffer.contents entries

let fnv_update h (s : string) =
  let h = ref h in
  String.iter (fun c ->
      h := Int64.logxor !h (Int64.of_int (Char.code c));
      h := Int64.mul !h 0x100000001b3L) s;
  !h

let layout_hash layout digest = fnv_update (fnv_update 0xcbf29ce484222325L layout) digest

(* ---- the definitions digest, SPEC-TABLES §3.4 (current scheme) ---- *)

let digest (root : struct_d) : string =
  let out = Buffer.create 256 in
  Buffer.add_string out "\000schema-fixed-definitions\002";
  let u32 b v = let x = Bytes.create 4 in Bytes.set_int32_le x 0 (Int32.of_int v); Buffer.add_bytes b x in
  let u64 b v = let x = Bytes.create 8 in Bytes.set_int64_le x 0 v; Buffer.add_bytes b x in
  let seg role kind id = let b = Buffer.create 13 in Buffer.add_char b role; u32 b kind; u64 b id; Buffer.contents b in
  let row tag path payload =
    Buffer.add_char out tag; u32 out (String.length path); Buffer.add_string out path;
    u32 out (String.length payload); Buffer.add_string out payload in
  let canon (v : Big.t) =
    let b = Buffer.create 8 in
    Buffer.add_char b (if Big.compare v Big.zero < 0 then '\001' else '\000');
    let m = Big.mag_be_bytes v in
    u32 b (String.length m); Buffer.add_string b m; Buffer.contents b in
  let rec leaf (t : ty) =
    match t with
    | Keyed _ -> 16
    | Arr _ | Bytes _ -> 14
    | Struct _ -> 13 | Union _ -> 15 | Str _ -> 12
    | Enum _ -> 7       (* SPEC-GAP: §3.4 says 7 for an enum here; the layout says 30 *)
    | Flags _ -> 9 | Timestamp -> 5
    | Opt t -> leaf t
    | Bits n -> bits_kind n
    | _ -> kind_of t in
  let rec facts path (t : ty) =
    match t with
    | Opt p -> facts (path ^ seg '?' 35 0L) p
    | Arr (k, et) ->
      (match k with ACounted (mn, _) -> let b = Buffer.create 8 in u64 b (Int64.of_int mn); row 'C' path (Buffer.contents b) | _ -> ());
      let p = path ^ seg 'e' (leaf et) 0L in
      facts p et
    | Keyed (_, et) ->
      let p = path ^ seg 'e' (leaf et) 0L in
      facts p et
    | Int { ranged = true; signed; lo; hi; _ } ->
      let b = Buffer.create 16 in
      u32 b (kind_of t); Buffer.add_char b (if signed then '\001' else '\000'); u32 b 0;
      Buffer.add_string b (canon lo); Buffer.add_string b (canon hi);
      row 'R' path (Buffer.contents b)
    | I128 (lo, hi) ->
      let b = Buffer.create 16 in
      u32 b 18; Buffer.add_char b '\001'; u32 b 0;
      Buffer.add_string b (canon lo); Buffer.add_string b (canon hi);
      row 'R' path (Buffer.contents b)
    | Fixed { i; f; signed; lo; hi } ->
      let b = Buffer.create 16 in
      u32 b (kind_of t); Buffer.add_char b (if signed then '\001' else '\000'); u32 b f;
      Buffer.add_string b (canon lo); Buffer.add_string b (canon hi);
      row 'R' path (Buffer.contents b);
      let x = Buffer.create 9 in u32 x i; u32 x f; Buffer.add_char x (if signed then '\001' else '\000');
      row 'X' path (Buffer.contents x)
    | CF32 (mn, mx, r) ->
      let b = Buffer.create 20 in
      u32 b 10; u64 b (Int64.bits_of_float mn); u64 b (Int64.bits_of_float mx);
      row 'D' path (Buffer.contents b);
      let q = Buffer.create 8 in u64 q (Int64.bits_of_float r); row 'Q' path (Buffer.contents q)
    | Bits n -> let b = Buffer.create 4 in u32 b n; row 'B' path (Buffer.contents b)
    | Flags fl ->
      let b = Buffer.create 16 in
      u32 b fl.fl_width; u32 b (Array.length fl.fl_vars);
      Array.iter (fun n -> u64 b (fnv1a64 n)) fl.fl_vars;
      row 'F' path (Buffer.contents b)
    | Struct s ->
      Array.iter (fun (f : field_d) -> facts (path ^ seg 'f' (leaf f.f_ty) f.f_id) f.f_ty) s.s_fields
    | Union un ->
      Array.iter (fun arm -> match arm.a_ty with
          | None -> ()
          | Some at -> facts (path ^ seg 'a' (leaf at) arm.a_id) at) un.u_arms
    | _ -> () in
  facts (seg 't' 13 root.s_id) (Struct root);
  Buffer.contents out

(* ---- the lock, for the lineage hashes (SPEC-TABLES §2.10 The lock) ---- *)

type lineage = { wire : int64; record : int; lbytes : string; ldigest : string }

let hex_decode s =
  String.init (String.length s / 2) (fun i -> Char.chr (int_of_string ("0x" ^ String.sub s (2 * i) 2)))

let read_lock (path : string) : (string, lineage list) Hashtbl.t =
  let t = Hashtbl.create 16 in
  (match open_in path with
   | exception _ -> ()
   | ic ->
     let cur = ref "" in
     (try
        while true do
          let line = String.trim (input_line ic) in
          let words = String.split_on_char ' ' line |> List.filter (( <> ) "") in
          match words with
          | "fixed" :: "table" :: name :: _ -> cur := name
          | "table" :: name :: _ | "type" :: name :: _ -> cur := name
          | "lineage" :: rest when !cur <> "" ->
            let get k = List.find_map (fun w ->
                let kl = String.length k + 1 in
                if String.length w >= kl && String.sub w 0 kl = k ^ "=" then Some (String.sub w kl (String.length w - kl)) else None) rest in
            (match get "wire", get "record", get "bytes" with
             | Some w, Some r, Some b ->
               let e = { wire = Int64.of_string w; record = int_of_string r; lbytes = hex_decode b;
                         ldigest = (match get "digest" with Some d -> hex_decode d | None -> "") } in
               let prev = try Hashtbl.find t !cur with Not_found -> [] in
               Hashtbl.replace t !cur (prev @ [ e ])
             | _ -> ())
          | _ -> ()
        done
      with End_of_file -> close_in ic));
  t

(* ---- the record writer, §3.1 ---- *)

let check = Jsonw.check_scalar

let rec put (img : Bytes.t) off (t : ty) (v : value) what =
  match t, v with
  | Bool, VBool b -> Bytes.set_uint8 img off (if b then 1 else 0)
  | (Int _ | Bits _ | I128 _ | U128 | Fixed _ | Flags _ | Timestamp), VInt x ->
    check t v what;
    Bytes.blit_string (Big.to_le_bytes x (csize t)) 0 img off (csize t)
  | F32, VFloat x -> check t v what; Bytes.set_int32_le img off (Int32.bits_of_float x)
  | CF32 _, VFloat x -> check t v what; Bytes.set_int32_le img off (Int32.bits_of_float (if x = 0.0 then 0.0 else x))
  | F64, VFloat x -> check t v what; Bytes.set_int64_le img off (Int64.bits_of_float x)
  | Enum e, VEnum o ->
    check t v what;
    Bytes.blit_string (Big.to_le_bytes (Big.of_int o) (ord_width (Array.length e.e_vars))) 0 img off (csize t)
  | Struct s, VStruct fs ->
    let o = ref off in
    Array.iteri (fun i (f : field_d) -> put img !o f.f_ty fs.(i) (what ^ "." ^ f.f_name); o := !o + csize f.f_ty) s.s_fields
  | Arr (AFixed n, et), VArr xs ->
    if Array.length xs <> n then refuse ("invalid_value: count " ^ what);
    Array.iteri (fun i x -> put img (off + i * csize et) et x what) xs
  | Arr (ACounted (mn, mx), et), VArr xs ->
    let n = Array.length xs in
    if n < mn || n > mx then refuse ("invalid_value: count " ^ what);
    Bytes.set_int32_le img off (Int32.of_int n);
    Array.iteri (fun i x -> put img (off + 4 + i * csize et) et x what) xs
  | (Str _ | Bytes _), VStr s ->
    check t v what;
    Bytes.set_int32_le img off (Int32.of_int (String.length s));
    Bytes.blit_string s 0 img (off + 4) (String.length s)
  | Union un, VUnion (o, x) ->
    if o < 0 || o > Array.length un.u_arms then refuse ("invalid_value: tag " ^ what);
    let tw = ord_width (Array.length un.u_arms) in
    Bytes.blit_string (Big.to_le_bytes (Big.of_int o) tw) 0 img off tw;
    if o > 0 then (match un.u_arms.(o - 1).a_ty with Some at -> put img (off + tw) at x what | None -> ())
  | Opt t, VOpt None -> ignore t; Bytes.set_uint8 img off 0
  | Opt t, VOpt (Some x) -> Bytes.set_uint8 img off 1; put img (off + 1) t x what
  | Keyed (_, et), VKeyed xs -> Array.iteri (fun i x -> put img (off + i * csize et) et x what) xs
  | _ -> refuse ("invalid_value: shape " ^ what)

let own_hash (root : struct_d) = layout_hash (layout root) (digest root)

let save_records (root : struct_d) (vs : value list) : string =
  let lay = layout root in
  let h = layout_hash lay (digest root) in
  let size = csize (Struct root) in
  let buf = Buffer.create 256 in
  Buffer.add_char buf '\003';
  Buffer.add_string buf "\000\000\000\000\000\000\000";
  let b8 = Bytes.create 8 in Bytes.set_int64_le b8 0 h;
  let b4 = Bytes.create 4 in Bytes.set_int32_le b4 0 (Int32.of_int (String.length lay));
  Buffer.add_bytes buf b8; Buffer.add_bytes buf b4; Buffer.add_string buf lay;
  let recs = List.map (fun v -> let img = Bytes.make size '\000' in put img 0 (Struct root) v root.s_name; img) vs in
  List.iter (fun img -> Buffer.add_bytes buf b8; Buffer.add_bytes buf img) recs;
  Buffer.contents buf

(* ---- the identity read, §4: bounds and dead bytes checked, nothing clamped ---- *)

let rec get (r : string) off (t : ty) what : value =
  let zero_from a n = for k = a to a + n - 1 do if r.[k] <> '\000' then refuse ("wire_damaged: dead byte " ^ what) done in
  match t with
  | Bool -> (match r.[off] with '\000' -> VBool false | '\001' -> VBool true | _ -> refuse "wire_damaged: bool")
  | Int { lo; hi; signed; w; _ } ->
    let v = Big.of_le_bytes ~signed r off w in
    if Big.compare v lo < 0 || Big.compare v hi > 0 then refuse ("out_of_range: " ^ what);
    VInt v
  | Bits n ->
    let v = Big.of_le_bytes r off (csize t) in
    if Big.num_bits v > n then refuse ("out_of_range: " ^ what);
    VInt v
  | I128 (lo, hi) ->
    let v = Big.of_le_bytes ~signed:true r off 16 in
    if Big.compare v lo < 0 || Big.compare v hi > 0 then refuse ("out_of_range: " ^ what);
    VInt v
  | U128 -> VInt (Big.of_le_bytes r off 16)
  | Fixed { i; f; signed; lo; hi } ->
    let v = Big.of_le_bytes ~signed r off ((i + f) / 8) in
    if Big.compare v (Big.shift_left lo f) < 0 || Big.compare v (Big.shift_left hi f) > 0 then refuse ("out_of_range: " ^ what);
    VInt v
  | Flags _ -> VInt (Big.of_le_bytes r off 8)
  | Timestamp -> VInt (Big.of_le_bytes ~signed:true r off 8)
  | F32 | CF32 _ ->
    let x = Int32.float_of_bits (String.get_int32_le r off) in
    if not (Float.is_finite x) then refuse ("non_finite_float: " ^ what);
    (match t with
     | CF32 (mn, mx, _) ->
       let x = if x = 0.0 then 0.0 else x in
       if x < to_f32 mn || x > to_f32 mx then refuse ("out_of_range: " ^ what);
       VFloat x
     | _ -> VFloat x)
  | F64 ->
    let x = Int64.float_of_bits (String.get_int64_le r off) in
    if not (Float.is_finite x) then refuse ("non_finite_float: " ^ what);
    VFloat x
  | Enum e ->
    let o = Big.of_le_bytes r off (csize t) in
    if Big.compare o (Big.of_int (Array.length e.e_vars)) > 0 then refuse ("out_of_range: " ^ what);
    VEnum (Big.to_int o)
  | Struct s ->
    let o = ref off in
    VStruct (Array.map (fun (f : field_d) ->
        let v = get r !o f.f_ty (what ^ "." ^ f.f_name) in o := !o + csize f.f_ty; v) s.s_fields)
  | Arr (AFixed n, et) -> VArr (Array.init n (fun i -> get r (off + i * csize et) et what))
  | Arr (ACounted (mn, mx), et) ->
    let n = Int32.to_int (String.get_int32_le r off) in
    if n < 0 || n > mx then refuse ("count_over_bound: " ^ what);
    if n < mn then refuse ("count_over_bound: " ^ what);
    let es = csize et in
    zero_from (off + 4 + n * es) ((mx - n) * es);
    VArr (Array.init n (fun i -> get r (off + 4 + i * es) et what))
  | Str (Some cap) | Bytes cap ->
    let n = Int32.to_int (String.get_int32_le r off) in
    if n < 0 || n > cap then refuse ("length_over_bound: " ^ what);
    zero_from (off + 4 + n) (cap - n);
    let s = String.sub r (off + 4) n in
    (match t with Str _ -> if not (Num.is_text s) then refuse ("wire_damaged: text " ^ what) | _ -> ());
    VStr s
  | Union un ->
    let tw = ord_width (Array.length un.u_arms) in
    let o = Big.of_le_bytes r off tw in
    if Big.compare o (Big.of_int (Array.length un.u_arms)) > 0 then refuse ("out_of_range: " ^ what);
    let o = Big.to_int o in
    let widest = arms_widest un in
    if o = 0 then (zero_from (off + tw) widest; VUnion (0, VUnit))
    else begin
      match un.u_arms.(o - 1).a_ty with
      | None -> zero_from (off + tw) widest; VUnion (o, VUnit)
      | Some at ->
        let sz = csize at in
        zero_from (off + tw + sz) (widest - sz);
        VUnion (o, get r (off + tw) at what)
    end
  | Opt p ->
    (match r.[off] with
     | '\000' -> zero_from (off + 1) (csize p); VOpt None
     | '\001' -> VOpt (Some (get r (off + 1) p what))
     | _ -> refuse "wire_damaged: present byte")
  | Keyed (_, et) ->
    let n = Array.length (match t with Keyed (en, _) -> en.e_vars | _ -> [||]) in
    VKeyed (Array.init n (fun i -> get r (off + i * csize et) et what))
  | _ -> refuse "not a fixed construct"

let load_records (root : struct_d) (lineage : lineage list) (s : string) ~capacity : value list =
  let n = String.length s in
  if n = 0 then refuse "truncated";
  (match s.[0] with
   | '\003' -> ()
   | '\001' -> refuse "previous_form"
   | '\002' -> refuse "message_form_as_file"
   | _ -> refuse "newer_form");
  if n < 20 then refuse "truncated";
  for k = 1 to 7 do if s.[k] <> '\000' then refuse "reserved_not_zero" done;
  let l = Int32.to_int (String.get_int32_le s 16) in
  if l < 0 || 20 + l > n then refuse "layout_malformed";
  let h = String.get_int64_le s 8 in
  let lay = layout root in
  let own = layout_hash lay (digest root) in
  let size = csize (Struct root) in
  if h <> own then begin
    match List.find_opt (fun e -> e.wire = h) lineage with
    | None -> refuse "layout_unknown"
    | Some _ -> refuse "lineage_plan_not_carried"
  end;
  if String.sub s 20 l <> lay then refuse "layout_malformed";
  let rest = n - 20 - l in
  let rb = 8 + size in
  if rest mod rb <> 0 then refuse "truncated";
  let count = rest / rb in
  if count = 0 then refuse "no_records";
  if count > capacity then refuse "batch_too_large";
  let out = ref [] in
  for k = 0 to count - 1 do
    let at = 20 + l + k * rb in
    if String.get_int64_le s at <> h then refuse "no_layout";
    let r = String.sub s (at + 8) size in
    out := get r 0 (Struct root) root.s_name :: !out
  done;
  List.rev !out
