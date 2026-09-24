;;;; tools/roadmap/render.lisp
;;;;
;;;; Deterministic roadmap renderer and validator for Schema.
;;;; Reads restricted S-expression data plists and updates ROADMAP.md
;;;; between <!-- nova-work:fixed-tables:start --> and <!-- nova-work:fixed-tables:end -->.
;;;;
;;;; Usage:
;;;;   sbcl --script tools/roadmap/render.lisp [--check] [roadmap.sexp] [ROADMAP.md]
;;;;   sbcl --script tools/roadmap/render.lisp --test

(defpackage :roadmap-renderer
  (:use :cl)
  (:export #:main
           #:render-table-string
           #:update-roadmap-text
           #:run-tests))

(in-package :roadmap-renderer)

(defparameter *max-file-size* (* 5 1024 1024)) ; 5 MB
(defparameter *max-ast-depth* 64)
(defparameter *max-node-count* 10000)
(defparameter *max-dfs-depth* 64)

(defparameter *start-marker* "<!-- nova-work:fixed-tables:start -->")
(defparameter *end-marker* "<!-- nova-work:fixed-tables:end -->")

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
  (with-open-file (out pathname :direction :output :if-exists :supersede :if-does-not-exist :error :external-format :utf-8)
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
                       (every (lambda (s) (and (stringp s) (plusp (length (string-trim '(#\Space #\Tab #\Newline #\Return) s)))))
                              (getf node :implementation-evidence)))
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
            (let ((state (getf node :state))
                  (evidence (getf node :evidence)))
              (unless (member state '(:unknown :todo :doing :done))
                (error "Task ~s has invalid state: ~s" id state))
              (when (eq state :done)
                (unless (and (listp evidence)
                             evidence
                             (some (lambda (e) (and (stringp e) (> (length (string-trim '(#\Space #\Tab #\Newline #\Return) e)) 0)))
                                   evidence))
                  (error "Done task ~s has empty or missing required :evidence" id)))))
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

;;; Marker update in ROADMAP.md

(defun update-roadmap-text (roadmap-text table-string)
  (let ((start-pos (search *start-marker* roadmap-text))
        (end-pos (search *end-marker* roadmap-text)))
    (unless (and start-pos end-pos (< start-pos end-pos))
      (error "Markers ~s and ~s not found or in wrong order" *start-marker* *end-marker*))
    (let* ((prefix-end (+ start-pos (length *start-marker*)))
           (prefix (subseq roadmap-text 0 prefix-end))
           (suffix (subseq roadmap-text end-pos))
           (cleaned-table (string-trim '(#\Newline #\Space #\Tab) table-string)))
      (format nil "~a~%~%~a~%~%~a" prefix cleaned-table suffix))))

(defun render-to-roadmap-file (sexp-path roadmap-path check-only &optional target-roadmap-id)
  (unless (probe-file sexp-path)
    (error "Source file not found: ~a" sexp-path))
  (unless (probe-file roadmap-path)
    (error "Target roadmap file not found: ~a" roadmap-path))

  (let* ((sexp (read-restricted-file sexp-path))
         (nodes (validate-root sexp))
         (root-id (getf sexp :root))
         (table (build-and-validate-node-table nodes root-id))
         ;; Select target roadmap reachable from :root (ignoring unreachable decoys)
         (roadmap (find-reachable-roadmap root-id table (or target-roadmap-id "fixed-tables"))))
    (unless roadmap
      (error "Roadmap ~s is not reachable from root ~s"
             (or target-roadmap-id "fixed-tables") root-id))

    (let* ((table-string (render-table-string roadmap table sexp-path))
           (existing-text (read-file-to-string roadmap-path))
           (updated-text (update-roadmap-text existing-text table-string)))
      (if check-only
          (if (string= existing-text updated-text)
              (progn
                (format t "OK: ~a is up to date with ~a~%" roadmap-path sexp-path)
                0)
              (progn
                (format *error-output* "DRIFT: ~a differs from generated roadmap table from ~a~%" roadmap-path sexp-path)
                1))
          (progn
            (write-string-to-file roadmap-path updated-text)
            (format t "Rendered roadmap table from ~a to ~a~%" sexp-path roadmap-path)
            0)))))

;;; Test Suite

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

    (format t "=== Results: ~D passed, ~D failed ===~%" pass-count fail-count)
    (if (zerop fail-count)
        0
        1))))

;;; CLI entrypoint

(defun main (argv)
  (let ((check-only nil)
        (test-mode nil)
        (positional nil))
    (dolist (arg argv)
      (cond
        ((string= arg "--check")
         (setf check-only t))
        ((string= arg "--test")
         (setf test-mode t))
        ((and (> (length arg) 2) (string= (subseq arg 0 2) "--"))
         (format *error-output* "Unknown flag: ~a~%" arg)
         (sb-ext:exit :code 2))
        (t
         (push arg positional))))
    (setf positional (nreverse positional))

    (when test-mode
      (let ((code (run-tests)))
        (sb-ext:exit :code code)))

    (let ((sexp-path (or (first positional) "docs/roadmap.sexp"))
          (roadmap-path (or (second positional) "ROADMAP.md")))
      (handler-case
          (let ((code (render-to-roadmap-file sexp-path roadmap-path check-only)))
            (sb-ext:exit :code code))
        (error (e)
          (format *error-output* "Error: ~a~%" e)
          (sb-ext:exit :code 1))))))

(unless (member :in-roadmap-test-runner *features*)
  (main (cdr sb-ext:*posix-argv*)))
