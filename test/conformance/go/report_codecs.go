package main

import (
	"reportarms"
	"reportkeyed1"
	"reportkeyed2"
	"reportmessages"
	"reportscalars"
)

// These variable readers retain the fixed fixtures' field vocabulary, allowing
// body-level evolution and refusal cases to reach the reader's field walk.
func init() {
	codecTable = append(codecTable,
		regionRow("reportmessages", "ToolMessage",
			reportmessages.ToolMessageLoadMeasure, reportmessages.ToolMessageLoad, reportmessages.ToolMessageMeasure, reportmessages.ToolMessageSave,
			func(text []byte, r *reportmessages.TableReport) (*reportmessages.ToolMessage, []byte, bool) {
				var b reportmessages.ToolMessageBuilder
				if !b.Init() {
					return nil, nil, false
				}
				defer b.Shutdown()
				if !reportmessages.ToolMessageFromJSON(&b, text, r) || !b.Lock() {
					return nil, nil, false
				}
				return b.AsConst(), b.Region(), true
			},
			reportmessages.ToolMessageToJSONMeasure, reportmessages.ToolMessageToJSON, reportmessages.ToolMessageCookMeasure, reportmessages.ToolMessageCookFrom,
			func(r *reportmessages.TableReport) report {
				return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
			}),
		regionRow("reportarms", "Root",
			reportarms.RootLoadMeasure, reportarms.RootLoad, reportarms.RootMeasure, reportarms.RootSave,
			func(text []byte, r *reportarms.TableReport) (*reportarms.Root, []byte, bool) {
				var b reportarms.RootBuilder
				if !b.Init() {
					return nil, nil, false
				}
				defer b.Shutdown()
				if !reportarms.RootFromJSON(&b, text, r) || !b.Lock() {
					return nil, nil, false
				}
				return b.AsConst(), b.Region(), true
			},
			reportarms.RootToJSONMeasure, reportarms.RootToJSON, reportarms.RootCookMeasure, reportarms.RootCookFrom,
			func(r *reportarms.TableReport) report {
				return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
			}),
		regionRow("reportscalars", "SimState",
			reportscalars.SimStateLoadMeasure, reportscalars.SimStateLoad, reportscalars.SimStateMeasure, reportscalars.SimStateSave,
			func(text []byte, r *reportscalars.TableReport) (*reportscalars.SimState, []byte, bool) {
				var b reportscalars.SimStateBuilder
				if !b.Init() {
					return nil, nil, false
				}
				defer b.Shutdown()
				if !reportscalars.SimStateFromJSON(&b, text, r) || !b.Lock() {
					return nil, nil, false
				}
				return b.AsConst(), b.Region(), true
			},
			reportscalars.SimStateToJSONMeasure, reportscalars.SimStateToJSON, reportscalars.SimStateCookMeasure, reportscalars.SimStateCookFrom,
			func(r *reportscalars.TableReport) report {
				return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
			}),
		regionRow("reportkeyed1", "Root",
			reportkeyed1.RootLoadMeasure, reportkeyed1.RootLoad, reportkeyed1.RootMeasure, reportkeyed1.RootSave,
			func(text []byte, r *reportkeyed1.TableReport) (*reportkeyed1.Root, []byte, bool) {
				var b reportkeyed1.RootBuilder
				if !b.Init() {
					return nil, nil, false
				}
				defer b.Shutdown()
				if !reportkeyed1.RootFromJSON(&b, text, r) || !b.Lock() {
					return nil, nil, false
				}
				return b.AsConst(), b.Region(), true
			},
			reportkeyed1.RootToJSONMeasure, reportkeyed1.RootToJSON, reportkeyed1.RootCookMeasure, reportkeyed1.RootCookFrom,
			func(r *reportkeyed1.TableReport) report {
				return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
			}),
		regionRow("reportkeyed2", "Root",
			reportkeyed2.RootLoadMeasure, reportkeyed2.RootLoad, reportkeyed2.RootMeasure, reportkeyed2.RootSave,
			func(text []byte, r *reportkeyed2.TableReport) (*reportkeyed2.Root, []byte, bool) {
				var b reportkeyed2.RootBuilder
				if !b.Init() {
					return nil, nil, false
				}
				defer b.Shutdown()
				if !reportkeyed2.RootFromJSON(&b, text, r) || !b.Lock() {
					return nil, nil, false
				}
				return b.AsConst(), b.Region(), true
			},
			reportkeyed2.RootToJSONMeasure, reportkeyed2.RootToJSON, reportkeyed2.RootCookMeasure, reportkeyed2.RootCookFrom,
			func(r *reportkeyed2.TableReport) report {
				return report{unknown: r.Unknown, widened: r.Widened, refused: r.Refused()}
			}),
	)
}
