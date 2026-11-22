;;;; tools/roadmap/fixes.lisp
;;;;
;;;; The Fixes table: a roadmap block that is a plain list of fixes, not a
;;;; feature-by-language matrix. Loaded by render.lisp.
;;;;
;;;; A node of :type :fixes carries
;;;;   :columns ((column-id "Column title") ...)   one or more, in page order
;;;;   :entries ((entry-id "cell" "cell" ...) ...) one string per column
;;;; Each entry is one fix: the rule a runtime breaks, with the languages it
;;;; breaks it in as one field, never one entry per language. The entry id is
;;;; the stable handle in the data and is not rendered. A cell is one line of
;;;; plain text and never holds a pipe. The node is named in :page :roadmaps
;;;; like any table, and its block holds the table and the source link only.

(in-package :roadmap-renderer)

(defun fixes-cell-problem (cell)
  (cond ((not (non-blank-string-p cell)) "is not a non-blank string")
        ((find #\| cell) "holds a pipe, which would split the table cell")
        ((find #\Newline cell) "holds a newline")
        ((find #\Return cell) "holds a carriage return")
        (t nil)))

(defun validate-fixes-node (id node)
  "The rules a Fixes node is held to; any breach is an error naming it."
  (let ((cols (getf node :columns))
        (entries (getf node :entries))
        (col-ids (make-hash-table :test (quote equal)))
        (entry-ids (make-hash-table :test (quote equal))))
    (unless (and (listp cols) cols)
      (error "Fixes ~s has empty or missing :columns" id))
    (unless (and (listp entries) entries)
      (error "Fixes ~s has empty or missing :entries" id))
    (dolist (c cols)
      (unless (and (listp c) (= (length c) 2) (non-blank-string-p (first c)) (non-blank-string-p (second c)))
        (error "Invalid column definition in fixes ~s: ~s" id c))
      (let ((problem (fixes-cell-problem (second c))))
        (when problem
          (error "Fixes ~s column title ~s ~a" id (second c) problem)))
      (when (gethash (first c) col-ids)
        (error "Duplicate column ID ~s in fixes ~s" (first c) id))
      (setf (gethash (first c) col-ids) t))
    (dolist (e entries)
      (unless (and (listp e) (stringp (first e)) (non-blank-string-p (first e)))
        (error "Fixes ~s has an entry without a string id: ~s" id e))
      (when (gethash (first e) entry-ids)
        (error "Duplicate entry ID ~s in fixes ~s" (first e) id))
      (setf (gethash (first e) entry-ids) t)
      (unless (= (length (rest e)) (length cols))
        (error "Fixes ~s entry ~s carries ~D cells and the table has ~D columns"
               id (first e) (length (rest e)) (length cols)))
      (dolist (cell (rest e))
        (let ((problem (fixes-cell-problem cell)))
          (when problem
            (error "Fixes ~s entry ~s has a cell that ~a: ~s" id (first e) problem cell)))))
    t))

(defun render-fixes-string (node sexp-path)
  "NODE as a markdown table, one row per entry, then the source link."
  (let ((cols (getf node :columns))
        (entries (getf node :entries))
        (out (make-string-output-stream)))
    (format out "|")
    (dolist (c cols)
      (format out " ~a |" (second c)))
    (format out "~%|")
    (dolist (c cols)
      (declare (ignore c))
      (format out "---|"))
    (format out "~%")
    (dolist (e entries)
      (format out "|")
      (dolist (cell (rest e))
        (format out " ~a |" cell))
      (format out "~%"))
    (format out "~%[Source data](~a)~%" sexp-path)
    (get-output-stream-string out)))

(defun render-page-block (node table sexp-path)
  "The text of one page block: a Fixes node or a feature matrix."
  (if (eq (getf node :type) :fixes)
      (render-fixes-string node sexp-path)
      (render-table-string node table sexp-path)))
