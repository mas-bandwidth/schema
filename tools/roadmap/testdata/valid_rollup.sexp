(:schema 1
 :root "schema"
 :nodes
 ((:id "task-shared"
   :type :task
   :title "Shared Task"
   :state :done
   :evidence ("commit a1b2c3d4"))
  (:id "task-unique"
   :type :task
   :title "Unique Task"
   :state :done
   :evidence ("commit e5f60718"))
  (:id "task-todo"
   :type :task
   :title "Todo Task"
   :state :todo
   :evidence ())
  (:id "task-unknown"
   :type :task
   :title "Unknown Task"
   :state :unknown
   :evidence ())
  (:id "ws-shared-pair"
   :type :work-set
   :children ("task-shared" "task-unique"))
  (:id "ws-green"
   :type :work-set
   :children ("ws-shared-pair" "task-shared"))
  (:id "ws-partial"
   :type :work-set
   :children ("task-shared" "task-todo"))
  (:id "ws-unknown"
   :type :work-set
   :children ("task-shared" "task-unknown"))
  (:id "roadmap-pilot"
   :type :roadmap
   :title "Fixed tables"
   :scope-revision "4a30359e"
   :source-revision "8ea5ed8e"
   :rows (("f1" "Feature 1")
          ("f2" "Feature 2")
          ("f3" "Feature 3"))
   :columns (("cpp" "cpp")
             ("c" "c"))
   :cells (("f1" "cpp" "ws-green")
           ("f1" "c" "ws-green")
           ("f2" "cpp" "ws-partial")
           ("f2" "c" "ws-green")
           ("f3" "cpp" "ws-unknown")
           ("f3" "c" "ws-green")))))
