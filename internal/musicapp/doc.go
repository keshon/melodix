// Package musicapp is the application facade for music use cases: history pages and enqueue
// orchestration. Transports (Discord slash, CLI REPL, tests) should depend on [Facade] or [*Music],
// not on internal/history or internal/playinput directly for these flows.
//
// Lower-level packages remain available for formatting helpers and unit tests.
package musicapp
