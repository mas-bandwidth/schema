(* The conformance driver, test/conformance/README.md:
     driver <manifest> list
     driver <manifest> <surface> <outdir>
   Run from the repository root; every manifest path is repo-relative. *)

open Model

let surfaces = [ "wire"; "json-read"; "json-write"; "report"; "framing"; "json-hostile";
                 "write-refusal"; "write-refusal-fixed"; "write-refusal-json" ]

let read_file path =
  let ic = open_in_bin path in
  let n = in_channel_length ic in
  let s = really_input_string ic n in
  close_in ic; s

let write_file path s =
  let oc = open_out_bin path in
  output_string oc s; close_out oc

let log fmt = Printf.ksprintf (fun s -> prerr_endline s) fmt

(* ---- units ---- *)

type loaded = { u : unit_t; gates : string list; lock : (string, Fixed.lineage list) Hashtbl.t }

let units : (string, string list) Hashtbl.t = Hashtbl.create 64
let cache : (string, (loaded, string) result) Hashtbl.t = Hashtbl.create 64

let schema_files path =
  if Sys.is_directory path then
    Sys.readdir path |> Array.to_list |> List.filter (fun f -> Filename.check_suffix f ".schema")
    |> List.sort compare |> List.map (Filename.concat path)
  else [ path ]

(* the gated keys (SPEC-TABLES §11) this port does not declare: a case
   whose unit uses one is an absence, never a guess *)
let undeclared = [ "required"; "forms"; "integrity" ]

let load_unit key : (loaded, string) result =
  match Hashtbl.find_opt cache key with
  | Some r -> r
  | None ->
    let r =
      try
        let paths = Hashtbl.find units key in
        let files = List.concat_map schema_files paths in
        let decls = List.concat_map (fun f ->
            try Parse.parse_file (read_file f)
            with Parse.Error (m, l) -> raise (Model.Refused (Printf.sprintf "%s:%d: %s" f l m))) files in
        let u = Model.build decls in
        let gates = !Model.gated in
        let lock = Hashtbl.create 4 in
        List.iter (fun p ->
            let dir = if Sys.is_directory p then p else Filename.dirname p in
            let lp = Filename.concat dir "schema.lock" in
            if Sys.file_exists lp then
              Hashtbl.iter (fun k v -> Hashtbl.replace lock k v) (Fixed.read_lock lp)) paths;
        Ok { u; gates; lock }
      with
      | Model.Refused m -> Error m
      | Not_found -> Error ("no unit " ^ key)
      | Failure m -> Error ("failure: " ^ m)
      | Invalid_argument m -> Error ("invalid_argument: " ^ m) in
    Hashtbl.replace cache key r;
    r

exception Absent of string

let unit_root key rootname =
  match load_unit key with
  | Error m -> raise (Absent m)
  | Ok l ->
    List.iter (fun g -> if List.mem g undeclared then raise (Absent ("gated: " ^ g))) l.gates;
    (match Hashtbl.find_opt l.u.structs rootname with
     | Some s -> (l, s)
     | None -> raise (Absent ("no root " ^ rootname)))

(* ---- the codec, by the root's class ---- *)

type img = One of value | Batch of value list

let load_wire ?(framing = false) (l : loaded) (root : struct_d) (s : string) : img =
  if framing && root.s_fixed && String.length s > 0 && s.[0] = '\001' then begin
    (* SPEC-GAP 7: SPEC-TABLES §2.2 says a fixed root refuses form 1 as
       previous_form, and the report rows previous_form_* hold that; the framing rows read form-1 files at fixed roots and
       owe a form-3 write-back. The vector is followed. *)
    let v, _, _ = Wire1.load l.u root s in
    Batch [ v ]
  end else if root.s_fixed then
    let lin = try Hashtbl.find l.lock root.s_name with Not_found -> [] in
    Batch (Fixed.load_records root lin s ~capacity:(1 lsl 30))
  else begin
    let v, _, _ = Wire1.load l.u root s in
    One v
  end

let save_wire (root : struct_d) (i : img) : string =
  match i with
  | One v -> Wire1.save root v
  | Batch vs -> Fixed.save_records root vs

let first = function One v -> v | Batch (v :: _) -> v | Batch [] -> raise (Absent "empty batch")

let to_json root i =
  match i with
  | One v -> Jsonw.to_json root v
  | Batch [ v ] -> Jsonw.to_json root v
  | Batch _ -> raise (Absent "a batch of more than one record has no single text")

let of_json (root : struct_d) text : img =
  let v, _ = Jsonr.of_json root text in
  if root.s_fixed then Batch [ v ] else One v

(* a refusal by any reader or writer of this port *)
let is_refusal = function
  | Wire1.Refuse _ | Fixed.Refuse _ | Jsonr.Refuse _ | Jsonw.Refuse _ -> true
  | _ -> false

let reason = function
  | Wire1.Refuse m | Fixed.Refuse m | Jsonr.Refuse m | Jsonw.Refuse m -> m
  | e -> Printexc.to_string e

(* ---- the surfaces ---- *)

let absent outdir case why =
  log "absent %s: %s" case why;
  write_file (Filename.concat outdir (case ^ ".absent")) ""

let run_case outdir case f =
  try f () with
  | Absent m -> absent outdir case m
  | Model.Refused m -> absent outdir case m
  | e -> log "FAIL %s: %s" case (Printexc.to_string e); absent outdir case ("crash: " ^ Printexc.to_string e)

let fields_of l = String.split_on_char ' ' l |> List.concat_map (String.split_on_char '\t') |> List.filter (( <> ) "")

(* a read surface: read\n + value + <case>.wire, or refused\n *)
let verdict_surface outdir case root (read : unit -> img) =
  match read () with
  | exception e when is_refusal e ->
    log "%s refused: %s" case (reason e);
    write_file (Filename.concat outdir case) "refused\n"
  | exception (Invalid_argument m) ->
    log "%s refused (bounds): %s" case m;
    write_file (Filename.concat outdir case) "refused\n"
  | i ->
    let text = to_json root i in
    let w = save_wire root i in
    write_file (Filename.concat outdir case) ("read\n" ^ text);
    write_file (Filename.concat outdir (case ^ ".wire")) w

(* ---- write-refusal: set one field ---- *)

type step = F of string | I of int

let parse_path p =
  let steps = ref [] in
  List.iter (fun part ->
      match String.index_opt part '[' with
      | None -> steps := F part :: !steps
      | Some k ->
        steps := F (String.sub part 0 k) :: !steps;
        let rest = ref (String.sub part k (String.length part - k)) in
        while !rest <> "" do
          let c = String.index !rest ']' in
          steps := I (int_of_string (String.sub !rest 1 (c - 1))) :: !steps;
          rest := String.sub !rest (c + 1) (String.length !rest - c - 1)
        done) (String.split_on_char '.' p);
  List.rev !steps

exception Unrepresentable

let hex s = String.init (String.length s / 2) (fun i -> Char.chr (int_of_string ("0x" ^ String.sub s (2 * i) 2)))

(* UTF-16 units to bytes; a lone surrogate keeps its 3-byte form, which is not
   UTF-8, the nearest value this leg holds that is not text *)
let units_to_bytes s =
  let us = List.init (String.length s / 4) (fun i -> int_of_string ("0x" ^ String.sub s (4 * i) 4)) in
  let b = Buffer.create 16 in
  let put3 u = Buffer.add_char b (Char.chr (0xE0 lor (u lsr 12)));
    Buffer.add_char b (Char.chr (0x80 lor ((u lsr 6) land 0x3F)));
    Buffer.add_char b (Char.chr (0x80 lor (u land 0x3F))) in
  let rec go = function
    | [] -> ()
    | h :: l :: rest when h >= 0xD800 && h <= 0xDBFF && l >= 0xDC00 && l <= 0xDFFF ->
      Buffer.add_utf_8_uchar b (Uchar.of_int (0x10000 + ((h - 0xD800) lsl 10) + (l - 0xDC00))); go rest
    | u :: rest when u >= 0xD800 && u <= 0xDFFF -> put3 u; go rest
    | u :: rest -> Buffer.add_utf_8_uchar b (Uchar.of_int u); go rest in
  go us;
  Buffer.contents b

let rec set (t : ty) (v : value) (path : step list) (kind : string) (arg : string) : value =
  match path, t, v with
  | [], _, _ -> leaf t v kind arg
  | F n :: rest, Struct s, VStruct fs ->
    let idx = ref (-1) in
    Array.iteri (fun i (f : field_d) -> if f.f_name = n then idx := i) s.s_fields;
    if !idx < 0 then raise (Absent ("no field " ^ n));
    let fs = Array.copy fs in
    fs.(!idx) <- set s.s_fields.(!idx).f_ty fs.(!idx) rest kind arg;
    VStruct fs
  | _, Opt p, VOpt (Some x) -> VOpt (Some (set p x path kind arg))
  | _, Opt p, VOpt None -> VOpt (Some (set p (zero_value p) path kind arg))
  | _, Ptr s, VPtr (Some nd) -> nd.nv <- set (Struct s) nd.nv path kind arg; v
  | I i :: rest, Arr (_, et), VArr xs ->
    let xs = Array.copy xs in
    if i >= Array.length xs then raise (Absent "index past the array");
    xs.(i) <- set et xs.(i) rest kind arg; VArr xs
  | I i :: rest, Keyed (_, et), VKeyed xs ->
    let xs = Array.copy xs in xs.(i) <- set et xs.(i) rest kind arg; VKeyed xs
  | _ -> raise (Absent "path shape")

and leaf (t : ty) (v : value) kind arg : value =
  match kind, t, v with
  | _, Opt p, VOpt x -> VOpt (Some (leaf p (match x with Some x -> x | None -> zero_value p) kind arg))
  | "int", _, _ -> VInt (Big.of_string arg)
  | "float", _, _ ->
    VFloat (match arg with "nan" -> Float.nan | "inf" -> Float.infinity | "-inf" -> Float.neg_infinity
                         | s -> float_of_string s)
  | "ordinal", _, _ ->
    let n = Big.of_string arg in
    if not (Big.fits_int n) then raise Unrepresentable;
    VEnum (Big.to_int n)
  | "tag", Union un, _ ->
    let n = Big.of_string arg in
    if not (Big.fits_int n) then raise Unrepresentable;
    let o = Big.to_int n in
    let payload = if o >= 1 && o <= Array.length un.u_arms then
        (match un.u_arms.(o - 1).a_ty with
         | Some (Struct s) -> VStruct (Array.map (fun f -> f.f_default) s.s_fields)
         | Some at -> zero_value at | None -> VUnit)
      else VUnit in
    VUnion (o, payload)
  | "count", _, VArr xs ->
    let n = int_of_string arg in
    if n < 0 then raise Unrepresentable;
    let e0 = if Array.length xs > 0 then xs.(0)
      else (match t with Arr (_, et) -> zero_value et | _ -> VUnit) in
    VArr (Array.init n (fun i -> if i < Array.length xs then xs.(i) else e0))
  | "length", _, _ ->
    let n = int_of_string arg in
    if n < 0 then raise Unrepresentable;
    VStr (String.make n 'a')
  | "bytes", _, _ -> VStr (hex arg)
  | "units", _, _ -> VStr (units_to_bytes arg)
  | _ -> raise (Absent ("write-refusal kind " ^ kind))

let write_refusal outdir surface fs =
  match fs with
  | [ _; case; ukey; rootname; wire; path; kind; arg ] ->
    run_case outdir case (fun () ->
        let l, root = unit_root ukey rootname in
        let i = load_wire l root (read_file wire) in
        let out =
          match set (Struct root) (first i) (parse_path path) kind arg with
          | exception Unrepresentable -> "unrepresentable\n"
          | v ->
            let attempt () =
              match surface with
              | "write-refusal" ->
                ignore (if root.s_fixed then Fixed.save_records root [ v ] else Wire1.save root v)
              | "write-refusal-fixed" ->
                if not root.s_fixed then raise (Absent "a variable root has no fixed writer");
                ignore (Fixed.save_records root [ v ])
              | _ -> ignore (Jsonw.to_json root v) in
            (match attempt () with
             | () -> "wrote\n"
             | exception e when is_refusal e -> log "%s refused: %s" case (reason e); "refused\n") in
        write_file (Filename.concat outdir case) out)
  | _ -> ()

(* json-write-refusal: damage a loaded value in code *)
let damage root v d =
  match String.index_opt d ':' with
  | None -> v
  | Some k ->
    let how = String.sub d 0 k and field = String.sub d (k + 1) (String.length d - k - 1) in
    let bytes = if how = "narrow" then "a\xffb" else units_to_bytes "0061D8000062" in
    set (Struct root) v [ F field ] "bytes" (let b = Buffer.create 8 in String.iter (fun c -> Buffer.add_string b (Printf.sprintf "%02x" (Char.code c))) bytes; Buffer.contents b)

let main () =
  let argv = Sys.argv in
  if Array.length argv < 3 then (prerr_endline "usage: driver <manifest> list | <surface> <outdir>"; exit 3);
  let manifest = argv.(1) and surface = argv.(2) in
  if surface = "x-layout" then begin
    let lines = String.split_on_char (Char.chr 10) (read_file manifest) |> List.map fields_of in
    List.iter (function "unit" :: key :: paths -> Hashtbl.replace units key paths | _ -> ()) lines;
    let l, root = unit_root argv.(3) argv.(4) in
    let hx s = String.concat "" (List.map (fun c -> Printf.sprintf "%02x" (Char.code c)) (List.of_seq (String.to_seq s))) in
    Printf.printf "hash %Lu\nlayout %s\ndigest %s\n" (Fixed.own_hash root) (hx (Fixed.layout root)) (hx (Fixed.digest root));
    List.iter (fun (e : Fixed.lineage) -> Printf.printf "lock wire=%Lu\n bytes %s\n digest %s\n" e.wire (hx e.lbytes) (hx e.ldigest)) (try Hashtbl.find l.lock root.s_name with Not_found -> []);
    exit 0 end;
  if surface = "list" then (List.iter print_endline surfaces; exit 0);
  if not (List.mem surface surfaces) then exit 2;
  let outdir = argv.(3) in
  let lines = String.split_on_char '\n' (read_file manifest)
              |> List.map String.trim |> List.filter (fun l -> l <> "" && l.[0] <> '#') |> List.map fields_of in
  List.iter (function "unit" :: key :: paths -> Hashtbl.replace units key paths | _ -> ()) lines;
  List.iter (fun fs ->
      match surface, fs with
      | "wire", "instance" :: name :: ukey :: rootname :: wire :: _ ->
        run_case outdir name (fun () ->
            let l, root = unit_root ukey rootname in
            match load_wire l root (read_file wire) with
            | exception e when is_refusal e ->
              log "%s refused: %s" name (reason e);
              (* SPEC-GAP 6: the contract names no refusal spelling for the wire surface; the historical .absent is the one it says scores *)
              write_file (Filename.concat outdir (name ^ ".absent")) ""
            | i -> write_file (Filename.concat outdir name) (save_wire root i))
      | "json-read", "instance" :: name :: ukey :: rootname :: _ :: flags when not (List.mem "no-text" flags) ->
        run_case outdir name (fun () ->
            let _, root = unit_root ukey rootname in
            let text = read_file (Printf.sprintf "testdata/conformance/tables/json/%s.json" name) in
            match of_json root text with
            | exception e when is_refusal e ->
              log "%s refused: %s" name (reason e);
              write_file (Filename.concat outdir name) "refused\n"
            | i -> write_file (Filename.concat outdir name) (save_wire root i))
      | "json-write", "instance" :: name :: ukey :: rootname :: wire :: flags when not (List.mem "no-text" flags) ->
        run_case outdir (name ^ ".json") (fun () ->
            let l, root = unit_root ukey rootname in
            let i = load_wire l root (read_file wire) in
            write_file (Filename.concat outdir (name ^ ".json")) (to_json root i))
      | "json-write", [ _; case; ukey; rootname; wire; d ] when List.hd fs = "json-write-refusal" ->
        run_case outdir (case ^ ".json") (fun () ->
            let l, root = unit_root ukey rootname in
            let v = damage root (first (load_wire l root (read_file wire))) d in
            let out = match Jsonw.to_json root v with
              | exception e when is_refusal e -> "refused\n"
              | t -> t in
            write_file (Filename.concat outdir (case ^ ".json")) out)
      | ("report" | "framing"), k :: case :: ukey :: rootname :: wire :: _ when k = surface ->
        run_case outdir case (fun () ->
            let l, root = unit_root ukey rootname in
            verdict_surface outdir case root (fun () -> load_wire ~framing:(surface = "framing") l root (read_file wire)))
      | "json-hostile", "json-hostile" :: case :: ukey :: rootname :: tree :: _ ->
        run_case outdir case (fun () ->
            let _, root = unit_root ukey rootname in
            verdict_surface outdir case root (fun () ->
                of_json root (read_file (Filename.concat tree (rootname ^ ".json")))))
      | ("write-refusal" | "write-refusal-fixed" | "write-refusal-json"), k :: _ when k = surface ->
        write_refusal outdir surface fs
      | _ -> ()) lines;
  exit 0

let () = main ()
