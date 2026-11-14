---
name: go-style
description: Write and review plain, human-looking Go - paragraphs, linear flow, explicit ownership, short useful comments, no AI boilerplate. Use for any Go (.go) code you write, edit, or review, however small. Package layout and file organization are covered by go-architecture.
---

# Go style

Write Go the way a careful senior engineer would: plain, explicit, linear, and
easy to read a month later. Code must not look generated: no template comments,
no ceremony, no speculative abstractions.

Keep existing behavior and public contracts. Match the surrounding code unless it
conflicts with these rules. Do not reformat or rename unrelated code.
Package layout: see [go-architecture](../go-architecture/SKILL.md).

## 1. Paragraphs

Never emit a solid wall of statements. Split code into paragraphs by meaning:
setup, validation, main action, result. One blank line between groups, none inside.
A function over ~15 lines with no blank lines is wrong. Do not put a blank line
before every `if`; group by meaning, not mechanically.

```go
// BAD
func (c *Client) Fetch(ctx context.Context, id string) (Doc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/docs/"+id, nil)
	if err != nil {
		return Doc{}, fmt.Errorf("building request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Doc{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Doc{}, fmt.Errorf("unexpected status %s", resp.Status)
	}
	var doc Doc
	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return Doc{}, fmt.Errorf("decoding response: %w", err)
	}
	return doc, nil
}

// GOOD
func (c *Client) Fetch(ctx context.Context, id string) (Doc, error) {
	url := c.baseURL + "/docs/" + id

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Doc{}, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Doc{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Doc{}, fmt.Errorf("unexpected status %s", resp.Status)
	}

	var doc Doc
	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return Doc{}, fmt.Errorf("decoding response: %w", err)
	}

	return doc, nil
}
```

## 2. Errors

Check `err` on its own line right after the call. Never `if val, err := f(); err != nil`.
Declare first, then check. For an error-only call use `err = f()` (`:=` on first use).

```go
// BAD
if err := os.MkdirAll(dir, 0o750); err != nil {
	return fmt.Errorf("failed to create directory: %w", err)
}

// GOOD
err := os.MkdirAll(dir, 0o750)
if err != nil {
	return fmt.Errorf("creating %s: %w", dir, err)
}
```

- Messages say what was being done, lowercase, no "failed to" / "error while".
- Wrap with `%w` when the caller benefits from context. Do not wrap a plain
  forwarding return that adds nothing. Use `errors.Is` / `errors.As` to inspect.
- Either handle an error or return it. Do not log it and also return it.
- Never ignore an error silently. If it is intentional, say why in a comment.
  Never `raw, _ := json.Marshal(...)` without a reason.

## 3. Linear flow

- Early returns. No `else` after `return`, `break`, `continue`, or `panic`.
- Max 2-3 indentation levels. Flatten first; extract a helper only when its name
  describes a real operation. A helper called once with a trivial body is noise.
- One meaningful operation per line. Name intermediate results. No chained calls,
  no calls nested in arguments, no multi-step logic in one expression.
  Trivial inline expressions (`len(items)`) are fine.
- Never call a method on an inline struct literal: `(T{X: x}).Do()`.
  Assign it first, use a constructor, or make it a plain function.
- Name complex conditions: `canRetry := attempts < maxRetries && isTransient(err)`.
- No naked returns. No shadowing of `err` or `ctx`.

## 4. Keep ownership visible

Prefer return values and direct calls across component boundaries. Shared controls
must report intent instead of retaining closures that execute feature behavior.
Avoid hook bundles and functional options that conceal who mutates state, performs
an effect, or owns cancellation.

A small callback retained privately by its owning feature is fine when it keeps an
operation together. For example, a feature may keep its own form submission beside
the form fields, while shared controls return submitted values. Capture a snapshot
when later changes would make a retained value stale; keep replacement and cleanup
rules explicit. Do not pass that callback into the shared controls.

Do not replace a clear local function with command types, empty structs, enums,
interfaces, and dispatch methods merely to eliminate function values. Introduce a
type when it owns meaningful state or behavior that is actually reused. Use a small
consumer-side interface for real substitution, and a channel for asynchronous
results when its lifetime has an owner. Framework and stdlib callbacks are fine.

## 5. Explicit ownership

For every resource, goroutine, and mutable value, a reader must be able to say who
creates it, who mutates it, and who closes or stops it.

- The creator owns cleanup. The caller defers `Close`/`Stop` right after construction.
- One owner mutates a value. Do not share one mutable pointer, map, or slice between
  components; pass values, copy at the boundary, or give one type sole control.
- A mutex lives in the struct with the data it guards, with a comment saying so.
- Every goroutine has an owner that waits for it (`errgroup`, `WaitGroup`) and a
  `ctx` that stops it. No fire-and-forget goroutines.
- No global mutable state. No `init()` that sets up resources.
- Doc comments state ownership when it is not obvious: what is retained, copied,
  must be closed, safe for concurrent use.

```go
// Client owns its HTTP transport; call Close when done. Safe for concurrent use.
type Client struct {
	mu    sync.Mutex // guards cache
	cache map[string]Item

	http *http.Client
}

client := NewClient(cfg)
defer client.Close()
```

## 6. Data, not string switches

Do not model several variants as one struct with `Kind string` and then `switch o.Kind`
in many methods. Adding a variant must not mean editing six functions. Put the
differences in one table, or give each variant its own small type.

```go
// BAD: variant list repeated in ReadOnly, RetrySafe, Description, Schema...
type Operation struct{ Kind string }

func (o Operation) ReadOnly() bool {
	return o.Kind == "media_status" || o.Kind == "media_inspect"
}

// GOOD: one place lists the operations
type spec struct {
	name        string
	description string
	readOnly    bool
	retrySafe   bool
}

var specs = []spec{
	{
		name:        "media_inspect",
		description: inspectDescription,
		readOnly:    true,
		retrySafe:   true,
	},
}
```

Same family: no `map[string]any` for fixed shapes. Use a struct, or a static JSON
string constant for schemas. Never build a deeply nested literal on one line.

## 7. Types and APIs

- Concrete types and direct calls by default. An interface only for a real
  substitution boundary: defined at the consumer, 1-2 methods. Accept interfaces,
  return structs.
- Plain functions over stateless structs. No `Manager`, `Helper`, `Service`, `Util`,
  or universal `Env`/`Deps` types. No getters/setters; use exported fields.
- Config is a named struct with keyed fields, one per line. No functional options.
  Several positional strings or bools in a signature is a smell.
- Accept small duplication instead of sharing through mode flags, callback
  bundles, or type assertions. Generics and new dependencies only for a present, concrete need.
- No Java-style patterns: no Factory/AbstractFactory types, Builder chains
  (`.WithA().WithB().Build()`), Singleton (`GetInstance`), or
  Strategy/Observer/Visitor hierarchies. In Go a plain function, a struct, or a small
  consumer-side interface does the job. A constructor `NewX(cfg) *X` is fine;
  an `XFactory` type is not. Use a named pattern only if the user asks for it.
- No imitation of inheritance: no `Base` structs embedded to share behavior.
  Embed only to promote a real interface or a small, stable piece of state.

## 8. Naming

- Short names in short scopes (`i`, `p`, `ctx`); descriptive names in wide scopes.
- Use domain words. Avoid empty names: `data`, `info`, `result`, `item`, `handle`,
  `process`, `do`, `manager` - unless that is truly what the thing is.
- No stutter: `sync.Client`, not `sync.SyncClient`. No type in the name
  (`userStruct`, `nameString`).

## 9. Comments

Plain `//` above the declaration. Short. A comment must tell something the code
cannot: why a choice was made, ownership, concurrency, cancellation, nil/zero
meaning, units, a hidden constraint.

- Exported names: doc comment starting with the name, one line, only if it adds
  something beyond the name and signature. Never restate the name.
- Unexported code: comment only the non-obvious. No narration of steps.
- If a comment explains tangled flow, simplify the flow instead.
- No banners, no section dividers, no emoji, no change history, no TODO without a
  concrete condition.

```go
// BAD
// NewClient creates a new Client with the given config.
// Step 1: validate the config.
// Step 2: create the HTTP client.

// GOOD
// NewClient opens a connection pool; call Close when done.

// Retries are limited to GET: the API is not idempotent for writes.
```

## 10. Don't write like a template

Tells of generated code. Remove them:

- Comments that restate the next line; `// Create a new X`; `// Helper function to...`.
- Defensive checks for impossible cases: nil-checks on values that cannot be nil,
  re-validating already validated input, copying data "just in case".
- Speculative abstractions: extension points, unused parameters, options nobody
  passes, interfaces with one implementation, `Option` types for one setting.
- Ceremony: constructors that only assign fields, wrappers that only forward,
  one-use type aliases, `util.go` / `helpers.go` / `common.go` dumping grounds.
- Uniform symmetric structure when the cases differ.
- Features, flags, or logging nobody asked for.

Keep the setup, validation, action, and result of one operation close together.
A longer cohesive function or file can be easier to read than a chain of tiny
helpers. Extract when it hides a responsibility or avoids meaningful repetition,
not to meet a line target. Compare the ordinary reading path before and after.

Write only what the task needs. If a line has no job, delete it.

## 11. Small things agents get wrong

- Magic numbers and strings: name them as constants; use a typed constant instead
  of a bare string for a closed set of values.
- Boolean or several same-typed positional parameters (`Run(ctx, true, false)`):
  use a config struct or two named functions.
- Pointers to small values or for "optional" (`*string`, `*int`) unless nil
  genuinely means something. Make the zero value useful where possible.
- Do not store `context.Context` in a struct. Pass it per call.
- No `panic` for control flow or ordinary errors. Only for programmer errors at startup.
- Sentinel errors and custom error types only when a caller actually inspects them.
  Otherwise a plain `fmt.Errorf` is enough.
- `defer` inside a loop: extract the body into a function.
- No dead code: remove unused functions, parameters, imports, and commented-out blocks.
  No placeholder stubs (`// TODO: implement`, `return nil`) presented as finished.
- Receivers: pointer receiver when the method mutates or the type holds a lock or
  large state; keep one kind per type.
- Do not rewrite a whole file for a small change. Make the smallest diff that does the job.
- Do not add a dependency for something the stdlib does in a few lines.
- No reflection or type switches where a concrete type or a small interface works.

## 12. Stdlib, context

- `ctx context.Context` is the first parameter when the function makes a network call,
  runs a subprocess, waits on a channel or lock, starts goroutines, or calls something
  that takes a `ctx`. Do not add it "just in case" or pass it to functions that never
  use it; plain file I/O does not need one.
- Use `any`, `slices`, `maps`, `cmp`, `min`/`max`, `for range n` where they replace
  boilerplate, not for their own sake.

## 13. Before finishing

Read the ordinary path like a maintainer: paragraphs, error handling, nesting,
names, ownership, and no template tells. Before removing a branch, lock, or cache,
understand why it exists (validation, ordering, cancellation, persistence format,
resource limits). Run `gofmt`, `go vet`, and affected tests; add `-race` when
concurrency changed. Keep the diff focused.
