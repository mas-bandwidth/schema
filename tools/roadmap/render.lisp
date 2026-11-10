;;;; tools/roadmap/render.lisp
;;;;
;;;; Deterministic roadmap table renderer and validator for Schema.
;;;;
;;;; The tables in ROADMAP.md are generated from data: the work graph in
;;;; docs/roadmap.sexp (one restricted S-expression plist) and the evidence
;;;; overlay in docs/roadmap-evidence/*.sexp (one form per task, written by
;;;; the cards that proved the task). Everything else on the page (the title,
;;;; the headings and every paragraph) is the owner's, written by hand, and this
;;;; renderer never writes it: each table sits between
;;;; <!-- nova-work:<roadmap-id>:start --> and <!-- nova-work:<roadmap-id>:end -->,
;;;; and only the bytes between those markers are rewritten.
;;;; tools/roadmap/tally_test.go holds every block to a table and its source
;;;; link, and, where sbcl is installed, holds the committed tables to this
;;;; renderer's output.
;;;;
;;;; A cell is ✅ only when every leaf task under its work node is :done, and a
;;;; :done task must carry non-empty :evidence. An evidence overlay entry may
;;;; move a task's :state and :evidence; it may not name a task the graph does
;;;; not have, and it is held to the same rule.
;;;;
;;;; Usage:
;;;;   sbcl --script tools/roadmap/render.lisp [--check] [--require-original-nine] [--evidence DIR] [roadmap.sexp] [ROADMAP.md]
;;;;   sbcl --script tools/roadmap/render.lisp --test

(defpackage :roadmap-renderer
  (:use :cl)
  (:export #:main
           #:render-table-string
           #:splice-page-string
           #:outside-markers
           #:update-roadmap-text
           #:apply-evidence-overlay
           #:run-tests))

(in-package :roadmap-renderer)

(defparameter *max-file-size* (* 5 1024 1024)) ; 5 MB
(defparameter *max-ast-depth* 64)
(defparameter *max-node-count* 10000)
(defparameter *max-dfs-depth* 64)

(defparameter *start-marker* "<!-- nova-work:fixed-tables:start -->")
(defparameter *end-marker* "<!-- nova-work:fixed-tables:end -->")

(defparameter *evidence-subdirectory* "roadmap-evidence/")

;;; UTF-8 file I/O avoiding NUL padding or byte-vs-character length mismatches

(defun read-file-to-string (pathname)
  (with-open-file (in pathname :direction :input :element-type 'character :external-format :utf-8)
    (let ((len (file-length in)))
      (when (and len (> len *max-file-size*))
        (error "File ~a exceeds maximum allowed size (~D bytes)" pathname *max-file-size*)))
    (with-output-to-string (out)
      (let ((buf (make-string 4096)))
        (loop for n = (read-sequence buf in)
              while (plusp n)
              do (write-sequence buf out :end n))))))

(defun write-string-to-file (pathname string)
  (with-open-file (out pathname :direction :output :if-exists :supersede :if-does-not-exist :create :external-format :utf-8)
    (write-string string out)))

;;; Restricted reader setup

(defun make-restricted-readtable ()
  (let ((rt (copy-readtable nil)))
    ;; Disable dispatch syntax # (blocks #. #+ #- #= ## #' #A #S etc.)
    (set-macro-character (code-char 35)
                         (lambda (stream char)
                           (declare (ignore stream char))
                           (error "Dispatch syntax # is forbidden in restricted reader"))
                         nil rt)
    ;; Disable quote syntax '
    (set-macro-character (code-char 39)
                         (lambda (stream char)
                           (declare (ignore stream char))
                           (error "Quote syntax ' is forbidden in restricted reader"))
                         nil rt)
    ;; Disable backquote `
    (set-macro-character (code-char 96)
                         (lambda (stream char)
                           (declare (ignore stream char))
                           (error "Backquote syntax ` is forbidden in restricted reader"))
                         nil rt)
    ;; Disable comma ,
    (set-macro-character (code-char 44)
                         (lambda (stream char)
                           (declare (ignore stream char))
                           (error "Comma syntax , is forbidden in restricted reader"))
                         nil rt)
    rt))

(defun read-restricted-from-string (str)
  (let ((*readtable* (make-restricted-readtable))
        (*read-eval* nil))
    (with-input-from-string (s str)
      (let ((form (read s t nil)))
        ;; Require exactly one top-level form; reject trailing garbage / multiple forms
        (let* ((eof (gensym "EOF"))
               (next (read s nil eof)))
          (unless (eq next eof)
            (error "Trailing data or multiple top-level forms in restricted reader input")))
        form))))

(defun read-restricted-forms-from-string (str)
  "Read every top-level form of STR under the restricted reader: the shape of
an evidence overlay file, one plist per task."
  (let ((*readtable* (make-restricted-readtable))
        (*read-eval* nil)
        (eof (gensym "EOF"))
        (forms nil))
    (with-input-from-string (s str)
      (loop for form = (read s nil eof)
            until (eq form eof)
            do (push form forms)))
    (nreverse forms)))

(defun read-restricted-file (pathname)
  (let ((content (read-file-to-string pathname)))
    (read-restricted-from-string content)))

;;; Plist and AST structural validation

(defun validate-plist-no-duplicates (plist context)
  (unless (and (listp plist) (evenp (length plist)))
    (error "Malformed property list in ~a: ~s" context plist))
  (let ((seen (make-hash-table :test 'eq)))
    (loop for (key val) on plist by #'cddr do
      (unless (keywordp key)
        (error "Non-keyword property key ~s in ~a" key context))
      (when (gethash key seen)
        (error "Duplicate property key ~s in ~a" key context))
      (setf (gethash key seen) t))))

(defun check-ast-depth (form depth max-depth)
  (when (> depth max-depth)
    (error "AST nesting depth exceeded maximum bound of ~D" max-depth))
  (when (consp form)
    (check-ast-depth (car form) (1+ depth) max-depth)
    (check-ast-depth (cdr form) depth max-depth)))

(defun validate-root (sexp)
  (validate-plist-no-duplicates sexp "root form")
  (check-ast-depth sexp 0 *max-ast-depth*)
  (let ((schema (getf sexp :schema))
        (root (getf sexp :root))
        (nodes (getf sexp :nodes)))
    (unless (eql schema 1)
      (error "Unsupported schema version ~s (expected 1)" schema))
    (unless (and (stringp root) (> (length (string-trim " " root)) 0))
      (error "Missing or invalid non-empty string :root in root form"))
    (unless (and (listp nodes) nodes)
      (error "Nodes property must be a non-empty list"))
    (when (> (length nodes) *max-node-count*)
      (error "Node count ~D exceeds maximum allowed limit ~D" (length nodes) *max-node-count*))
    nodes))

(defun non-blank-string-p (s)
  (and (stringp s) (plusp (length (string-trim '(#\Space #\Tab #\Newline #\Return) s)))))

(defun validate-task-node (id node)
  "The rule a task is held to: a known state, and evidence when it is done."
  (let ((state (getf node :state))
        (evidence (getf node :evidence)))
    (unless (member state '(:unknown :todo :doing :done))
      (error "Task ~s has invalid state: ~s" id state))
    (when (eq state :done)
      (unless (and (listp evidence)
                   evidence
                   (some #'non-blank-string-p evidence))
        (error "Done task ~s has empty or missing required :evidence" id)))))

(defun build-and-validate-node-table (nodes root-id)
  (let ((table (make-hash-table :test 'equal)))
    ;; Step 1: Ensure valid plists with no duplicate keys, unique IDs, known types
    (dolist (node nodes)
      (validate-plist-no-duplicates node "node")
      (let ((id (getf node :id))
            (type (getf node :type)))
        (unless (and (stringp id) (> (length (string-trim " " id)) 0))
          (error "Node missing valid non-empty string :id: ~s" node))
        (unless (member type '(:task :work-set :roadmap))
          (error "Unsupported work node type ~s in node ~s" type id))
        (when (gethash id table)
          (error "Duplicate node ID: ~s" id))
        (when (getf node :implementation)
          (unless (and (member (getf node :implementation) '(:implemented :partial :unsupported))
                       (getf node :implementation-evidence)
                       (every #'non-blank-string-p (getf node :implementation-evidence)))
            (error "Invalid implementation assessment or missing source evidence in ~s" id)))
        (setf (gethash id table) node)))

    ;; Verify declared root exists in the node table
    (unless (gethash root-id table)
      (error "Declared :root node ~s not found in :nodes" root-id))

    ;; Step 2: Structural validation per node type
    (maphash
     (lambda (id node)
       (let ((type (getf node :type)))
         (case type
           (:work-set
            (let ((children (getf node :children)))
              (unless (and (listp children) children)
                (error "Empty or missing required :children in work-set ~s" id))
              (dolist (c children)
                (unless (stringp c)
                  (error "Child reference in work-set ~s must be a string: ~s" id c))
                (unless (gethash c table)
                  (error "Dangling reference: child ~s in work-set ~s does not exist" c id)))))
           (:task
            (validate-task-node id node))
           (:roadmap
            (let ((rows (getf node :rows))
                  (cols (getf node :columns))
                  (cells (getf node :cells))
                  (row-ids (make-hash-table :test 'equal))
                  (col-ids (make-hash-table :test 'equal))
                  (coords (make-hash-table :test 'equal)))
              (unless (and (listp rows) rows)
                (error "Roadmap ~s has empty or missing :rows" id))
              (unless (and (listp cols) cols)
                (error "Roadmap ~s has empty or missing :columns" id))
              (unless (and (listp cells) cells)
                (error "Roadmap ~s has empty or missing :cells" id))

              ;; Validate rows
              (dolist (r rows)
                (unless (and (listp r) (>= (length r) 2) (stringp (first r)) (stringp (second r)))
                  (error "Invalid row definition in roadmap ~s: ~s" id r))
                (when (gethash (first r) row-ids)
                  (error "Duplicate row ID ~s in roadmap ~s" (first r) id))
                (setf (gethash (first r) row-ids) t))

              ;; Validate columns
              (dolist (c cols)
                (unless (and (listp c) (>= (length c) 2) (stringp (first c)) (stringp (second c)))
                  (error "Invalid column definition in roadmap ~s: ~s" id c))
                (when (gethash (first c) col-ids)
                  (error "Duplicate column ID ~s in roadmap ~s" (first c) id))
                (setf (gethash (first c) col-ids) t))

              ;; Validate cells
              (dolist (cell cells)
                (unless (and (listp cell) (>= (length cell) 3)
                             (stringp (first cell))
                             (stringp (second cell))
                             (stringp (third cell)))
                  (error "Invalid cell definition in roadmap ~s: ~s" id cell))
                (let ((r (first cell))
                      (c (second cell))
                      (w (third cell)))
                  (unless (gethash r row-ids)
                    (error "Cell in roadmap ~s references unknown row ~s" id r))
                  (unless (gethash c col-ids)
                    (error "Cell in roadmap ~s references unknown column ~s" id c))
                  (unless (gethash w table)
                    (error "Cell (~s, ~s) in roadmap ~s references non-existent work node ~s" r c id w))
                  (let ((coord (cons r c)))
                    (when (gethash coord coords)
                      (error "Duplicate cell coordinates (~s, ~s) in roadmap ~s" r c id))
                    (setf (gethash coord coords) t))))

              ;; Completeness check
              (maphash
               (lambda (r-id ignore-r)
                 (declare (ignore ignore-r))
                 (maphash
                  (lambda (c-id ignore-c)
                    (declare (ignore ignore-c))
                    (unless (gethash (cons r-id c-id) coords)
                      (error "Missing cell for row ~s and column ~s in roadmap ~s" r-id c-id id)))
                  col-ids))
               row-ids)))
           (otherwise
            nil))))
     table)

    ;; Step 3: Validate entire graph for cycles (including disconnected subgraphs)
    (let ((memo (make-hash-table :test 'equal)))
      (maphash
       (lambda (id node)
         (declare (ignore node))
         (collect-work-leaves id table memo nil 0))
       table))

    table))

;;; The evidence overlay: docs/roadmap-evidence/*.sexp
;;;
;;; Each file holds any number of top-level plists, one per task:
;;;   (:id "go/F9" :state :done :evidence ("file_test.go: TestName") :tested-revision "..." :note "...")
;;; The entry's :state replaces the task's; its :evidence replaces the task's
;;; when present; :note replaces the task's when present. The task must exist
;;; and be a :task, and the result is held to validate-task-node.

(defun plist-put (plist key value)
  "A copy of PLIST with KEY set to VALUE, replacing or appending."
  (let ((out nil) (found nil))
    (loop for (k v) on plist by #'cddr do
      (if (eq k key)
          (progn (push k out) (push value out) (setf found t))
          (progn (push k out) (push v out))))
    (unless found
      (push key out) (push value out))
    (nreverse out)))

(defun apply-evidence-overlay (table forms &optional (context "evidence overlay"))
  "Apply overlay FORMS onto the node TABLE in place. Returns the count applied."
  (let ((applied 0))
    (dolist (form forms)
      (validate-plist-no-duplicates form context)
      (let* ((id (getf form :id))
             (node (and (stringp id) (gethash id table))))
        (unless (non-blank-string-p id)
          (error "~a: entry without a non-empty string :id: ~s" context form))
        (unless node
          (error "~a: id ~s is not a node of the roadmap" context id))
        (unless (eq (getf node :type) :task)
          (error "~a: id ~s is not a task (it is a ~s)" context id (getf node :type)))
        (unless (getf form :state)
          (error "~a: entry ~s carries no :state" context id))
        (let ((updated (plist-put node :state (getf form :state))))
          (when (getf form :evidence)
            (setf updated (plist-put updated :evidence (getf form :evidence))))
          (when (getf form :note)
            (setf updated (plist-put updated :note (getf form :note))))
          (when (getf form :tested-revision)
            (setf updated (plist-put updated :tested-revision (getf form :tested-revision))))
          (validate-task-node id updated)
          (setf (gethash id table) updated)
          (incf applied))))
    applied))

(defun evidence-files (dir)
  "Every *.sexp under DIR whose name does not start with a dot, sorted by
name; nil when DIR does not exist."
  (when (and dir (probe-file dir))
    (sort (remove-if (lambda (p) (let ((name (pathname-name p)))
                                   (or (null name) (zerop (length name)) (char= (char name 0) #\.))))
                     (copy-list (directory (merge-pathnames "*.sexp" dir))))
          #'string< :key #'namestring)))

(defun apply-evidence-directory (table dir)
  "Apply every overlay file under DIR. Returns (values files-applied entries-applied)."
  (let ((files 0) (entries 0))
    (dolist (file (evidence-files dir))
      (let ((forms (read-restricted-forms-from-string (read-file-to-string file))))
        (incf entries (apply-evidence-overlay table forms (namestring file)))
        (incf files)))
    (values files entries)))

(defun default-evidence-directory (sexp-path)
  "docs/roadmap-evidence/ beside the sexp: the directory the cards write to."
  (merge-pathnames *evidence-subdirectory*
                   (make-pathname :name nil :type nil :defaults (pathname sexp-path))))

;;; DFS folding with cycle detection, depth bound, and memoization

(defun collect-work-leaves (work-node-id table memo visiting depth)
  (when (> depth *max-dfs-depth*)
    (error "Maximum recursion depth ~D exceeded at node ~s" *max-dfs-depth* work-node-id))
  (when (member work-node-id visiting :test 'equal)
    (error "Cycle detected involving node ~s in path ~s" work-node-id visiting))
  (multiple-value-bind (cached found) (gethash work-node-id memo)
    (if found
        cached
        (let ((node (gethash work-node-id table)))
          (unless node
            (error "Dangling reference to work node ~s" work-node-id))
          (let ((type (getf node :type)))
            (cond
              ((eq type :task)
               (let ((result (list work-node-id)))
                 (setf (gethash work-node-id memo) result)
                 result))
              ((eq type :work-set)
               (let ((children (getf node :children))
                     (accum nil))
                 (unless children
                   (error "Empty work-set: ~s" work-node-id))
                 (dolist (child-id children)
                   (let ((child-leaves (collect-work-leaves child-id table memo (cons work-node-id visiting) (1+ depth))))
                     (dolist (leaf child-leaves)
                       ;; Deduplicate leaf tasks so shared task refs do not inflate count
                       (pushnew leaf accum :test 'equal))))
                 (let ((sorted (sort accum #'string<)))
                   (setf (gethash work-node-id memo) sorted)
                   sorted)))
              ((eq type :roadmap)
               ;; Recursive roadmap support: traverse its cell work references
               (let ((cells (getf node :cells))
                     (accum nil))
                 (unless cells
                   (error "Empty roadmap: ~s" work-node-id))
                 (dolist (cell cells)
                   (let ((cell-work-id (third cell)))
                     (let ((child-leaves (collect-work-leaves cell-work-id table memo (cons work-node-id visiting) (1+ depth))))
                       (dolist (leaf child-leaves)
                         (pushnew leaf accum :test 'equal)))))
                 (let ((sorted (sort accum #'string<)))
                   (setf (gethash work-node-id memo) sorted)
                   sorted)))
              (t
               (error "Unsupported work node type ~s in node ~s" type work-node-id))))))))

;;; Finding the targeted roadmap via reachability from :root

(defun find-reachable-roadmap (root-id table &optional target-id)
  (let ((visited (make-hash-table :test 'equal)))
    (labels ((walk (node-id)
               (when (gethash node-id visited)
                 (return-from walk nil))
               (setf (gethash node-id visited) t)
               (let ((node (gethash node-id table)))
                 (when node
                   (let ((type (getf node :type)))
                     (when (eq type :roadmap)
                       (when (or (null target-id) (string= (getf node :id) target-id))
                         (return-from find-reachable-roadmap node)))
                     (case type
                       (:work-set
                        (dolist (c (getf node :children))
                          (walk c)))
                       (:roadmap
                        (dolist (cell (getf node :cells))
                          (walk (third cell))))))))))
      (walk root-id)))
  nil)

;;; Progress evaluation

(defstruct cell-eval
  text
  is-green
  has-unknown)

(defun evaluate-cell (leaf-ids table)
  (let ((total (length leaf-ids))
        (done 0)
        (unknown 0)
        (todo 0)
        (doing 0))
    (when (zerop total)
      (error "Cell has 0 leaf tasks"))
    (dolist (id leaf-ids)
      (let* ((task (gethash id table))
             (state (getf task :state)))
        (case state
          (:done (incf done))
          (:unknown (incf unknown))
          (:todo (incf todo))
          (:doing (incf doing))
          (otherwise (error "Unrecognized state ~s for task ~s" state id)))))
    (cond
      ((= done total)
       ;; All required leaves done => visible green indicator + 100%
       (make-cell-eval :text "✅ 100%" :is-green t :has-unknown nil))
      ((zerop unknown)
       ;; Known incomplete cell => done/total percentage
       (let ((pct (floor (* 100 done) total)))
         (make-cell-eval :text (format nil "~D%" pct) :is-green nil :has-unknown nil)))
      (t
       ;; Unknown leaves => explicit ? with known verified count / labelled lower bound
       (let ((pct (floor (* 100 done) total)))
         (make-cell-eval :text (format nil "? (≥ ~D%)" pct) :is-green nil :has-unknown t))))))

;;; Table rendering

(defun assessed-cell-text (eval work &optional leaf-ids table)
  ;; This roadmap view shows verified completion only. Detailed states stay in S.
  (declare (ignore work leaf-ids table))
  (if (cell-eval-is-green eval) "✅" "❌"))

(defun render-table-string (roadmap table sexp-path)
  (let* ((rows (getf roadmap :rows))
         (cols (getf roadmap :columns))
         (cells (getf roadmap :cells))
         (memo (make-hash-table :test 'equal))
         (cell-evals (make-hash-table :test 'equal))
         (out (make-string-output-stream)))

    ;; Precompute all cells
    (dolist (cell cells)
      (let* ((r (first cell))
             (c (second cell))
             (work-id (third cell))
             (leaves (collect-work-leaves work-id table memo nil 0))
             (eval (evaluate-cell leaves table)))
        (setf (cell-eval-text eval) (assessed-cell-text eval (gethash work-id table) leaves table))
        (setf (gethash (cons r c) cell-evals) eval)))

    ;; Header
    (format out "| feature")
    (dolist (col cols)
      (format out " | ~a" (second col)))
    (format out " |~%")

    ;; Separator
    (format out "|---")
    (dolist (col cols)
      (declare (ignore col))
      (format out "|:---:"))
    (format out "|~%")

    ;; Feature rows
    (dolist (row rows)
      (let ((r-id (first row))
            (r-title (second row)))
        (format out "| ~a" r-title)
        (dolist (col cols)
          (let* ((c-id (first col))
                 (eval (gethash (cons r-id c-id) cell-evals)))
            (format out " | ~a" (cell-eval-text eval))))
        (format out " |~%")))

    ;; Summary row (Final language row)
    ;; "Output the verified feature percentage; keep counts in the source work set."
    (let ((n-features (length rows)))
      (format out "| complete")
      (dolist (col cols)
        (let ((c-id (first col))
              (green-count 0))
          (dolist (row rows)
            (let* ((r-id (first row))
                   (eval (gethash (cons r-id c-id) cell-evals)))
              (when (cell-eval-is-green eval)
                (incf green-count))))
          (let ((pct (floor (* 100 green-count) n-features)))
            (format out " | ~D%" pct))))
      (format out " |~%"))

    ;; Link to source data
    (format out "~%[Source data](~a)~%" sexp-path)

    (get-output-stream-string out)))

;;; Tables only
;;;
;;; The root form carries
;;;   :page (:roadmaps ("packet-wire" "fixed-tables" "variable-tables"))
;;; and nothing else: the page's headings and prose are the owner's, written by hand
;;; in ROADMAP.md, and the data holds cell data and evidence only. A :page key
;;; other than :roadmaps, or a :text on a roadmap node, is prose in the data and
;;; is refused.

(defun validate-page (sexp table)
  "The :page form, checked: only :roadmaps, naming reachable roadmap nodes
once each, none of which carries :text."
  (let ((page (getf sexp :page))
        (root-id (getf sexp :root)))
    (unless page
      (error "Root form lacks :page (:roadmaps, the table ids in page order)"))
    (validate-plist-no-duplicates page ":page")
    (loop for (key nil) on page by #'cddr do
      (unless (eq key :roadmaps)
        (error ":page carries ~s; it holds :roadmaps only (headings and prose live in ROADMAP.md, written by hand)" key)))
    (let ((ids (getf page :roadmaps)))
      (unless (and (listp ids) ids (every #'stringp ids))
        (error ":page :roadmaps must be a non-empty list of roadmap ids"))
      (let ((seen (make-hash-table :test 'equal)))
        (dolist (id ids)
          (when (gethash id seen)
            (error ":page :roadmaps names ~s twice" id))
          (setf (gethash id seen) t)
          (let ((rm (find-reachable-roadmap root-id table id)))
            (unless rm
              (error ":page :roadmaps names ~s, which is no roadmap node reachable from root ~s" id root-id))
            (when (getf rm :text)
              (error "Roadmap ~s carries :text; prose lives in ROADMAP.md, written by hand, never in the data" id))))))
    page))

;;; Explicit release acceptance; rendering/drift checks remain usable mid-work.

(defparameter *original-nine-languages*
  '("cpp" "c" "cs" "go" "rust" "java" "js" "dart" "elixir"))

(defparameter *original-nine-roadmaps*
  '("packet-wire" "fixed-tables" "variable-tables"))

(defun original-nine-problems (sexp table)
  "Every unfinished original-nine cell and leaf, plus missing required page
sections/columns. TABLE is the validated graph after evidence overlays."
  (let ((page (validate-page sexp table))
        (memo (make-hash-table :test 'equal))
        (problems nil))
    (dolist (id *original-nine-roadmaps*)
      (unless (member id (getf page :roadmaps) :test #'string=)
        (push (format nil "Missing required rendered roadmap section ~s" id) problems))
      (let ((roadmap (find-reachable-roadmap (getf sexp :root) table id)))
        (if (null roadmap)
            (push (format nil "Missing required reachable roadmap ~s" id) problems)
            (dolist (lang *original-nine-languages*)
              (if (not (assoc lang (getf roadmap :columns) :test #'string=))
                  (push (format nil "Missing required language ~s in roadmap ~s" lang id) problems)
                  (dolist (row (getf roadmap :rows))
                    (let* ((row-id (first row))
                           (cell (find-if (lambda (c)
                                            (and (string= row-id (first c))
                                                 (string= lang (second c))))
                                          (getf roadmap :cells)))
                           (work-id (third cell))
                           (leaves (collect-work-leaves work-id table memo nil 0))
                           (unfinished
                             (remove-if
                              (lambda (leaf)
                                (let ((task (gethash leaf table)))
                                  (and (eq (getf task :state) :done)
                                       (some #'non-blank-string-p (getf task :evidence)))))
                              leaves)))
                      (when unfinished
                        (push (format nil "Unfinished cell ~a/~a/~a (work ~s)" id row-id lang work-id)
                              problems)
                        (dolist (leaf unfinished)
                          (push (format nil "  leaf ~s: state ~s~a"
                                        leaf (getf (gethash leaf table) :state)
                                        (if (some #'non-blank-string-p
                                                  (getf (gethash leaf table) :evidence))
                                            "" ", missing evidence"))
                                problems))))))))))
    (nreverse problems)))

;;; Splicing the tables into ROADMAP.md
;;;
;;; make roadmap reads the committed page and rewrites only what sits between
;;; each <!-- nova-work:<id>:start --> and <!-- nova-work:<id>:end --> pair:
;;; a blank line, the table and its [Source data] link, a blank line. Every
;;; byte outside the markers is left exactly as it was.

(defun start-marker (id) (format nil "<!-- nova-work:~a:start -->" id))
(defun end-marker (id) (format nil "<!-- nova-work:~a:end -->" id))

(defun marker-line-p (line)
  (let ((prefix "<!-- nova-work:")
        (suffix " -->"))
    (and (> (length line) (+ (length prefix) (length suffix)))
         (string= prefix line :end2 (length prefix))
         (string= suffix line :start2 (- (length line) (length suffix))))))

(defun line-bounds (text pos)
  "The start and end (exclusive, before the newline) of the line holding POS."
  (let ((start (let ((nl (position #\Newline text :end pos :from-end t)))
                 (if nl (1+ nl) 0)))
        (end (or (position #\Newline text :start pos) (length text))))
    (values start end)))

(defun find-marker-line (text marker)
  "The start of the one line of TEXT that is exactly MARKER; refuses a marker
that is missing, repeated, or shares its line with other text."
  (let ((found nil)
        (from 0))
    (loop
      (let ((pos (search marker text :start2 from)))
        (unless pos (return))
        (multiple-value-bind (start end) (line-bounds text pos)
          (unless (and (= start pos) (= end (+ pos (length marker))))
            (error "~s shares its line with other text" marker))
          (when found
            (error "~s appears more than once" marker))
          (setf found start))
        (setf from (+ pos (length marker)))))
    (unless found
      (error "~s is missing from the page" marker))
    found))

(defun splice-block (text id table-string)
  "TEXT with the block ID's contents replaced by TABLE-STRING; everything
outside the block's two marker lines is returned unchanged."
  (let* ((start (find-marker-line text (start-marker id)))
         (inner-start (+ start (length (start-marker id))))
         (end (find-marker-line text (end-marker id))))
    (unless (< start end)
      (error "~s opens after it closes" id))
    (when (search "<!-- nova-work:" text :start2 inner-start :end2 end)
      (error "block ~s holds another marker" id))
    (concatenate 'string
                 (subseq text 0 inner-start)
                 (format nil "~%~%~a~%~%"
                         (string-trim '(#\Newline #\Space #\Tab) table-string))
                 (subseq text end))))

(defun update-roadmap-text (roadmap-text table-string)
  "The single-block form: splice TABLE-STRING into the fixed-tables block."
  (splice-block roadmap-text "fixed-tables" table-string))

(defun outside-markers (text)
  "TEXT with the contents of every block removed and its marker lines kept:
the part of the page that is written by hand."
  (let ((out (make-string-output-stream))
        (inside nil)
        (pos 0))
    (loop while (< pos (length text)) do
      (let* ((nl (position #\Newline text :start pos))
             (end (if nl (1+ nl) (length text)))
             (line (subseq text pos (if nl nl (length text)))))
        (cond
          ((marker-line-p line)
           (write-string (subseq text pos end) out)
           (setf inside (not (null (search ":start -->" line)))))
          ((not inside)
           (write-string (subseq text pos end) out)))
        (setf pos end)))
    (get-output-stream-string out)))

(defun page-block-ids (text)
  "The ids of the start markers in TEXT, in page order."
  (let ((ids nil)
        (prefix "<!-- nova-work:")
        (suffix ":start -->"))
    (dolist (line (split-lines text))
      (when (and (marker-line-p line) (search suffix line))
        (push (subseq line (length prefix) (- (length line) (length suffix))) ids)))
    (nreverse ids)))

(defun split-lines (text)
  (let ((lines nil) (pos 0))
    (loop
      (let ((nl (position #\Newline text :start pos)))
        (push (subseq text pos (or nl (length text))) lines)
        (unless nl (return))
        (setf pos (1+ nl))))
    (nreverse lines)))

(defun splice-page-string (existing sexp table sexp-path)
  "EXISTING with every table block re-rendered from the data, in :page
order. Refuses a page whose blocks are not exactly the :page roadmaps in
order, and refuses a result whose hand-written part differs from EXISTING's."
  (let* ((page (validate-page sexp table))
         (root-id (getf sexp :root))
         (ids (getf page :roadmaps))
         (text existing))
    (unless (equal (page-block-ids existing) ids)
      (error "ROADMAP.md carries blocks ~s; the data's :page :roadmaps are ~s"
             (page-block-ids existing) ids))
    (dolist (id ids)
      (setf text (splice-block text id
                               (render-table-string (find-reachable-roadmap root-id table id)
                                                    table sexp-path))))
    (unless (string= (outside-markers text) (outside-markers existing))
      (error "Splicing changed ROADMAP.md outside the nova-work markers"))
    text))

(defun load-work-graph (sexp-path evidence-dir)
  "Read the sexp, validate it, apply the evidence overlay. Returns
(values sexp table files entries)."
  (unless (probe-file sexp-path)
    (error "Source file not found: ~a" sexp-path))
  (let* ((sexp (read-restricted-file sexp-path))
         (nodes (validate-root sexp))
         (root-id (getf sexp :root))
         (table (build-and-validate-node-table nodes root-id)))
    (multiple-value-bind (files entries) (apply-evidence-directory table evidence-dir)
      (values sexp table files entries))))

(defun render-to-roadmap-file (sexp-path roadmap-path check-only
                                &optional evidence-dir require-original-nine)
  (let ((evidence-dir (or evidence-dir (default-evidence-directory sexp-path))))
    (multiple-value-bind (sexp table files entries) (load-work-graph sexp-path evidence-dir)
      (unless (probe-file roadmap-path)
        (error "~a not found: the tables are spliced into the hand-written page, which must exist" roadmap-path))
      (let* ((existing (read-file-to-string roadmap-path))
             (page (splice-page-string existing sexp table sexp-path))
             (problems (and require-original-nine (original-nine-problems sexp table))))
        (when require-original-nine
          (if problems
              (progn
                (format *error-output* "INCOMPLETE: original-nine roadmap acceptance failed~%")
                (dolist (problem problems)
                  (format *error-output* "~a~%" problem)))
              (format t "OK: original-nine roadmap acceptance (all three sections, all required leaves done with evidence)~%")))
        (if check-only
            (if (string= existing page)
                (progn
                  (format t "OK: the tables in ~a are up to date with ~a (~D evidence files, ~D entries)~%"
                          roadmap-path sexp-path files entries)
                  (if problems 1 0))
                (progn
                  (format *error-output* "DRIFT: a table in ~a differs from what ~a renders; run: make roadmap~%"
                          roadmap-path sexp-path)
                  1))
            (if problems
                1
                (progn
                  (write-string-to-file roadmap-path page)
                  (format t "Spliced the tables into ~a from ~a (~D evidence files, ~D entries)~%"
                          roadmap-path sexp-path files entries)
                  0)))))))

;;; Test Suite

(defun original-nine-test-graph ()
  "A complete original-nine page with unfinished optional expansion columns."
  (let ((sections '("packet-wire" "fixed-tables" "variable-tables"))
        (languages '("cpp" "c" "cs" "go" "rust" "java" "js" "dart" "elixir" "lua"))
        (nodes nil))
    (dolist (section sections)
      (let ((cells nil))
        (dolist (lang languages)
          (let ((id (format nil "~a/~a" section lang)))
            (push (list :id id :type :task
                        :state (if (string= lang "lua") :todo :done)
                        :evidence (unless (string= lang "lua") '("executed fixture gate")))
                  nodes)
            (push (list "feature" lang id) cells)))
        (push (list :id section :type :roadmap :title section
                    :rows '(("feature" "Feature"))
                    :columns (mapcar (lambda (lang) (list lang lang)) languages)
                    :cells (nreverse cells))
              nodes)))
    (push (list :id "goal" :type :work-set :children sections) nodes)
    (list :schema 1 :root "goal"
          :page (list :roadmaps (copy-list sections))
          :nodes (nreverse nodes))))

(defun original-nine-test-problems (sexp &optional overlays)
  (let ((table (build-and-validate-node-table (validate-root sexp) (getf sexp :root))))
    (apply-evidence-overlay table overlays)
    (original-nine-problems sexp table)))

(defun original-nine-test-node (sexp id)
  (find id (getf sexp :nodes) :key (lambda (node) (getf node :id)) :test #'string=))

(defun original-nine-test-set (sexp id key value)
  (setf (getf sexp :nodes)
        (mapcar (lambda (node)
                  (if (string= id (getf node :id)) (plist-put node key value) node))
                (getf sexp :nodes))))

(defun run-tests ()
  (format t "=== Running Roadmap Renderer Validation Test Suite ===~%")
  (let ((pass-count 0)
        (fail-count 0))

    (labels ((test (name fn)
               (handler-case
                   (progn
                     (funcall fn)
                     (format t "PASS: ~a~%" name)
                     (incf pass-count))
                 (error (e)
                   (format t "FAIL: ~a (~a)~%" name e)
                   (incf fail-count)))))

      (test "original nine accepts all three sections and ignores optional expansion TODOs"
        (lambda ()
          (when (original-nine-test-problems (original-nine-test-graph))
            (error "Complete original nine did not pass"))))

      (test "original nine names every unfinished cell and leaf"
        (lambda ()
          (let ((sexp (original-nine-test-graph)))
            (loop for id in '("packet-wire/cpp" "fixed-tables/c" "variable-tables/dart")
                  for state in '(:todo :doing :unknown) do
                    (original-nine-test-set sexp id :state state))
            (let ((problems (original-nine-test-problems sexp)))
              (unless (= 6 (length problems))
                (error "Expected three cell and three leaf diagnostics: ~s" problems))
              (dolist (id '("packet-wire/cpp" "fixed-tables/c" "variable-tables/dart"))
                (unless (some (lambda (line) (and (search "leaf" line) (search id line))) problems)
                  (error "Missing unfinished leaf ~a: ~s" id problems)))))))

      (test "original nine descends work sets and reports shared debt for both cells"
        (lambda ()
          (let* ((sexp (original-nine-test-graph))
                 (roadmap (original-nine-test-node sexp "variable-tables")))
            (push '(:id "shared-debt" :type :task :state :todo) (getf sexp :nodes))
            (push '(:id "nested" :type :work-set :children ("shared-debt")) (getf sexp :nodes))
            (push '(:id "both" :type :work-set :children ("nested" "variable-tables/dart"))
                  (getf sexp :nodes))
            (dolist (cell (getf roadmap :cells))
              (when (member (second cell) '("dart" "go") :test #'string=)
                (setf (third cell) "both")))
            (let ((problems (original-nine-test-problems sexp)))
              (unless (and (= 4 (length problems))
                           (= 2 (count-if (lambda (line) (search "leaf \"shared-debt\"" line)) problems))
                           (some (lambda (line) (search "variable-tables/feature/dart" line)) problems)
                           (some (lambda (line) (search "variable-tables/feature/go" line)) problems))
                (error "Shared nested debt lost a cell or leaf: ~s" problems))))))

      (test "original nine refuses a missing required column even when remaining matrix is complete"
        (lambda ()
          (let* ((sexp (original-nine-test-graph))
                 (roadmap (original-nine-test-node sexp "fixed-tables")))
            (setf (getf roadmap :columns)
                  (remove "dart" (getf roadmap :columns) :key #'first :test #'string=)
                  (getf roadmap :cells)
                  (remove "dart" (getf roadmap :cells) :key #'second :test #'string=))
            (unless (equal (original-nine-test-problems sexp)
                           '("Missing required language \"dart\" in roadmap \"fixed-tables\""))
              (error "Missing original-nine column escaped")))))

      (test "original nine refuses an unrendered section that remains in the graph"
        (lambda ()
          (let ((sexp (original-nine-test-graph)))
            (setf (getf (getf sexp :page) :roadmaps) '("packet-wire" "fixed-tables"))
            (unless (equal (original-nine-test-problems sexp)
                           '("Missing required rendered roadmap section \"variable-tables\""))
              (error "Hidden required section escaped")))))

      (test "original nine refuses a removed section"
        (lambda ()
          (let ((sexp (original-nine-test-graph)))
            (setf (getf (getf sexp :page) :roadmaps) '("packet-wire" "fixed-tables")
                  (getf sexp :nodes)
                  (remove "variable-tables" (getf sexp :nodes)
                          :key (lambda (node) (getf node :id)) :test #'string=))
            (original-nine-test-set sexp "goal" :children '("packet-wire" "fixed-tables"))
            (unless (= 2 (length (original-nine-test-problems sexp)))
              (error "Missing required section escaped")))))

      (test "original nine uses effective overlay state in both directions"
        (lambda ()
          (let ((sexp (original-nine-test-graph)))
            (unless (= 2 (length (original-nine-test-problems
                                 sexp '((:id "variable-tables/dart" :state :todo)))))
              (error "Overlay regression escaped"))
            (original-nine-test-set sexp "variable-tables/dart" :state :todo)
            (when (original-nine-test-problems
                   sexp '((:id "variable-tables/dart" :state :done :evidence ("executed new gate"))))
              (error "Proven overlay completion was ignored")))))

      (test "original nine fails closed on evidence and malformed graph"
        (lambda ()
          (dolist (fault '(:evidence :dangling :cycle :missing-cell))
            (let* ((sexp (original-nine-test-graph))
                   (node (original-nine-test-node sexp "variable-tables"))
                   (caught nil))
              (case fault
                (:evidence
                 (original-nine-test-set sexp "variable-tables/dart" :evidence nil))
                (:dangling (setf (third (first (getf node :cells))) "missing"))
                (:cycle
                 (push '(:id "cycle" :type :work-set :children ("cycle")) (getf sexp :nodes)))
                (:missing-cell (setf (getf node :cells) (rest (getf node :cells)))))
              (handler-case (original-nine-test-problems sexp)
                (error () (setf caught t)))
              (unless caught (error "Accepted malformed original-nine input: ~s" fault))))))

      (test "literal EOF keyword is trailing data"
        (lambda ()
          (let ((caught nil))
            (handler-case (read-restricted-from-string "(:schema 1) :eof")
              (error () (setf caught t)))
            (unless caught (error "Accepted trailing :eof keyword")))))

      (test "whitespace-only evidence is not proof"
        (lambda ()
          (let* ((blank (coerce (list #\Tab #\Newline #\Return #\Space) 'string))
                 (nodes (list (list :id "proof" :type :task :state :done
                                    :evidence (list blank))))
                 (caught nil))
            (handler-case (build-and-validate-node-table nodes "proof")
              (error () (setf caught t)))
            (unless caught (error "Accepted whitespace-only evidence")))))

      ;; Test 1: Shared leaf deduplication
      (test "implementation assessment cannot manufacture acceptance"
        (lambda ()
          (dolist (pair '((:implemented "❌") (:partial "❌") (:unsupported "❌")))
            (let ((eval (make-cell-eval :text "? (≥ 0%)" :is-green nil :has-unknown t)))
              (unless (string= (assessed-cell-text eval (list :implementation (first pair))) (second pair))
                (error "Implementation status not rendered"))
              (when (cell-eval-is-green eval) (error "Source assessment became green"))))))

      (test "unstarted and verified cell presentation"
        (lambda ()
          (let* ((table (make-hash-table :test 'equal))
                 (eval (make-cell-eval :text "0%" :is-green nil :has-unknown nil)))
            (setf (gethash "new" table) '(:state :todo))
            (unless (string= (assessed-cell-text eval nil '("new") table) "❌")
              (error "Unstarted cell lost its cross"))
            (setf (cell-eval-is-green eval) t)
            (unless (string= (assessed-cell-text eval nil '("new") table) "✅")
              (error "Verified cell lost its tick")))))

      (test "shared leaf deduplication"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "ws-root" :nodes
                         ((:id "task-shared" :type :task :title "Shared" :state :done :evidence ("commit 123"))
                          (:id "task-unique" :type :task :title "Unique" :state :done :evidence ("commit 456"))
                          (:id "ws-1" :type :work-set :children ("task-shared" "task-unique"))
                          (:id "ws-2" :type :work-set :children ("task-shared"))
                          (:id "ws-root" :type :work-set :children ("ws-1" "ws-2")))))
                 (nodes (validate-root sexp))
                 (table (build-and-validate-node-table nodes "ws-root"))
                 (memo (make-hash-table :test 'equal))
                 (leaves (collect-work-leaves "ws-root" table memo nil 0)))
            (unless (= (length leaves) 2)
              (error "Expected exactly 2 unique leaves, got ~D: ~s" (length leaves) leaves))
            (let ((eval (evaluate-cell leaves table)))
              (unless (cell-eval-is-green eval)
                (error "Expected cell to be green"))
              (unless (string= (cell-eval-text eval) "✅ 100%")
                (error "Expected ✅ 100%, got ~s" (cell-eval-text eval)))))))

      ;; Test 2: Cycle rejection (including disconnected cycle)
      (test "cycle rejection"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "ws-a" :nodes
                         ((:id "ws-a" :type :work-set :children ("ws-b"))
                          (:id "ws-b" :type :work-set :children ("ws-a")))))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case
                (build-and-validate-node-table nodes "ws-a")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to detect cycle between ws-a and ws-b")))))

      ;; Test 3: Dangling reference rejection
      (test "dangling reference rejection"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "ws-1" :nodes
                         ((:id "ws-1" :type :work-set :children ("nonexistent")))))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case
                (build-and-validate-node-table nodes "ws-1")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to detect dangling reference")))))

      ;; Test 4: Missing matrix cell rejection
      (test "missing matrix cell rejection"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "rm" :nodes
                         ((:id "t1" :type :task :title "T1" :state :todo :evidence ())
                          (:id "rm" :type :roadmap :title "RM"
                           :rows (("r1" "Row 1") ("r2" "Row 2"))
                           :columns (("c1" "Col 1"))
                           :cells (("r1" "c1" "t1")))))) ; missing ("r2" "c1")
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case
                (build-and-validate-node-table nodes "rm")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to detect missing matrix cell")))))

      ;; Test 5: Empty checklist rejection
      (test "empty checklist rejection"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "ws-empty" :nodes
                         ((:id "ws-empty" :type :work-set :children ()))))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case
                (build-and-validate-node-table nodes "ws-empty")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to reject empty work-set")))))

      ;; Test 6: Done task without evidence rejection
      (test "done leaf without evidence rejection"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "t-done-no-ev" :nodes
                         ((:id "t-done-no-ev" :type :task :title "T" :state :done :evidence ()))))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case
                (build-and-validate-node-table nodes "t-done-no-ev")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to reject done task without evidence")))))

      ;; Test 7: Reader-eval / dispatch rejection (#. payload)
      (test "#. reader payload rejection"
        (lambda ()
          (let ((caught nil))
            (handler-case
                (read-restricted-from-string "(:schema 1 :root \"schema\" :nodes #.(list 1 2 3))")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to reject #. reader payload in restricted reader")))))

      ;; Test 8: Nested completion and partial-vs-green rollup
      (test "nested completion and partial-vs-green rollup"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "roadmap-test" :nodes
                         ((:id "t-done-1" :type :task :title "T1" :state :done :evidence ("PR #1"))
                          (:id "t-done-2" :type :task :title "T2" :state :done :evidence ("PR #2"))
                          (:id "t-todo" :type :task :title "T3" :state :todo :evidence ())
                          (:id "t-unk" :type :task :title "T4" :state :unknown :evidence ())
                          (:id "ws-all-done" :type :work-set :children ("t-done-1" "t-done-2"))
                          (:id "ws-partial" :type :work-set :children ("t-done-1" "t-todo"))
                          (:id "ws-unknown" :type :work-set :children ("t-done-1" "t-unk"))
                          (:id "roadmap-test" :type :roadmap :title "Test Roadmap"
                           :rows (("f1" "Feature 1")
                                  ("f2" "Feature 2")
                                  ("f3" "Feature 3"))
                           :columns (("col-a" "Lang A")
                                     ("col-b" "Lang B"))
                           :cells (("f1" "col-a" "ws-all-done")
                                   ("f1" "col-b" "ws-all-done")
                                   ("f2" "col-a" "ws-partial")
                                   ("f2" "col-b" "ws-all-done")
                                   ("f3" "col-a" "ws-unknown")
                                   ("f3" "col-b" "ws-all-done"))))))
                 (nodes (validate-root sexp))
                 (table (build-and-validate-node-table nodes "roadmap-test"))
                 (rm (find-if (lambda (n) (eq (getf n :type) :roadmap)) nodes))
                 (rendered (render-table-string rm table "docs/roadmap.sexp")))

            (unless (search "| Feature 1 | ✅ | ✅ |" rendered)
              (error "Feature 1 row missing or incorrect: ~s" rendered))
            (unless (search "| Feature 2 | ❌ | ✅ |" rendered)
              (error "Feature 2 row missing or incorrect: ~s" rendered))
            (unless (search "| Feature 3 | ❌ | ✅ |" rendered)
              (error "Feature 3 row missing or incorrect: ~s" rendered))
            (unless (search "| complete | 33% | 100% |" rendered)
              (error "Complete row missing or incorrect: ~s" rendered))
            (unless (search "[Source data](docs/roadmap.sexp)" rendered)
              (error "Source data link missing: ~s" rendered)))))

      ;; Test 9: Quote and backquote syntax rejection
      (test "quote and backquote syntax rejection"
        (lambda ()
          (let ((caught-quote nil)
                (caught-backquote nil))
            (handler-case
                (read-restricted-from-string "(:foo 'bar)")
              (error () (setf caught-quote t)))
            (handler-case
                (read-restricted-from-string "(:foo `bar)")
              (error () (setf caught-backquote t)))
            (unless (and caught-quote caught-backquote)
              (error "Failed to reject quote or backquote")))))

      ;; Test 10: Single top-level form requirement
      (test "single top-level form requirement"
        (lambda ()
          (let ((caught nil))
            (handler-case
                (read-restricted-from-string "(:schema 1 :root \"s\" :nodes ()) (:extra form)")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to reject multiple top-level forms")))))

      ;; Test 11: Duplicate plist key rejection
      (test "duplicate plist key rejection"
        (lambda ()
          (let ((caught nil))
            (handler-case
                (validate-plist-no-duplicates '(:schema 1 :root "s" :root "s2") "test")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to reject duplicate plist key")))))

      ;; Test 12: Missing :root property rejection
      (test "missing :root property rejection"
        (lambda ()
          (let ((caught nil))
            (handler-case
                (validate-root '(:schema 1 :nodes ()))
              (error () (setf caught t)))
            (unless caught
              (error "Failed to reject missing :root")))))

      ;; Test 13: Unknown work node type rejection
      (test "unknown work node type rejection"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "bad" :nodes
                         ((:id "bad" :type :unsupported-type :title "Bad"))))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case
                (build-and-validate-node-table nodes "bad")
              (error () (setf caught t)))
            (unless caught
              (error "Failed to reject unsupported work node type")))))

      ;; Test 14: Reachable roadmap selection and decoy rejection
      (test "reachable roadmap selection and decoy rejection"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "root-goal" :nodes
                         ((:id "t-done" :type :task :title "Done" :state :done :evidence ("ev"))
                          (:id "real-roadmap" :type :roadmap :title "Real"
                           :rows (("r1" "Row 1")) :columns (("c1" "Col 1")) :cells (("r1" "c1" "t-done")))
                          (:id "decoy-roadmap" :type :roadmap :title "Decoy"
                           :rows (("r1" "Row 1")) :columns (("c1" "Col 1")) :cells (("r1" "c1" "t-done")))
                          (:id "root-goal" :type :work-set :children ("real-roadmap")))))
                 (nodes (validate-root sexp))
                 (table (build-and-validate-node-table nodes "root-goal"))
                 (selected (find-reachable-roadmap "root-goal" table "real-roadmap"))
                 (decoy-selected (find-reachable-roadmap "root-goal" table "decoy-roadmap")))
            (unless (and selected (string= (getf selected :id) "real-roadmap"))
              (error "Failed to find reachable roadmap 'real-roadmap'"))
            (when decoy-selected
              (error "Unreachable decoy roadmap was incorrectly selected")))))

      ;; Test 15: Recursive roadmap inside work-set leaves
      (test "recursive roadmap inside work-set leaves"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "nested-rm" :nodes
                         ((:id "t-done" :type :task :title "Done" :state :done :evidence ("ev"))
                          (:id "nested-rm" :type :roadmap :title "Nested"
                           :rows (("r1" "R1")) :columns (("c1" "C1")) :cells (("r1" "c1" "t-done")))
                          (:id "outer-ws" :type :work-set :children ("nested-rm")))))
                 (nodes (validate-root sexp))
                 (table (build-and-validate-node-table nodes "nested-rm"))
                 (memo (make-hash-table :test 'equal))
                 (leaves (collect-work-leaves "outer-ws" table memo nil 0)))
            (unless (equal leaves '("t-done"))
              (error "Expected leaves ('t-done'), got ~s" leaves)))))

      ;; Test 16: Two-write idempotence and zero NUL bytes on real UTF-8 content
      (test "two-write idempotence and zero NUL bytes"
        (lambda ()
          (let* ((utf8-sample (format nil "# Heading with Unicode: ✅ ❌ 🚀~%~%<!-- nova-work:fixed-tables:start -->~%OLD~%<!-- nova-work:fixed-tables:end -->~%~%Trailing text with symbols: §1.1 — © 2026~%"))
                 (table-content (format nil "| feature | c |~%|---|---|~%| f1 | ✅ 100% |~%| complete | 1/1 (100%) |~%~%[Source data](docs/roadmap.sexp)"))
                 (pass1 (update-roadmap-text utf8-sample table-content))
                 (pass2 (update-roadmap-text pass1 table-content)))
            (unless (string= pass1 pass2)
              (error "Marker update is not idempotent across multiple writes"))
            (when (find (code-char 0) pass1)
              (error "Rendered output contains NUL characters"))
            (unless (search "§1.1 — © 2026" pass1)
              (error "Trailing Unicode text corrupted or missing"))
            (unless (search "✅ ❌ 🚀" pass1)
              (error "Heading Unicode characters corrupted or missing")))))

      ;; Test 17: Disk fixture suite from testdata/
      (test "disk fixtures in tools/roadmap/testdata/"
        (lambda ()
          ;; valid_rollup.sexp
          (let* ((sexp (read-restricted-file "tools/roadmap/testdata/valid_rollup.sexp"))
                 (nodes (validate-root sexp))
                 (root-id (getf sexp :root))
                 (table (build-and-validate-node-table nodes root-id))
                 (rm (find-reachable-roadmap root-id table "roadmap-pilot"))
                 (rendered (render-table-string rm table "docs/roadmap.sexp")))
            (unless (search "| Feature 1 | ✅ | ✅ |" rendered)
              (error "valid_rollup Feature 1 failed"))
            (unless (search "| Feature 2 | ❌ | ✅ |" rendered)
              (error "valid_rollup Feature 2 failed"))
            (unless (search "| Feature 3 | ❌ | ✅ |" rendered)
              (error "valid_rollup Feature 3 failed"))
            (unless (search "| complete | 33% | 100% |" rendered)
              (error "valid_rollup complete row failed")))

          ;; cycle.sexp
          (let* ((sexp (read-restricted-file "tools/roadmap/testdata/cycle.sexp"))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case (build-and-validate-node-table nodes (getf sexp :root))
              (error () (setf caught t)))
            (unless caught (error "Failed to reject cycle.sexp")))

          ;; dangling_ref.sexp
          (let* ((sexp (read-restricted-file "tools/roadmap/testdata/dangling_ref.sexp"))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case (build-and-validate-node-table nodes (getf sexp :root))
              (error () (setf caught t)))
            (unless caught (error "Failed to reject dangling_ref.sexp")))

          ;; missing_cell.sexp
          (let* ((sexp (read-restricted-file "tools/roadmap/testdata/missing_cell.sexp"))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case (build-and-validate-node-table nodes (getf sexp :root))
              (error () (setf caught t)))
            (unless caught (error "Failed to reject missing_cell.sexp")))

          ;; empty_checklist.sexp
          (let* ((sexp (read-restricted-file "tools/roadmap/testdata/empty_checklist.sexp"))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case (build-and-validate-node-table nodes (getf sexp :root))
              (error () (setf caught t)))
            (unless caught (error "Failed to reject empty_checklist.sexp")))

          ;; done_no_evidence.sexp
          (let* ((sexp (read-restricted-file "tools/roadmap/testdata/done_no_evidence.sexp"))
                 (nodes (validate-root sexp))
                 (caught nil))
            (handler-case (build-and-validate-node-table nodes (getf sexp :root))
              (error () (setf caught t)))
            (unless caught (error "Failed to reject done_no_evidence.sexp")))

          ;; reader_payload.sexp
          (let ((caught nil))
            (handler-case (read-restricted-file "tools/roadmap/testdata/reader_payload.sexp")
              (error () (setf caught t)))
            (unless caught (error "Failed to reject reader_payload.sexp")))))

      ;; Test 18: The evidence overlay moves a task's state and evidence
      (test "evidence overlay moves state and evidence"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "rm" :nodes
                         ((:id "t1" :type :task :title "T1" :state :unknown :evidence nil)
                          (:id "t2" :type :task :title "T2" :state :done :evidence ("old proof"))
                          (:id "rm" :type :roadmap :title "RM"
                           :rows (("r1" "Row 1")) :columns (("c1" "Col 1") ("c2" "Col 2"))
                           :cells (("r1" "c1" "t1") ("r1" "c2" "t2"))))))
                 (nodes (validate-root sexp))
                 (table (build-and-validate-node-table nodes "rm"))
                 (forms (read-restricted-forms-from-string
                         "(:id \"t1\" :state :done :evidence (\"a_test.go: TestOne\") :tested-revision \"abc\")
                          (:id \"t2\" :state :unknown :note \"the corpus does not build here\")"))
                 (applied (apply-evidence-overlay table forms "test overlay"))
                 (rendered (render-table-string (gethash "rm" table) table "docs/roadmap.sexp")))
            (unless (= applied 2) (error "Expected 2 entries applied, got ~D" applied))
            (unless (search "| Row 1 | ✅ | ❌ |" rendered)
              (error "Overlay did not move the cells: ~s" rendered))
            (unless (equal (getf (gethash "t1" table) :evidence) '("a_test.go: TestOne"))
              (error "Overlay did not replace the evidence"))
            (unless (equal (getf (gethash "t2" table) :evidence) '("old proof"))
              (error "An entry without :evidence must keep the task's evidence")))))

      ;; Test 19: The overlay is refused for an unknown id, a non-task, and done without evidence
      (test "evidence overlay refusals"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "ws" :nodes
                         ((:id "t1" :type :task :title "T1" :state :unknown :evidence nil)
                          (:id "ws" :type :work-set :children ("t1")))))
                 (nodes (validate-root sexp))
                 (table (build-and-validate-node-table nodes "ws")))
            (dolist (bad '("(:id \"nope\" :state :done :evidence (\"x\"))"
                           "(:id \"ws\" :state :done :evidence (\"x\"))"
                           "(:id \"t1\" :state :done)"
                           "(:id \"t1\" :state :done :evidence (\" \"))"
                           "(:id \"t1\" :state :finished)"
                           "(:id \"t1\")"))
              (let ((caught nil))
                (handler-case
                    (apply-evidence-overlay table (read-restricted-forms-from-string bad) "test overlay")
                  (error () (setf caught t)))
                (unless caught
                  (error "Accepted a bad overlay entry: ~a" bad))))
            (unless (eq (getf (gethash "t1" table) :state) :unknown)
              (error "A refused overlay must leave the task as it was")))))

      ;; Test 20: The multi-form reader keeps the restricted reader's refusals
      (test "overlay reader is restricted too"
        (lambda ()
          (let ((caught nil))
            (handler-case
                (read-restricted-forms-from-string "(:id \"t1\" :state :done :evidence #.(list 1))")
              (error () (setf caught t)))
            (unless caught (error "Overlay reader accepted a #. payload")))
          (unless (= 3 (length (read-restricted-forms-from-string (format nil "(:a 1) ; a comment~%(:b 2) (:c 3)"))))
            (error "Overlay reader did not read every form"))))

      ;; Test 21: Only the tables are rewritten; the hand-written page is kept byte for byte
      (test "tables spliced, hand text kept"
        (lambda ()
          (let* ((sexp '(:schema 1 :root "goal"
                         :page (:roadmaps ("alpha" "beta"))
                         :nodes
                         ((:id "t-done" :type :task :title "Done" :state :done :evidence ("ev"))
                          (:id "t-todo" :type :task :title "Todo" :state :todo :evidence nil)
                          (:id "alpha" :type :roadmap :title "Alpha"
                           :rows (("r1" "Row 1")) :columns (("c1" "c1") ("c2" "c2"))
                           :cells (("r1" "c1" "t-done") ("r1" "c2" "t-todo")))
                          (:id "beta" :type :roadmap :title "Beta"
                           :rows (("r1" "Row 1")) :columns (("c1" "c1"))
                           :cells (("r1" "c1" "t-done")))
                          (:id "goal" :type :work-set :children ("alpha" "beta")))))
                 (nodes (validate-root sexp))
                 (table (build-and-validate-node-table nodes "goal"))
                 (hand (format nil "# the owner's  title~%~%Some   prose, kept  as written.~%~%<!-- nova-work:alpha:start -->~%stale~%<!-- nova-work:alpha:end -->~%~%## Beta~%<!-- nova-work:beta:start -->~%<!-- nova-work:beta:end -->~%Tail without a newline"))
                 (page (splice-page-string hand sexp table "docs/roadmap.sexp"))
                 (expected (format nil "# the owner's  title~%~%Some   prose, kept  as written.~%~%<!-- nova-work:alpha:start -->~%~%| feature | c1 | c2 |~%|---|:---:|:---:|~%| Row 1 | ✅ | ❌ |~%| complete | 100% | 0% |~%~%[Source data](docs/roadmap.sexp)~%~%<!-- nova-work:alpha:end -->~%~%## Beta~%<!-- nova-work:beta:start -->~%~%| feature | c1 |~%|---|:---:|~%| Row 1 | ✅ |~%| complete | 100% |~%~%[Source data](docs/roadmap.sexp)~%~%<!-- nova-work:beta:end -->~%Tail without a newline")))
            (unless (string= page expected)
              (error "Page differs from the expected splice:~%~a~%--- expected ---~%~a" page expected))
            (unless (string= page (splice-page-string page sexp table "docs/roadmap.sexp"))
              (error "Splice is not idempotent"))
            (unless (string= (outside-markers page) (outside-markers hand))
              (error "Splice changed the page outside the markers"))
            ;; Negative control: an edit outside the markers is seen by outside-markers.
            (when (string= (outside-markers (concatenate 'string page (format nil "~%Rust update: x.")))
                           (outside-markers page))
              (error "outside-markers is blind to a paragraph added outside the blocks"))
            ;; and an edit inside a block is not.
            (unless (string= (outside-markers (splice-block page "alpha" "| feature |"))
                             (outside-markers page))
              (error "outside-markers counts a block's contents as hand text")))))

      ;; Test 22: The data refuses prose, and the page must carry exactly the data's blocks
      (test "page refusals"
        (lambda ()
          (let* ((nodes '((:id "t-done" :type :task :title "Done" :state :done :evidence ("ev"))
                          (:id "alpha" :type :roadmap :title "Alpha"
                           :rows (("r1" "Row 1")) :columns (("c1" "c1")) :cells (("r1" "c1" "t-done")))
                          (:id "decoy" :type :roadmap :title "Decoy"
                           :rows (("r1" "Row 1")) :columns (("c1" "c1")) :cells (("r1" "c1" "t-done")))
                          (:id "goal" :type :work-set :children ("alpha"))))
                 (table (build-and-validate-node-table nodes "goal"))
                 (good (format nil "<!-- nova-work:alpha:start -->~%<!-- nova-work:alpha:end -->~%")))
            (dolist (page '((:roadmaps ("decoy"))
                            (:roadmaps ("alpha" "alpha"))
                            (:roadmaps ())
                            (:title "P" :roadmaps ("alpha"))
                            (:roadmaps ("alpha") :closing ((:title "Fund this work" :text "x")))
                            nil))
              (let ((caught nil))
                (handler-case
                    (splice-page-string good (list :schema 1 :root "goal" :page page :nodes nodes) table "docs/roadmap.sexp")
                  (error () (setf caught t)))
                (unless caught
                  (error "Accepted a bad :page: ~s" page))))
            (let* ((texty (mapcar (lambda (n)
                                    (if (string= (getf n :id) "alpha")
                                        (append n (list :text "Rust update: prose in the data."))
                                        n))
                                  nodes))
                   (texty-table (build-and-validate-node-table texty "goal"))
                   (caught nil))
              (handler-case
                  (splice-page-string good (list :schema 1 :root "goal" :page '(:roadmaps ("alpha")) :nodes texty)
                                      texty-table "docs/roadmap.sexp")
                (error () (setf caught t)))
              (unless caught (error "Accepted a roadmap node carrying :text")))
            (dolist (bad (list ""
                               (format nil "<!-- nova-work:alpha:start -->~%")
                               (format nil "~a~a" good good)
                               (format nil "x <!-- nova-work:alpha:start -->~%<!-- nova-work:alpha:end -->~%")
                               (format nil "<!-- nova-work:alpha:end -->~%<!-- nova-work:alpha:start -->~%")
                               (format nil "~a<!-- nova-work:decoy:start -->~%<!-- nova-work:decoy:end -->~%" good)))
              (let ((caught nil))
                (handler-case
                    (splice-page-string bad (list :schema 1 :root "goal" :page '(:roadmaps ("alpha")) :nodes nodes)
                                        table "docs/roadmap.sexp")
                  (error () (setf caught t)))
                (unless caught
                  (error "Spliced into a page with bad markers: ~s" bad))))))))

    (format t "=== Results: ~D passed, ~D failed ===~%" pass-count fail-count)
    (if (zerop fail-count)
        0
        1)))

;;; CLI entrypoint

(defun main (argv)
  (let ((check-only nil)
        (test-mode nil)
        (require-original-nine nil)
        (evidence-dir nil)
        (positional nil))
    (loop while argv do
      (let ((arg (pop argv)))
        (cond
          ((string= arg "--check")
           (setf check-only t))
          ((string= arg "--require-original-nine")
           (setf require-original-nine t))
          ((string= arg "--test")
           (setf test-mode t))
          ((string= arg "--evidence")
           (unless argv
             (format *error-output* "--evidence needs a directory~%")
             (sb-ext:exit :code 2))
           (setf evidence-dir (pop argv)))
          ((and (> (length arg) 2) (string= (subseq arg 0 2) "--"))
           (format *error-output* "Unknown flag: ~a~%" arg)
           (sb-ext:exit :code 2))
          (t
           (push arg positional)))))
    (setf positional (nreverse positional))

    (when test-mode
      (let ((code (run-tests)))
        (sb-ext:exit :code code)))

    (let ((sexp-path (or (first positional) "docs/roadmap.sexp"))
          (roadmap-path (or (second positional) "ROADMAP.md")))
      (handler-case
          (let ((code (render-to-roadmap-file sexp-path roadmap-path check-only
                                              (and evidence-dir
                                                   (pathname (if (char= (char evidence-dir (1- (length evidence-dir))) #\/)
                                                                 evidence-dir
                                                                 (concatenate 'string evidence-dir "/"))))
                                              require-original-nine)))
            (sb-ext:exit :code code))
        (error (e)
          (format *error-output* "Error: ~a~%" e)
          (sb-ext:exit :code 1))))))

(unless (member :in-roadmap-test-runner *features*)
  (main (cdr sb-ext:*posix-argv*)))
