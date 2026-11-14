---
name: go-architecture
description: Package boundaries, directory layout, file organization, and test placement for Go. Use when creating or splitting packages, reorganizing files, changing feature ownership, or when a package has grown into a flat pile of files. Not needed for ordinary code edits; statement-level style is in go-style.
---

# Go architecture

A maintainer should find a feature, follow its ordinary path, and change it without
understanding the whole app. This skill owns package boundaries and file layout;
code-level rules are in [go-style](../go-style/SKILL.md). For a refactor apply both.
A small edit does not justify reorganizing its package. Stay within the requested scope.

## 1. Group by feature, not flat, not layered

- Group code by feature or responsibility into directories. Each directory is a
  package with a clear entry point and a small public surface. The entry point is
  a type when the package owns state, resources, or a lifetime, and plain functions
  when it is stateless. Do not invent a type just to have one.
- Do not pile dozens of files into one package. Repeated filename prefixes
  (`sync_client.go`, `sync_store.go`, `sync_diff.go`) and two unrelated
  responsibilities in one package suggest a missing directory.
- File counts are review signals, not rules: around 8-10 files in a package is a
  reason to look at it, not a reason to split it. Split only along a real
  responsibility boundary (section 3). Never cut a cohesive package into pieces
  just to get smaller directories.
- Do not create technical layers (`models/`, `services/`, `utils/`, `common/`) or a
  package per type. A package of one or two small files rarely earns its own directory.
- Use `internal/` for everything that is not a public API.
- No Repository/Service/Controller layers and no DI containers or "provider"
  frameworks. Wire dependencies explicitly in `app/` (or `main`) with plain constructors.
- Package names: short, lowercase, one word, no underscores, a noun for what the
  package is. No `utils`, `common`, `helpers`, `base`. Do not repeat the package name
  in its identifiers (`sync.Client`, not `sync.SyncClient`).

```
cmd/app/main.go
internal/
  app/              wiring and lifecycle only
  library/          feature: documents
    library.go      package doc, entry type, constructor
    store.go
    search.go
  sync/             feature: remote sync
    sync.go
    client.go
    diff.go
```

## 2. Start at `<package>.go`

Prefer a substantive starting file named after the package: `agent.go` for
`package agent`, `main.go` for `package main`. It holds the package introduction and the main public operations:
the entry type and its constructor if the package owns state, or the primary
functions if it is stateless. Keep large state and responsibility-specific behavior
in companion files named by responsibility (`model.go`, `store.go`). The root file is
where reading starts. Keep declarations beside the behavior they explain when
moving them to the root would make an ordinary change harder to follow.

Do not add an empty marker file or forwarding wrappers to satisfy the name; rename
a real file instead. Respect generated files and build constraints. Do not impose
this on unrelated packages during a local edit. The filename is a navigation
convention, not evidence that a package or split deserves to exist.

## 3. Files or packages?

- Split a **file** when the package still owns the behavior but a reader must hold
  too many responsibilities at once. Keep the opening, validation, application,
  and failure path of one interaction together. Several hundred lines can be fine
  for a cohesive responsibility; there is no target file length.
- Merge files that a maintainer repeatedly opens together for the same operation.
  Do not separate declarations, dispatch, and implementation just because they
  are different technical categories. Peer features do not need matching files.
- Extract a **package** only when it hides a coherent responsibility behind a
  smaller API. Before extracting, sketch its public operations, dependencies,
  results, and cleanup, then trace one ordinary call and its failure path across
  the new boundary. If the caller still needs most of the old internals, fix
  ownership first.
- Moving tangled methods into several files, or inventing an interface, owner type,
  or callback bundle just to make a layout possible, is not a fix.

## 4. Ownership across boundaries

- Each package owns its state, resources, and cancellation. Peer features do not
  host each other; coordination between peers lives in their parent.
- Export operations and facts callers need. Do not export mutable internals or add
  forwarding types just to make a move compile or to keep old tests working.
- Share behavior only when semantics truly agree. Accept modest duplication over
  mode flags and callback bundles. A shared package must not switch on feature
  names or exist only to break an import cycle.
- Prefer concrete owners and direct calls. No universal dependency bags.
- A feature may privately retain a small callback for its own local operation.
  Shared packages report results or intent; they do not retain feature callbacks.
  Preserve this boundary without inventing command hierarchies to avoid functions.
- Model actual lifetimes. One active operation usually needs one pending value,
  not a registry or map. Keep separate state for a suspended operation or multiple
  outstanding requests only when those lifetimes really coexist.

## 5. Preserve lifecycle and behavior

Before simplifying, understand validation, permissions, ordering, synchronization,
persistence, cancellation, and resource limits. Preserve wire formats and
user-visible behavior. When moving an owner of goroutines or resources, trace
startup, completion, cancellation, and cleanup together; keep cleanup on
construction failure, and wait for owned work to stop. No compatibility wrappers
for internal renames unless real callers require them.

## 6. Tests

- Feature tests live with the feature; parent tests cover coordination and shared
  lifecycle. A child's name in a test filename does not decide ownership.
- Use real constructors. Fakes only at real dependency boundaries; do not recreate
  production behavior in fixtures or export internals for tests.
- Keep assertions when moving tests. Wait for async work before releasing test
  resources.

## 7. Done check

- Can a reader open `<package>.go` and find the public operations? Is there a type only where something is actually owned?
- Do directory and file names show where a feature is changed? Can an ordinary
  operation be followed in one or two cohesive implementation files?
- Did a split reduce the concepts and files a maintainer must consult? Update
  affected reading guides when files move; do not require cosmetic symmetry.
- Is every mutable value and resource owned explicitly?
- Is the ordinary path easier to follow, with fewer concepts held at once (not just
  fewer lines or files)?
- Were behavior, assertions, and required checks preserved? (`gofmt`, `go vet`,
  affected tests, `-race` if concurrency changed.)

If the refactor only moved complexity around, say so and revise.
