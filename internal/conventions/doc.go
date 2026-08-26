// Package conventions turns the checkable half of docs/conventions.md into a
// test, so the document's claims are facts rather than intentions. It has no
// runtime role and nothing imports it.
//
// The rules it enforces are marked [enforced] in the document. A rule belongs
// here only if violating it can be detected mechanically; the rest stay prose
// and are enforced by review, which the document says plainly rather than
// implying tooling that does not exist.
//
// See conventions_test.go for the rules and baseline.json for what the
// codebase owed when they were introduced.
package conventions
