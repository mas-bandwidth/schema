(* The canonical text writer, SPEC-TABLES §16.14 W0-W14. *)

open Model

exception Refuse of string

let refuse s = raise (Refuse s)

let check_int_range lo hi v what =
  if Big.compare v lo < 0 || Big.compare v hi > 0 then refuse ("invalid_value: " ^ what)

(* the domain check every writer makes (SPEC.md §5.1; §16.3) on one scalar *)
let rec check_scalar (t : ty) (v : value) what =
  match t, v with
  | Bool, VBool _ -> ()
  | Int { lo; hi; _ }, VInt x -> check_int_range lo hi x what
  | Bits n, VInt x -> check_int_range Big.zero (Big.pred (Big.pow2 n)) x what
  | I128 (lo, hi), VInt x -> check_int_range lo hi x what
  | U128, VInt x -> check_int_range Big.zero (Big.pred (Big.pow2 128)) x what
  | Fixed { f; lo; hi; _ }, VInt x ->
    check_int_range (Big.shift_left lo f) (Big.shift_left hi f) x what
  | Timestamp, VInt x -> check_int_range (Big.neg (Big.pow2 63)) (Big.pred (Big.pow2 63)) x what
  | (F32 | F64), VFloat x ->
    if not (Float.is_finite x) then refuse ("non_finite_float: " ^ what);
    if t = F32 && not (Float.is_finite (to_f32 x)) then refuse ("non_finite_float: " ^ what)
  | CF32 (mn, mx, _), VFloat x ->
    if not (Float.is_finite x) then refuse ("non_finite_float: " ^ what);
    let x = to_f32 x in
    if not (Float.is_finite x) || x < to_f32 mn || x > to_f32 mx then refuse ("invalid_value: " ^ what)
  | Str n, VStr s ->
    (match n with Some n when String.length s > n -> refuse ("length_over_bound: " ^ what) | _ -> ());
    if not (Num.is_text s) then refuse ("invalid_value: " ^ what)
  | Bytes n, VStr s -> if String.length s > n then refuse ("length_over_bound: " ^ what)
  | Enum e, VEnum o ->
    if o < 0 || o > Array.length e.e_vars then refuse ("invalid_value: " ^ what);
    if o > 0 && e.e_vars.(o - 1).v_deprecated then refuse ("deprecated_variant: " ^ what)
  | Flags fl, VInt m ->
    if Big.compare m Big.zero < 0 || Big.num_bits m > Array.length fl.fl_vars then refuse ("invalid_value: " ^ what)
  | _ -> ignore check_scalar; refuse ("invalid_value: " ^ what)

let buf_esc buf (s : string) =
  Buffer.add_char buf '"';
  String.iter (fun c ->
      match c with
      | '"' -> Buffer.add_string buf "\\\""
      | '\\' -> Buffer.add_string buf "\\\\"
      | '\b' -> Buffer.add_string buf "\\b"
      | '\012' -> Buffer.add_string buf "\\f"
      | '\n' -> Buffer.add_string buf "\\n"
      | '\r' -> Buffer.add_string buf "\\r"
      | '\t' -> Buffer.add_string buf "\\t"
      | c when Char.code c < 0x20 -> Buffer.add_string buf (Printf.sprintf "\\u%04x" (Char.code c))
      | c -> Buffer.add_char buf c) s;
  Buffer.add_char buf '"'

let nl buf d = Buffer.add_char buf '\n'; Buffer.add_string buf (String.make (2 * d) ' ')

let int_width_wide = function
  | Int { w; _ } -> w > 4
  | Bits n -> n > 32
  | I128 _ | U128 -> true
  | _ -> false

let guard_holds (fields : value array) (guards : (int * bool) list) =
  List.for_all (fun (gi, want) -> match fields.(gi) with VBool b -> b = want | _ -> false) guards

(* pointer graph pass W1: count namings *)
type gstate = { counts : (int, int) Hashtbl.t; labels : (int, int) Hashtbl.t; mutable next_label : int;
                written : (int, unit) Hashtbl.t }

let rec walk_count st (t : ty) (v : value) (open_ : int list) =
  match t, v with
  | Struct s, VStruct fs ->
    Array.iteri (fun i (f : field_d) ->
        if guard_holds fs f.f_guards then
          match f.f_ty, fs.(i) with
          | Opt _, VOpt None -> ()
          | ft, fv -> walk_count st ft fv open_) s.s_fields
  | Opt t, VOpt (Some x) -> walk_count st t x open_
  | (Arr (_, t) | Keyed (_, t)), (VArr xs | VKeyed xs) -> Array.iter (fun x -> walk_count st t x open_) xs
  | Map (_, vt), VMap es -> Array.iter (fun (_, x) -> match vt, x with
      | Opt _, VOpt None -> ()
      | _ -> walk_count st vt x open_) es
  | Union un, VUnion (o, x) when o > 0 && o <= Array.length un.u_arms ->
    (match un.u_arms.(o - 1).a_ty with Some at -> walk_count st at x open_ | None -> ())
  | Ptr s, VPtr (Some n) ->
    if List.mem n.nid open_ then refuse "data_cycle";
    let c = try Hashtbl.find st.counts n.nid with Not_found -> 0 in
    Hashtbl.replace st.counts n.nid (c + 1);
    if c = 0 then walk_count st (Struct s) n.nv (n.nid :: open_)
  | Blob _, VPtr (Some n) ->
    let c = try Hashtbl.find st.counts n.nid with Not_found -> 0 in
    if c > 0 then refuse "blob_shared";
    Hashtbl.replace st.counts n.nid (c + 1)
  | _ -> ()

let rec value st buf (t : ty) (v : value) (d : int) what =
  match t, v with
  | Bool, VBool b -> Buffer.add_string buf (if b then "true" else "false")
  | (Int _ | Bits _ | I128 _ | U128), VInt x ->
    check_scalar t v what;
    if int_width_wide t then (Buffer.add_char buf '"'; Buffer.add_string buf (Big.to_string x); Buffer.add_char buf '"')
    else Buffer.add_string buf (Big.to_string x)
  | Fixed { i; f; _ }, VInt x ->
    check_scalar t v what;
    let s = Num.fixed_text x f in
    if i + f > 32 then (Buffer.add_char buf '"'; Buffer.add_string buf s; Buffer.add_char buf '"')
    else Buffer.add_string buf s
  | (F32 | CF32 _), VFloat x ->
    check_scalar t v what;
    let x = to_f32 x in
    let x = (match t with CF32 _ when x = 0.0 -> 0.0 | _ -> x) in
    Buffer.add_string buf (Num.float_text ~is32:true x)
  | F64, VFloat x -> check_scalar t v what; Buffer.add_string buf (Num.float_text ~is32:false x)
  | Timestamp, VInt x -> check_scalar t v what; buf_esc buf (Timefmt.to_rfc3339 x)
  | Str _, VStr s -> check_scalar t v what; buf_esc buf s
  | Bytes _, VStr s -> check_scalar t v what; buf_esc buf (Num.b64_encode s)
  | Enum e, VEnum o ->
    check_scalar t v what;
    buf_esc buf (if o = 0 then "None" else e.e_vars.(o - 1).v_name)
  | Flags fl, VInt m ->
    check_scalar t v what;
    let names = ref [] in
    Array.iteri (fun i n -> if Big.test_bit_mag (Big.abs m).Big.mag i then names := n :: !names) fl.fl_vars;
    let names = List.rev !names in
    if names = [] then Buffer.add_string buf "[]"
    else begin
      Buffer.add_char buf '[';
      List.iteri (fun k n -> if k > 0 then Buffer.add_char buf ','; nl buf (d + 1); buf_esc buf n) names;
      nl buf d; Buffer.add_char buf ']'
    end
  | Struct s, VStruct fs -> table st buf s fs d None what
  | Opt t, VOpt (Some x) -> value st buf t x d what
  | Arr (k, et), VArr xs ->
    (match k with
     | ACounted (mn, mx) -> if Array.length xs < mn || Array.length xs > mx then refuse ("count_over_bound: " ^ what)
     | AFixed n -> if Array.length xs <> n then refuse ("count_over_bound: " ^ what)
     | AOpen -> ());
    if Array.length xs = 0 then Buffer.add_string buf "[]"
    else begin
      Buffer.add_char buf '[';
      Array.iteri (fun k x ->
          if k > 0 then Buffer.add_char buf ',';
          nl buf (d + 1);
          (match et, x with
           | Opt _, VOpt None -> Buffer.add_string buf "null"
           | _ -> value st buf et x (d + 1) what)) xs;
      nl buf d; Buffer.add_char buf ']'
    end
  | Keyed (en, et), VKeyed xs ->
    if Array.length xs = 0 then Buffer.add_string buf "{}"
    else begin
      Buffer.add_char buf '{';
      Array.iteri (fun k x ->
          if k > 0 then Buffer.add_char buf ',';
          nl buf (d + 1);
          buf_esc buf en.e_vars.(k).v_name;
          Buffer.add_string buf ": ";
          value st buf et x (d + 1) what) xs;
      nl buf d; Buffer.add_char buf '}'
    end
  | Map (kt, vt), VMap es ->
    if Array.length es = 0 then Buffer.add_string buf "{}"
    else begin
      let es = Array.copy es in
      Array.sort (fun (a, _) (b, _) -> Wirecommon.key_compare kt a b) es;
      Buffer.add_char buf '{';
      Array.iteri (fun k (key, x) ->
          if k > 0 then Buffer.add_char buf ',';
          nl buf (d + 1);
          (match key with
           | VStr s ->
             check_scalar kt key what;
             buf_esc buf s
           | VInt n -> check_scalar kt key what; buf_esc buf (Big.to_string n)
           | _ -> refuse "bad map key");
          Buffer.add_string buf ": ";
          (match vt, x with
           | Opt _, VOpt None -> Buffer.add_string buf "null"
           | _ -> value st buf vt x (d + 1) what)) es;
      nl buf d; Buffer.add_char buf '}'
    end
  | Union un, VUnion (o, x) ->
    if o < 0 || o > Array.length un.u_arms then refuse ("invalid_value: " ^ what);
    if o = 0 then Buffer.add_string buf "{}"
    else begin
      let arm = un.u_arms.(o - 1) in
      Buffer.add_char buf '{';
      nl buf (d + 1);
      buf_esc buf arm.a_name;
      Buffer.add_string buf ": ";
      (match arm.a_ty with
       | None -> Buffer.add_string buf "null"
       | Some at -> value st buf at x (d + 1) what);
      nl buf d; Buffer.add_char buf '}'
    end
  | Ptr _, VPtr None | Blob _, VPtr None -> Buffer.add_string buf "null"
  | Blob is_str, VPtr (Some n) ->
    (match n.nv with
     | VStr s -> if is_str then (if not (Num.is_text s) then refuse "invalid_value"; buf_esc buf s) else buf_esc buf (Num.b64_encode s)
     | _ -> refuse "bad blob")
  | Ptr s, VPtr (Some n) ->
    let count = try Hashtbl.find st.counts n.nid with Not_found -> 1 in
    if count <= 1 then table st buf s (match n.nv with VStruct fs -> fs | _ -> refuse "bad node") d None what
    else begin
      match Hashtbl.find_opt st.labels n.nid with
      | Some l ->
        Buffer.add_char buf '{'; nl buf (d + 1);
        Buffer.add_string buf "\"&node\": "; Buffer.add_string buf (string_of_int l);
        nl buf d; Buffer.add_char buf '}'
      | None ->
        let l = st.next_label in
        st.next_label <- l + 1;
        Hashtbl.replace st.labels n.nid l;
        table st buf s (match n.nv with VStruct fs -> fs | _ -> refuse "bad node") d (Some l) what
    end
  | _ -> refuse ("invalid_value: shape of " ^ what)

and table st buf (s : struct_d) (fs : value array) d label what =
  if d > 128 then refuse "depth";
  if s.s_json_array then begin
    (* W14: the table's text IS its one field's array *)
    let f = s.s_fields.(0) in
    value st buf f.f_ty fs.(0) d (what ^ "." ^ f.f_name)
  end else begin
    let present = ref [] in
    Array.iteri (fun i (f : field_d) ->
        if guard_holds fs f.f_guards then
          match f.f_ty, fs.(i) with
          | Opt _, VOpt None -> ()
          | _ -> present := i :: !present) s.s_fields;
    let present = List.rev !present in
    if present = [] && label = None then Buffer.add_string buf "{}"
    else if present = [] then refuse "empty_node_definition"
    else begin
      Buffer.add_char buf '{';
      let first = ref true in
      (match label with
       | Some l ->
         nl buf (d + 1); Buffer.add_string buf "\"&node\": "; Buffer.add_string buf (string_of_int l);
         first := false
       | None -> ());
      List.iter (fun i ->
          let f = s.s_fields.(i) in
          if not !first then Buffer.add_char buf ',';
          first := false;
          nl buf (d + 1);
          buf_esc buf f.f_key;
          Buffer.add_string buf ": ";
          value st buf f.f_ty fs.(i) (d + 1) (s.s_name ^ "." ^ f.f_name)) present;
      nl buf d; Buffer.add_char buf '}'
    end
  end

let to_json (root : struct_d) (v : value) : string =
  let st = { counts = Hashtbl.create 16; labels = Hashtbl.create 16; next_label = 1; written = Hashtbl.create 1 } in
  walk_count st (Struct root) v [];
  let buf = Buffer.create 1024 in
  (match v with
   | VStruct fs -> table st buf root fs 0 None root.s_name
   | _ -> refuse "root");
  Buffer.add_char buf '\n';
  Buffer.contents buf
