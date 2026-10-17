; Restricted work data. Acceptance is distinct.
;
; A comment attached to the table.
(:schema 1 :root "schema/goal"
 :title ; explanation between key and value
 "Schema roadmap"

 ; Comment above the next field.
 :nodes
 ((:id "one" :title "Changed" :state :unknown) ; first element
  ; Second element stays below a blank line.

  (:id "two" :title "Second" :state :done))
 :text ((:line "a ; semicolon is data") (:line "line\nwith escape"))
 :count 2 ; trailing field comment
 ; container footer
)
; document footer
