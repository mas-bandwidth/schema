(* Form 1, the variable form: SPEC-TABLES §3, §3.1, §3.2, §4, §22.1-§22.5. *)

open Model
open Wirecommon

exception Refuse of string
let refuse s = raise (Refuse s)

let node_table_id = -1L          (* 0xFFFFFFFFFFFFFFFF *)
let build_version_id = -2L
let vocabulary_id = -3L
let key_id = fnv1a64 "key"
let value_id = fnv1a64 "value"

(* ====================== the writer ====================== *)

type wstate = {
  mutable ids : int64 list;              (* reversed *)
  mutable nids : int;
  refs : (int64, int) Hashtbl.t;
  nodeidx : (int, int) Hashtbl.t;        (* node nid -> index *)
}

let intern st id =
  match Hashtbl.find_opt st.refs id with
  | Some r -> r
  | None ->
    st.nids <- st.nids + 1;
    st.ids <- id :: st.ids;
    Hashtbl.replace st.refs id st.nids;
    st.nids

let mark st = st.nids
let rollback st m =
  while st.nids > m do
    (match st.ids with
     | id :: rest -> Hashtbl.remove st.refs id; st.ids <- rest
     | [] -> ());
    st.nids <- st.nids - 1
  done

let guard_holds = Jsonw.guard_holds

let check = Jsonw.check_scalar

let float_eq_default (t : ty) a b =
  match t with
  | CF32 _ -> a = b
  | _ -> Int64.bits_of_float a = Int64.bits_of_float b

let rec scalar_bytes (t : ty) (v : value) what : string =
  match t, v with
  | Bool, VBool b -> if b then "\001" else "\000"
  | (Int _ | Bits _ | I128 _ | U128 | Fixed _ | Timestamp | Flags _), VInt x ->
    check t v what;
    Big.to_le_bytes x (scalar_width t)
  | F32, VFloat x ->
    check t v what;
    let b = Bytes.create 4 in Bytes.set_int32_le b 0 (Int32.bits_of_float x); Bytes.to_string b
  | CF32 _, VFloat x ->
    check t v what;
    let x = if x = 0.0 then 0.0 else x in
    let b = Bytes.create 4 in Bytes.set_int32_le b 0 (Int32.bits_of_float x); Bytes.to_string b
  | F64, VFloat x ->
    check t v what;
    let b = Bytes.create 8 in Bytes.set_int64_le b 0 (Int64.bits_of_float x); Bytes.to_string b
  | _ -> ignore scalar_bytes; refuse ("invalid_value: " ^ what)

let lenpre (s : string) = let b = Buffer.create (String.length s + 2) in leb_put b (String.length s); Buffer.add_string b s; Buffer.contents b
let leb_s v = let b = Buffer.create 4 in leb_put b v; Buffer.contents b

let is_scalar = function
  | Bool | Int _ | Bits _ | I128 _ | U128 | F32 | F64 | CF32 _ | Fixed _ | Timestamp | Flags _ -> true
  | _ -> false

(* does a value of [t] sit at its (element/zero) default [d]? used for elision *)
let rec at_default st (t : ty) (v : value) (d : value) : bool =
  match t, v, d with
  | (F32 | F64 | CF32 _), VFloat a, VFloat b -> float_eq_default t a b
  | _, _, _ when is_scalar t -> v = d
  | (Str _ | Bytes _), VStr a, VStr b -> a = b
  | Enum _, VEnum a, VEnum b -> a = b
  | Struct s, VStruct fs, _ ->
    let m = mark st in
    let body = struct_body st s fs "" in
    rollback st m;
    body = "\000"
  | Union _, VUnion (o, _), _ -> o = 0
  | (Ptr _ | Blob _), VPtr None, _ -> true
  | _ -> false

and struct_body st (s : struct_d) (fs : value array) what : string =
  let buf = Buffer.create 64 in
  fields_into st buf s fs what;
  Buffer.add_char buf '\000';
  Buffer.contents buf

and fields_into st buf (s : struct_d) (fs : value array) what =
  Array.iteri (fun i (f : field_d) ->
      if guard_holds fs f.f_guards then field st buf f.f_id f.f_ty fs.(i) f.f_default f.f_required (what ^ "." ^ f.f_name))
    s.s_fields

and elides st (t : ty) (v : value) (d : value) : bool =
  match t, v with
  | Opt _, VOpt None -> true
  | Opt _, VOpt (Some _) -> false
  | (Ptr _ | Blob _), VPtr None -> true
  | (Ptr _ | Blob _), VPtr (Some _) -> false
  | Arr (AFixed _, Struct _), _ -> false
  | Arr (AFixed _, et), VArr xs -> Array.for_all (fun x -> at_default st et x (zero_value et)) xs
  | Arr (ACounted (0, _), _), VArr xs | Arr (AOpen, _), VArr xs -> Array.length xs = 0
  | Arr (ACounted (mn, _), et), VArr xs ->
    Array.length xs = mn && Array.for_all (fun x -> at_default st et x (zero_value et)) xs
  | Keyed (_, et), VKeyed xs -> Array.for_all (fun x -> at_default st et x (zero_value et)) xs
  | Map _, VMap es -> Array.length es = 0
  | Union _, VUnion (o, _) -> o = 0
  | Struct _, _ -> at_default st t v d
  | _ -> at_default st t v d

and field st buf id (t : ty) (v : value) (d : value) required what =
  if required || not (elides st t v d) then begin
    let m = mark st in
    let r = intern st id in
    let payload = Buffer.create 16 in
    (try field_payload st payload t v what with e -> rollback st m; raise e);
    leb_put buf r;
    Buffer.add_char buf (Char.chr (kind_of t));
    Buffer.add_buffer buf payload
  end

(* the bytes a FIELD of [t] puts after its kind byte *)
and field_payload st buf (t : ty) (v : value) what =
  match t, v with
  | Opt t, VOpt (Some x) -> field_payload st buf t x what
  | _ when is_scalar t -> Buffer.add_string buf (scalar_bytes t v what)
  | Str _, VStr s -> check t v what; Buffer.add_string buf (lenpre s)
  | Bytes _, VStr s ->
    check t v what;
    let b = Buffer.create 8 in
    Buffer.add_char b '\006'; leb_put b (String.length s); Buffer.add_string b s;
    Buffer.add_string buf (lenpre (Buffer.contents b))
  | Struct s, VStruct fs -> Buffer.add_string buf (lenpre (struct_body st s fs what))
  | Enum e, VEnum o ->
    check t v what;
    if o = 0 then leb_put buf 0 else leb_put buf (intern st e.e_vars.(o - 1).v_id)
  | Union un, VUnion (o, x) -> union_payload st buf un o x what
  | Arr (k, et), VArr xs ->
    (match k with
     | ACounted (mn, mx) -> if Array.length xs < mn || Array.length xs > mx then refuse ("invalid_value: count " ^ what)
     | AFixed n -> if Array.length xs <> n then refuse ("invalid_value: count " ^ what)
     | AOpen -> ());
    let b = Buffer.create 16 in
    Buffer.add_char b (Char.chr (elem_kind et));
    leb_put b (Array.length xs);
    Array.iter (fun x -> elem st b et x what) xs;
    Buffer.add_string buf (lenpre (Buffer.contents b))
  | Keyed (en, et), VKeyed xs ->
    let b = Buffer.create 16 in
    Buffer.add_char b (Char.chr (elem_kind et));
    let present = ref [] in
    Array.iteri (fun i x -> if not (at_default st et x (zero_value et)) then present := i :: !present) xs;
    let present = List.rev !present in
    leb_put b (List.length present);
    List.iter (fun i ->
        leb_put b (intern st en.e_vars.(i).v_id);
        let eb = Buffer.create 8 in
        (match et with
         | Struct s -> (match xs.(i) with VStruct fs -> Buffer.add_string eb (struct_body st s fs what) | _ -> refuse "shape")
         | _ -> keyed_elem st eb et xs.(i) what);
        leb_put b (Buffer.length eb);
        Buffer.add_buffer b eb) present;
    Buffer.add_string buf (lenpre (Buffer.contents b))
  | Map (kt, vt), VMap es ->
    let es = Array.copy es in
    Array.sort (fun (a, _) (b, _) -> key_compare kt a b) es;
    Array.iteri (fun i (k, _) -> if i > 0 && key_compare kt (fst es.(i - 1)) k = 0 then refuse "duplicate map key") es;
    let b = Buffer.create 16 in
    Buffer.add_char b '\013';
    leb_put b (Array.length es);
    Array.iter (fun (k, x) ->
        let eb = Buffer.create 16 in
        field st eb key_id kt k (zero_value kt) false (what ^ ".key");
        (match vt, x with
         | Opt _, VOpt None -> ()
         | Opt _, VOpt (Some _) -> field st eb value_id vt x (zero_value vt) true (what ^ ".value")
         | _ -> field st eb value_id vt x (zero_value vt) false (what ^ ".value"));
        Buffer.add_char eb '\000';
        Buffer.add_string b (lenpre (Buffer.contents eb))) es;
    Buffer.add_string buf (lenpre (Buffer.contents b))
  | (Ptr _ | Blob _), VPtr None -> leb_put buf 0
  | (Ptr _ | Blob _), VPtr (Some n) ->
    (match Hashtbl.find_opt st.nodeidx n.nid with
     | Some i -> leb_put buf i
     | None -> refuse "unnumbered node")
  | _ -> refuse ("invalid_value: shape " ^ what)

and elem_kind (et : ty) =
  match et with
  | Opt (Str _) -> 13
  | Bits n -> bits_kind n
  | _ -> kind_of et

and elem st buf (et : ty) (x : value) what =
  match et, x with
  | Opt (Str _), VOpt None -> Buffer.add_string buf (lenpre "\000")
  | Opt (Str n), VOpt (Some s) ->
    let eb = Buffer.create 8 in
    field st eb value_id (Str n) s (VStr "") true what;
    Buffer.add_char eb '\000';
    Buffer.add_string buf (lenpre (Buffer.contents eb))
  | Union un, VUnion (o, y) -> union_payload st buf un o y what
  | _ -> field_payload st buf et x what

and keyed_elem st buf et x what =
  (* a keyed element is framed by its own L, so its bytes carry no inner length for scalars *)
  match et with
  | _ when is_scalar et -> Buffer.add_string buf (scalar_bytes et x what)
  | _ -> field_payload st buf et x what

and union_payload st buf (un : union_d) o x what =
  if o < 0 || o > Array.length un.u_arms then refuse ("invalid_value: tag " ^ what);
  if o = 0 then leb_put buf 0
  else begin
    let arm = un.u_arms.(o - 1) in
    leb_put buf (intern st arm.a_id);
    match arm.a_ty with
    | None -> Buffer.add_char buf '\032'; Buffer.add_char buf '\000'
    | Some at ->
      let k = kind_of at in
      Buffer.add_char buf (Char.chr k);
      let pb = Buffer.create 16 in
      field_payload st pb at x what;
      let p = Buffer.contents pb in
      if k = 12 || k = 13 || k = 14 || k = 16 then Buffer.add_string buf p
      else Buffer.add_string buf (lenpre p)
  end

(* §22.3: the node numbering *)
let number (root : struct_d) (v : value) : (node * ty) list * (int, int) Hashtbl.t =
  let idx = Hashtbl.create 16 in
  let order = ref [] in
  let next = ref 2 in
  let rec walk (t : ty) (v : value) open_ =
    match t, v with
    | Struct s, VStruct fs ->
      Array.iteri (fun i (f : field_d) -> if guard_holds fs f.f_guards then walk f.f_ty fs.(i) open_) s.s_fields
    | Opt t, VOpt (Some x) -> walk t x open_
    | (Arr (_, t) | Keyed (_, t)), (VArr xs | VKeyed xs) -> Array.iter (fun x -> walk t x open_) xs
    | Map (kt, vt), VMap es ->
      let es = Array.copy es in
      Array.sort (fun (a, _) (b, _) -> key_compare kt a b) es;
      Array.iter (fun (_, x) -> walk vt x open_) es
    | Union un, VUnion (o, x) when o > 0 ->
      (match un.u_arms.(o - 1).a_ty with Some at -> walk at x open_ | None -> ())
    | Ptr s, VPtr (Some n) ->
      if List.mem n.nid open_ then refuse "data_cycle";
      if not (Hashtbl.mem idx n.nid) then begin
        Hashtbl.replace idx n.nid !next; incr next;
        order := (n, Struct s) :: !order;
        walk (Struct s) n.nv (n.nid :: open_)
      end
    | Blob b, VPtr (Some n) ->
      if Hashtbl.mem idx n.nid then refuse "blob_shared";
      Hashtbl.replace idx n.nid !next; incr next;
      order := (n, Blob b) :: !order
    | _ -> () in
  walk (Struct root) v [];
  (List.rev !order, idx)

let save (root : struct_d) (v : value) : string =
  let fs = match v with VStruct fs -> fs | _ -> refuse "root" in
  let nodes, idx = number root v in
  let st = { ids = []; nids = 0; refs = Hashtbl.create 64; nodeidx = idx } in
  let buf = Buffer.create 256 in
  Buffer.add_char buf '\001';
  fields_into st buf root fs root.s_name;
  if nodes <> [] then begin
    let r = intern st node_table_id in
    let p = Buffer.create 64 in
    leb_put p (List.length nodes);
    List.iter (fun ((n : node), t) ->
        match t, n.nv with
        | Struct s, VStruct nfs ->
          leb_put p (intern st s.s_id);
          Buffer.add_string p (lenpre (struct_body st s nfs s.s_name))
        | Blob is_str, VStr bytes ->
          leb_put p (intern st (fnv1a64 (if is_str then "string" else "bytes")));
          if is_str && not (Num.is_text bytes) then refuse "invalid_value: blob text";
          Buffer.add_string p (lenpre bytes)
        | _ -> refuse "node shape") nodes;
    leb_put buf r;
    Buffer.add_char buf '\012';
    Buffer.add_string buf (lenpre (Buffer.contents p))
  end;
  Buffer.add_char buf '\000';
  if st.nids > 4096 then refuse "id table past 4096";
  List.iter (fun id -> let b = Bytes.create 8 in Bytes.set_int64_le b 0 id; Buffer.add_bytes buf b) (List.rev st.ids);
  let b = Bytes.create 8 in Bytes.set_int64_le b 0 (Int64.of_int st.nids); Buffer.add_bytes buf b;
  Buffer.contents buf

(* ====================== the reader ====================== *)

type rstate = {
  s : string;
  ids : int64 array;
  mutable seen : int;                    (* distinct references met so far *)
  mutable unknown : int;
  mutable widened : int;
  mutable skipped : int list;             (* node indexes named by pointers this reader skipped *)
  mutable pending : (node * ty * int) list;  (* pointer slots to resolve: holder, slot type, index *)
  nodes : (int, node * ty) Hashtbl.t;     (* index -> decoded record *)
  types : (int64, ty) Hashtbl.t;          (* record type id -> what it decodes as *)
}

type cur = { mutable p : int; lim : int }

let byte rs c = if c.p >= c.lim then refuse "wire_damaged: short" else (let b = Char.code rs.s.[c.p] in c.p <- c.p + 1; b)

let leb rs c =
  let v = ref 0 and shift = ref 0 and fin = ref false and n = ref 0 in
  while not !fin do
    let b = byte rs c in
    incr n;
    if !n > 10 then refuse "wire_damaged: leb too long";
    if !n = 10 && b > 1 then refuse "wire_damaged: leb past 64 bits";
    if !shift >= 63 && (b land 0x7f) > 0 then refuse "wire_damaged: leb too large for this port";
    v := !v lor ((b land 0x7f) lsl !shift);
    shift := !shift + 7;
    if b land 0x80 = 0 then begin
      fin := true;
      if b = 0 && !n > 1 then refuse "wire_damaged: non-canonical leb"
    end
  done;
  !v

let take rs c n = if n < 0 || c.p + n > c.lim then refuse "wire_damaged: length past body" else (let s = String.sub rs.s c.p n in c.p <- c.p + n; s)

let sub rs c n = if n < 0 || c.p + n > c.lim then refuse "wire_damaged: length past body" else (let r = { p = c.p; lim = c.p + n } in c.p <- c.p + n; r)

(* a reference, held to the table and to first-use order *)
let ref_ rs c ~required =
  let r = leb rs c in
  if r = 0 then (if required then refuse "wire_damaged: zero reference where an id is required"; 0)
  else begin
    if r > Array.length rs.ids then refuse "wire_damaged: reference past table";
    if r > rs.seen then begin
      if r <> rs.seen + 1 then refuse "noncanonical: id table not in first-use order";
      rs.seen <- r
    end;
    r
  end

let id_of rs r = rs.ids.(r - 1)

let array_elem_kinds = [ 1; 2; 3; 4; 5; 6; 7; 8; 9; 10; 11; 12; 13; 15; 17; 18; 19; 20; 21; 22; 23; 24; 25; 26; 27; 28; 29; 30 ]
let keyed_elem_kinds = [ 1; 2; 3; 4; 5; 6; 7; 8; 9; 10; 11; 13; 18; 19; 20; 21; 22; 23; 24; 25; 26; 27; 28; 29; 30 ]
let known_kinds = [ 1; 2; 3; 4; 5; 6; 7; 8; 9; 10; 11; 12; 13; 14; 15; 16; 17; 18; 19; 20; 21; 22; 23; 24; 25; 26; 27; 28; 29; 30; 31; 32; 36 ]

let max_depth = 128

(* walk a payload this reader cannot name, holding it to every wire-only rule *)
let rec skip rs c kind depth =
  if not (List.mem kind known_kinds) then refuse "wire_damaged: kind unknown";
  match kind with
  | 31 -> let n = leb rs c in ignore (take rs c n)
  | 32 -> let n = leb rs c in if n <> 0 then refuse "wire_damaged: kind 32 with L"
  | 12 -> let n = leb rs c in let s = take rs c n in if not (Num.is_text s) then refuse "wire_damaged: text"
  | 13 -> let n = leb rs c in let b = sub rs c n in skip_body rs b (depth + 1)
  | 14 -> let n = leb rs c in let b = sub rs c n in skip_array rs b (depth + 1) false
  | 16 -> let n = leb rs c in let b = sub rs c n in skip_array rs b (depth + 1) true
  | 15 -> skip_union rs c depth
  | 17 -> let i = leb rs c in if i <> 0 then rs.skipped <- i :: rs.skipped
  | 30 -> ignore (ref_ rs c ~required:false)
  | 1 -> let b = byte rs c in if b > 1 then refuse "wire_damaged: bool"
  | 10 -> let s = take rs c 4 in
    if not (Float.is_finite (Int32.float_of_bits (String.get_int32_le s 0))) then refuse "non_finite_float"
  | 11 -> let s = take rs c 8 in
    if not (Float.is_finite (Int64.float_of_bits (String.get_int64_le s 0))) then refuse "non_finite_float"
  | k -> ignore (take rs c (kind_width k))

and skip_body rs c depth =
  if depth > max_depth then refuse "wire_damaged: depth";
  let seen = Hashtbl.create 8 in
  let fin = ref false in
  while not !fin do
    let r = ref_ rs c ~required:false in
    if r = 0 then (if c.p <> c.lim then refuse "wire_damaged: early terminator"; fin := true)
    else begin
      let id = id_of rs r in
      if id = node_table_id || id = build_version_id || id = vocabulary_id then refuse "wire_damaged: reserved id in nested body";
      if Hashtbl.mem seen id then refuse "wire_damaged: field twice";
      Hashtbl.replace seen id ();
      let k = byte rs c in
      skip rs c k depth
    end
  done

and skip_array rs c depth keyed =
  if depth > max_depth then refuse "wire_damaged: depth";
  if c.lim - c.p < 2 then refuse "wire_damaged: array header";
  let ek = byte rs c in
  if not (List.mem ek (if keyed then keyed_elem_kinds else array_elem_kinds)) then refuse "wire_damaged: element kind";
  let n = leb rs c in
  if n > c.lim - c.p then refuse "wire_damaged: count past body";
  for _ = 1 to n do
    if keyed then begin
      ignore (ref_ rs c ~required:true);
      let l = leb rs c in
      let e = sub rs c l in
      skip_keyed_elem rs e ek depth;
      if e.p <> e.lim then refuse "wire_damaged: keyed element length"
    end else skip_elem rs c ek depth
  done;
  if c.p <> c.lim then refuse "wire_damaged: bytes after elements"

and skip_elem rs c ek depth =
  match ek with
  | 13 -> let n = leb rs c in let b = sub rs c n in skip_body rs b (depth + 1)
  | 15 -> skip_union rs c depth
  | _ -> skip rs c ek depth

and skip_keyed_elem rs c ek depth =
  match ek with
  | 13 -> skip_body rs c (depth + 1)
  | 30 -> ignore (ref_ rs c ~required:false)
  | _ -> skip rs c ek depth

and skip_union rs c depth =
  let r = ref_ rs c ~required:false in
  if r <> 0 then begin
    let k = byte rs c in
    if not (List.mem k known_kinds) then refuse "wire_damaged: kind unknown";
    let l = leb rs c in
    let a = sub rs c l in
    skip_arm rs a k depth;
    if a.p <> a.lim then refuse "wire_damaged: arm length"
  end

and skip_arm rs a k depth =
  match k with
  | 13 -> skip_body rs a (depth + 1)
  | 14 -> skip_array rs a (depth + 1) false
  | 16 -> skip_array rs a (depth + 1) true
  | 12 -> let s = take rs a (a.lim - a.p) in if not (Num.is_text s) then refuse "wire_damaged: text"
  | 31 -> a.p <- a.lim
  | 32 -> ()
  | 15 -> skip_union rs a (depth + 1)
  | 17 -> ignore (leb rs a)
  | 30 -> ignore (ref_ rs a ~required:false)
  | k -> skip rs a k depth

(* widening ladders, §4 *)
let ladder_pos k =
  match k with
  | 2 -> Some (0, 0) | 3 -> Some (0, 1) | 4 -> Some (0, 2) | 5 -> Some (0, 3) | 18 -> Some (0, 4)
  | 6 -> Some (1, 0) | 7 -> Some (1, 1) | 8 -> Some (1, 2) | 9 -> Some (1, 3) | 19 -> Some (1, 4)
  | _ -> None

(* `Same | `Widen | `Mismatch *)
let kind_pair ~reader ~wire =
  if reader = wire then `Same
  else if reader = 11 && wire = 10 then `Widen
  else match ladder_pos reader, ladder_pos wire with
    | Some (a, x), Some (b, y) when a = b && y < x -> `Widen
    | _ -> `Mismatch

let in_range lo hi v what = if Big.compare v lo < 0 || Big.compare v hi > 0 then refuse ("out_of_range: " ^ what)

(* read a scalar of reader type t from a wire kind wk *)
let read_scalar rs c (t : ty) (wk : int) what : value =
  let rk = kind_of t in
  (match kind_pair ~reader:rk ~wire:wk with
   | `Same -> ()
   | `Widen -> rs.widened <- rs.widened + 1
   | `Mismatch -> refuse ("kind_mismatch: " ^ what));
  let w = kind_width wk in
  let raw = take rs c w in
  match t with
  | Bool -> (match raw with "\000" -> VBool false | "\001" -> VBool true | _ -> refuse "wire_damaged: bool")
  | F32 | CF32 _ ->
    let x = Int32.float_of_bits (String.get_int32_le raw 0) in
    if not (Float.is_finite x) then refuse ("non_finite_float: " ^ what);
    (match t with
     | CF32 (mn, mx, _) ->
       let x = if x = 0.0 then 0.0 else x in
       if x < to_f32 mn || x > to_f32 mx then refuse ("out_of_range: " ^ what);
       VFloat x
     | _ -> VFloat x)
  | F64 ->
    let x = if wk = 10 then Int32.float_of_bits (String.get_int32_le raw 0) else Int64.float_of_bits (String.get_int64_le raw 0) in
    if not (Float.is_finite x) then refuse ("non_finite_float: " ^ what);
    VFloat x
  | _ ->
    let v = Big.of_le_bytes ~signed:(kind_signed wk) raw 0 w in
    (match t with
     | Int { lo; hi; _ } -> in_range lo hi v what
     | Bits n -> in_range Big.zero (Big.pred (Big.pow2 n)) v what
     | I128 (lo, hi) -> in_range lo hi v what
     | Fixed { f; lo; hi; _ } -> in_range (Big.shift_left lo f) (Big.shift_left hi f) v what
     | _ -> ());
    VInt v

let rec read_body rs c (s : struct_d) depth ~root : value =
  if depth > max_depth then refuse "wire_damaged: depth";
  let fs = Array.map (fun (f : field_d) -> f.f_default) s.s_fields in
  let seen = Hashtbl.create 8 in
  let fin = ref false in
  let node_table_seen = ref false in
  while not !fin do
    let r = ref_ rs c ~required:false in
    if r = 0 then (if c.p <> c.lim then refuse "wire_damaged: early terminator"; fin := true)
    else begin
      if !node_table_seen then refuse "noncanonical: node table not last";
      let id = id_of rs r in
      if Hashtbl.mem seen id then refuse "wire_damaged: field twice";
      Hashtbl.replace seen id ();
      let k = byte rs c in
      if id = node_table_id then begin
        if not root then refuse "wire_damaged: node table in nested body";
        if k <> 12 then refuse "wire_damaged: node table kind";
        read_node_table rs c;
        node_table_seen := true
      end else if id = build_version_id || id = vocabulary_id then refuse "wire_damaged: reserved id"
      else begin
        let rec find i = if i >= Array.length s.s_fields then -1 else if s.s_fields.(i).f_id = id then i else find (i + 1) in
        let i = find 0 in
        if i < 0 then begin
          if not (List.mem k known_kinds) then refuse "wire_damaged: kind unknown";
          rs.unknown <- rs.unknown + 1;
          skip rs c k depth
        end else begin
          let f = s.s_fields.(i) in
          fs.(i) <- read_field rs c f.f_ty k depth (s.s_name ^ "." ^ f.f_name) (Some (fs, i))
        end
      end
    end
  done;
  VStruct fs

and read_field rs c (t : ty) (k : int) depth what (slot : (value array * int) option) : value =
  match t with
  | Opt t -> VOpt (Some (read_field rs c t k depth what slot))
  | Bool | Int _ | Bits _ | I128 _ | U128 | F32 | F64 | CF32 _ | Fixed _ | Timestamp | Flags _ ->
    if not (List.mem k known_kinds) then refuse "wire_damaged: kind unknown";
    if kind_width k = 0 then refuse ("kind_mismatch: " ^ what);
    read_scalar rs c t k what
  | _ ->
    if not (List.mem k known_kinds) then refuse "wire_damaged: kind unknown";
    let rk = kind_of t in
    if rk <> k then begin
      (* a kind-31/32 at a named position, or any other kind: mismatch; but damage first *)
      refuse ("kind_mismatch: " ^ what)
    end;
    match t with
    | Str n ->
      let l = leb rs c in
      let s = take rs c l in
      if not (Num.is_text s) then refuse "wire_damaged: text";
      (match n with Some n when l > n -> refuse ("length_over_bound: " ^ what) | _ -> ());
      VStr s
    | Struct s ->
      let l = leb rs c in
      let b = sub rs c l in
      read_body rs b s (depth + 1) ~root:false
    | Enum e -> read_enum rs c e
    | Union un -> read_union rs c un depth what
    | Bytes n ->
      let l = leb rs c in
      let b = sub rs c l in
      let v = read_array_body rs b (Int { w = 1; signed = false; lo = Big.zero; hi = Big.of_int 255; ranged = false }) (ACounted (0, n)) (depth + 1) what in
      (match v with
       | VArr xs -> VStr (String.init (Array.length xs) (fun i -> match xs.(i) with VInt x -> Char.chr (Big.to_int x) | _ -> '\000'))
       | _ -> assert false)
    | Arr (ak, et) ->
      let l = leb rs c in
      let b = sub rs c l in
      read_array_body rs b et ak (depth + 1) what
    | Keyed (en, et) ->
      let l = leb rs c in
      let b = sub rs c l in
      read_keyed rs b en et (depth + 1) what
    | Map (kt, vt) ->
      let l = leb rs c in
      let b = sub rs c l in
      read_map rs b kt vt (depth + 1) what
    | Ptr _ | Blob _ ->
      let i = leb rs c in
      let holder = { nv = VUnit; nid = - 2 - List.length rs.pending } in
      if i <> 0 then rs.pending <- (holder, t, i) :: rs.pending;
      if i = 0 then VPtr None else VPtr (Some holder)
    | _ -> refuse ("kind_mismatch: " ^ what)

and read_enum rs c (e : enum_d) : value =
  let r = ref_ rs c ~required:false in
  if r = 0 then VEnum 0
  else begin
    let id = id_of rs r in
    let rec find i = if i >= Array.length e.e_vars then 0 else if e.e_vars.(i).v_id = id then i + 1 else find (i + 1) in
    let o = find 0 in
    if o = 0 then rs.unknown <- rs.unknown + 1;
    VEnum o
  end

and read_union rs c (un : union_d) depth what : value =
  let r = ref_ rs c ~required:false in
  if r = 0 then VUnion (0, VUnit)
  else begin
    let id = id_of rs r in
    let k = byte rs c in
    if not (List.mem k known_kinds) then refuse "wire_damaged: kind unknown";
    let l = leb rs c in
    let a = sub rs c l in
    let rec find i = if i >= Array.length un.u_arms then 0 else if un.u_arms.(i).a_id = id then i + 1 else find (i + 1) in
    let o = find 0 in
    if o = 0 then begin
      rs.unknown <- rs.unknown + 1;
      skip_arm rs a k depth;
      if a.p <> a.lim then refuse "wire_damaged: arm length";
      VUnion (0, VUnit)
    end else begin
      let arm = un.u_arms.(o - 1) in
      let v =
        match arm.a_ty with
        | None ->
          if k <> 32 then refuse ("kind_mismatch: arm " ^ what);
          if l <> 0 then refuse "wire_damaged: kind 32 with L";
          VUnit
        | Some at ->
          if k = 32 then refuse ("kind_mismatch: arm " ^ what);
          read_arm rs a at k depth what in
      if a.p <> a.lim then refuse "wire_damaged: arm length";
      VUnion (o, v)
    end
  end

and read_arm rs a (at : ty) k depth what : value =
  match at with
  | Struct s ->
    if k <> 13 then refuse ("kind_mismatch: arm " ^ what);
    read_body rs a s (depth + 1) ~root:false
  | Str n ->
    if k <> 12 then refuse ("kind_mismatch: arm " ^ what);
    let s = take rs a (a.lim - a.p) in
    if not (Num.is_text s) then refuse "wire_damaged: text";
    (match n with Some n when String.length s > n -> refuse ("length_over_bound: " ^ what) | _ -> ());
    VStr s
  | Arr _ | Bytes _ ->
    if k <> 14 then refuse ("kind_mismatch: arm " ^ what);
    (* the array body sits directly under the arm's L *)
    (match at with
     | Arr (ak, et) -> read_array_body rs a et ak (depth + 1) what
     | Bytes n ->
       (match read_array_body rs a (Int { w = 1; signed = false; lo = Big.zero; hi = Big.of_int 255; ranged = false }) (ACounted (0, n)) (depth + 1) what with
        | VArr xs -> VStr (String.init (Array.length xs) (fun i -> match xs.(i) with VInt x -> Char.chr (Big.to_int x) | _ -> '\000'))
        | _ -> assert false)
     | _ -> assert false)
  | Union un ->
    if k <> 15 then refuse ("kind_mismatch: arm " ^ what);
    read_union rs a un (depth + 1) what
  | Enum e ->
    if k <> 30 then refuse ("kind_mismatch: arm " ^ what);
    read_enum rs a e
  | Ptr _ | Blob _ ->
    if k <> 17 then refuse ("kind_mismatch: arm " ^ what);
    read_field rs a at 17 depth what None
  | _ ->
    if kind_width k = 0 then refuse ("kind_mismatch: arm " ^ what);
    if a.lim - a.p <> kind_width k then refuse "wire_damaged: arm length";
    read_scalar rs a at k what

and elem_read_kind (et : ty) = elem_kind et

and read_array_body rs c (et : ty) (ak : arrk) depth what : value =
  if depth > max_depth then refuse "wire_damaged: depth";
  if c.lim - c.p < 2 then refuse "wire_damaged: array header";
  let ek = byte rs c in
  if not (List.mem ek array_elem_kinds) then refuse "wire_damaged: element kind";
  let n = leb rs c in
  if n > c.lim - c.p then refuse "wire_damaged: count past body";
  let rk = elem_kind et in
  let widen = ref false in
  (match kind_pair ~reader:rk ~wire:ek with
   | `Same -> ()
   | `Widen when is_scalar et -> widen := true
   | _ -> refuse ("kind_mismatch: element " ^ what));
  let xs = Array.init n (fun _ ->
      match et with
      | Opt (Str m) ->
        let l = leb rs c in
        let b = sub rs c l in
        (* a body holding `value` under kind 12, or the terminator alone *)
        let r = ref_ rs b ~required:false in
        if r = 0 then (if b.p <> b.lim then refuse "wire_damaged: early terminator"; VOpt None)
        else begin
          let id = id_of rs r in
          let k = byte rs b in
          if id <> value_id || k <> 12 then refuse ("kind_mismatch: element " ^ what);
          let v = read_field rs b (Str m) 12 depth what None in
          let r2 = ref_ rs b ~required:false in
          if r2 <> 0 || b.p <> b.lim then refuse "wire_damaged: element body";
          VOpt (Some v)
        end
      | Struct s ->
        let l = leb rs c in
        let b = sub rs c l in
        read_body rs b s (depth + 1) ~root:false
      | Union un -> read_union rs c un depth what
      | Enum e -> read_enum rs c e
      | Str _ | Ptr _ | Blob _ -> read_field rs c et ek depth what None
      | _ -> read_scalar rs c et ek what) in
  if !widen then rs.widened <- rs.widened + 1;
  if c.p <> c.lim then refuse "wire_damaged: bytes after elements";
  (match ak with
   | AFixed m ->
     if n > m then refuse ("count_over_bound: " ^ what);
     VArr (Array.init m (fun i -> if i < n then xs.(i) else zero_value et))
   | ACounted (mn, mx) ->
     if n > mx || n < mn then refuse ("count_over_bound: " ^ what);
     VArr xs
   | AOpen -> VArr xs)

and read_keyed rs c (en : enum_d) (et : ty) depth what : value =
  if depth > max_depth then refuse "wire_damaged: depth";
  if c.lim - c.p < 2 then refuse "wire_damaged: array header";
  let ek = byte rs c in
  if not (List.mem ek keyed_elem_kinds) then refuse "wire_damaged: element kind";
  let n = leb rs c in
  if n > c.lim - c.p then refuse "wire_damaged: count past body";
  let rk = elem_kind et in
  (match kind_pair ~reader:rk ~wire:ek with
   | `Same -> ()
   | `Widen when is_scalar et -> rs.widened <- rs.widened + 1
   | _ -> refuse ("kind_mismatch: element " ^ what));
  let slots = Array.init (Array.length en.e_vars) (fun _ -> zero_value et) in
  let seen = Hashtbl.create 8 in
  for _ = 1 to n do
    let r = ref_ rs c ~required:true in
    let id = id_of rs r in
    if Hashtbl.mem seen id then refuse "wire_damaged: keyed slot twice";
    Hashtbl.replace seen id ();
    let l = leb rs c in
    let e = sub rs c l in
    let rec find i = if i >= Array.length en.e_vars then -1 else if en.e_vars.(i).v_id = id then i else find (i + 1) in
    let i = find 0 in
    if i < 0 then begin
      rs.unknown <- rs.unknown + 1;
      skip_keyed_elem rs e ek depth
    end else begin
      slots.(i) <-
        (match et with
         | Struct s -> read_body rs e s (depth + 1) ~root:false
         | Enum en2 -> read_enum rs e en2
         | _ -> read_scalar rs e et ek what)
    end;
    if e.p <> e.lim then refuse "wire_damaged: keyed element length"
  done;
  if c.p <> c.lim then refuse "wire_damaged: bytes after elements";
  VKeyed slots

and read_map rs c (kt : ty) (vt : ty) depth what : value =
  if depth > max_depth then refuse "wire_damaged: depth";
  if c.lim - c.p < 2 then refuse "wire_damaged: array header";
  let ek = byte rs c in
  if not (List.mem ek array_elem_kinds) then refuse "wire_damaged: element kind";
  if ek <> 13 then refuse ("kind_mismatch: map " ^ what);
  let n = leb rs c in
  if n > c.lim - c.p then refuse "wire_damaged: count past body";
  let es = Array.init n (fun _ ->
      let l = leb rs c in
      let b = sub rs c l in
      (* an entry body: fields `key` and `value` *)
      let key = ref (zero_value kt) and value = ref (match vt with Opt _ -> VOpt None | _ -> zero_value vt) in
      let seen = Hashtbl.create 2 in
      let fin = ref false in
      while not !fin do
        let r = ref_ rs b ~required:false in
        if r = 0 then (if b.p <> b.lim then refuse "wire_damaged: early terminator"; fin := true)
        else begin
          let id = id_of rs r in
          if Hashtbl.mem seen id then refuse "wire_damaged: field twice";
          Hashtbl.replace seen id ();
          let k = byte rs b in
          if id = key_id then key := read_field rs b kt k (depth + 1) (what ^ ".key") None
          else if id = value_id then value := read_field rs b vt k (depth + 1) (what ^ ".value") None
          else if id = node_table_id || id = build_version_id || id = vocabulary_id then refuse "wire_damaged: reserved id"
          else (rs.unknown <- rs.unknown + 1; skip rs b k (depth + 1))
        end
      done;
      (!key, !value)) in
  if c.p <> c.lim then refuse "wire_damaged: bytes after elements";
  Array.iteri (fun i (k, _) -> if i > 0 && key_compare kt (fst es.(i - 1)) k >= 0 then refuse "wire_damaged: map key order") es;
  VMap es


(* §3.1: records decode in byte order (their type id names what they are), so
   the id table's first-use order is checked in the order the bytes run *)
and read_node_table rs c =
  let l = leb rs c in
  let b = sub rs c l in
  let count = leb rs b in
  if count = 0 then refuse "noncanonical: empty node table";
  if count > b.lim - b.p then refuse "wire_damaged: node count";
  for k = 1 to count do
    let r = ref_ rs b ~required:true in
    let id = id_of rs r in
    let l = leb rs b in
    let body = sub rs b l in
    match Hashtbl.find_opt rs.types id with
    | Some (Struct st as t) ->
      let nd = { nv = VUnit; nid = k + 1 } in
      nd.nv <- read_body rs body st 0 ~root:false;
      Hashtbl.replace rs.nodes (k + 1) (nd, t)
    | Some (Blob is_str as t) ->
      let bs = String.sub rs.s body.p (body.lim - body.p) in
      if is_str && not (Num.is_text bs) then refuse "wire_damaged: text";
      Hashtbl.replace rs.nodes (k + 1) ({ nv = VStr bs; nid = k + 1 }, t)
    | _ ->
      rs.unknown <- rs.unknown + 1;
      skip_body rs body 0;
      Hashtbl.replace rs.nodes (k + 1) ({ nv = VUnit; nid = k + 1 }, Bool)
  done;
  if b.p <> b.lim then refuse "wire_damaged: node table bytes left"

let holder_target : (int, node) Hashtbl.t = Hashtbl.create 16

let unit_types (u : unit_t) =
  let t = Hashtbl.create 16 in
  Hashtbl.iter (fun _ (s : struct_d) -> if s.s_table then Hashtbl.replace t s.s_id (Struct s)) u.structs;
  Hashtbl.replace t (fnv1a64 "bytes") (Blob false);
  Hashtbl.replace t (fnv1a64 "string") (Blob true);
  t

let load (u : unit_t) (root : struct_d) (s : string) : value * int * int =
  Hashtbl.reset holder_target;
  let n = String.length s in
  if n = 0 then refuse "wire_damaged: empty";
  (match s.[0] with
   | '\001' -> ()
   | '\002' -> refuse "message_form_as_file"
   | '\003' -> refuse "newer_form"
   | _ -> refuse "newer_form");
  if n < 9 then refuse "wire_damaged: short";
  let count = String.get_int64_le s (n - 8) in
  if Int64.compare count 0L < 0 || Int64.compare count 4096L > 0 then refuse "wire_damaged: id count";
  let count = Int64.to_int count in
  if 8 * count + 8 > n - 1 then refuse "wire_damaged: id table past front";
  let tstart = n - 8 - 8 * count in
  let ids = Array.init count (fun i -> String.get_int64_le s (tstart + 8 * i)) in
  let dup = Hashtbl.create 16 in
  Array.iter (fun id -> if Hashtbl.mem dup id then refuse "wire_damaged: id twice"; Hashtbl.replace dup id ()) ids;
  let rs = { s; ids; seen = 0; unknown = 0; widened = 0; skipped = []; pending = []; nodes = Hashtbl.create 16; types = unit_types u } in
  let c = { p = 1; lim = tstart } in
  let v = read_body rs c root 0 ~root:true in
  if c.p <> c.lim then refuse "wire_damaged: bytes before id table";
  if rs.seen <> count then refuse "noncanonical: unused id-table entry";
  (* resolve every slot to its record *)
  let reached = Hashtbl.create 16 in
  let null_holders = Hashtbl.create 4 in
  List.iter (fun (h, t, i) ->
      if i = 1 then refuse "data_cycle: pointer to root";
      match Hashtbl.find_opt rs.nodes i with
      | None -> refuse "wire_damaged: index past node table"
      | Some (_, Bool) ->
        (* a record whose type this reader cannot name: every pointer naming it reads null (§3.1) *)
        Hashtbl.replace reached i ();
        Hashtbl.replace null_holders h.nid ()
      | Some (nd, t2) ->
        let ok = match t, t2 with Ptr x, Struct y -> x == y | Blob x, Blob y -> x = y | _ -> false in
        if not ok then refuse "wire_damaged: wrong-type pointer target";
        (match t with Blob _ -> if Hashtbl.mem reached i then refuse "blob_shared" | _ -> ());
        Hashtbl.replace reached i ();
        Hashtbl.replace holder_target h.nid nd) rs.pending;
  List.iter (fun i ->
      if i = 1 then refuse "data_cycle: pointer to root";
      if not (Hashtbl.mem rs.nodes i) then refuse "wire_damaged: index past node table";
      Hashtbl.replace reached i ()) rs.skipped;
  Hashtbl.iter (fun i _ -> if not (Hashtbl.mem reached i) then refuse "wire_damaged: node no pointer reaches") rs.nodes;
  (* splice holders, then check the graph for cycles from the root *)
  let fixed = Hashtbl.create 16 in
  let rec fix v =
    match v with
    | VPtr (Some h) when h.nid < 0 && Hashtbl.mem null_holders h.nid -> VPtr None
    | VPtr (Some h) when h.nid < 0 ->
      let t = Hashtbl.find holder_target h.nid in
      if not (Hashtbl.mem fixed t.nid) then (Hashtbl.replace fixed t.nid (); t.nv <- fix t.nv);
      VPtr (Some t)
    | VStruct fs -> VStruct (Array.map fix fs)
    | VOpt (Some x) -> VOpt (Some (fix x))
    | VArr xs -> VArr (Array.map fix xs)
    | VKeyed xs -> VKeyed (Array.map fix xs)
    | VMap es -> VMap (Array.map (fun (k, x) -> (k, fix x)) es)
    | VUnion (o, x) -> VUnion (o, fix x)
    | _ -> v in
  let v = fix v in
  let rec walk v open_ done_ =
    match v with
    | VPtr (Some nd) ->
      if List.mem nd.nid open_ then refuse "data_cycle";
      if not (Hashtbl.mem done_ nd.nid) then (Hashtbl.replace done_ nd.nid (); walk nd.nv (nd.nid :: open_) done_)
    | VStruct xs | VArr xs | VKeyed xs -> Array.iter (fun x -> walk x open_ done_) xs
    | VOpt (Some x) | VUnion (_, x) -> walk x open_ done_
    | VMap es -> Array.iter (fun (_, x) -> walk x open_ done_) es
    | _ -> () in
  walk v [] (Hashtbl.create 16);
  (v, rs.unknown, rs.widened)
