---
name: docs-pattern
description: Establish or apply Keshon's shared repository documentation pattern, using Melodix as the reference. Use for cross-project docs consistency, documentation restructuring, or deciding where new documentation belongs. Ordinary feature edits only need the affected document.
---

# Repository documentation pattern

Use Melodix's separation of readers and responsibilities as the reference.
Adapt the number of documents to the project. A small script may need only a
README; a service with deployment and recovery procedures needs more.
Consistency means that readers find a subject in the same kind of document,
not that every repository has the same files or headings.

## Document ownership

| Document | Reader's question | Canonical content |
|---|---|---|
| `README.md` | What is this, and how do I try it? | Purpose, supported capabilities, relevant limitations, minimal working start, links to guides, license. Screenshots or demos when useful. |
| `docs/running.md` | How do I operate it? | Requirements, complete setup, configuration, ordinary use, persistent data and routine operations. |
| `docs/architecture.md` | How does it work? | Components and boundaries, important flows, state ownership and lifetime, failure domains, current design tradeoffs. |
| `docs/conventions.md` | How do I change it correctly? | Development setup/checks, naming and extension rules, contributor workflows and verification. |
| Specialist guide | How do I complete this distinct task? | A substantial client, routing, recovery, API or deployment procedure that would interrupt the main guide. |
| Package-local README | How do I consume this module? | Its public interface, minimal example and package-specific constraints; link back for system architecture. |
| `docs/internal/` | What evidence led to this decision? | Dated investigations, measurements and retired approaches, marked historical. |

Existing useful paths such as `ARCHITECTURE.md` or `docker/README.md` can stay.
Do not rename solely for uniform spelling. Add `docs/README.md` only when the
README navigation no longer makes the guides discoverable.

An ownership document is optional: use it for substantial lifecycle/concurrency
contracts with distinct checks, not as a compulsory template. Brand, security,
command and protocol guides are likewise project-specific.

## Canonical facts and navigation

- Give each subject one maintained home. A summary in the README links to it;
  detailed instructions and configuration tables are not copied there.
- Treat source schemas, package manifests and compatibility files as the
  authorities for identifiers, defaults and supported versions. A documentation
  table may explain them; it does not define a second version policy.
- Link every public guide from the README, directly or through a task index.
  Use relative repository links. Link to a specific section when it saves the
  reader searching a long guide.
- Keep generated examples and catalogs generated. Find templates, generators
  and documentation-reading tests before changing their output or structure.
  A collapsible generated catalog can stay in the README when moving it would
  require changing the generator; do not build tooling merely to shorten it.
- Preserve useful historical evidence outside current instructions. Link to
  existing history where sufficient; do not create a diary for routine edits.
- Before retiring a path, inspect code, packaging, update manifests and incoming
  links. Keep a short forwarding page where path removal has a compatibility
  cost. Do not retain a second full copy.

## Writing and evidence

Use the documentation's existing language. Write concrete behavior, commands
and limitations. Avoid marketing, claims of superior design, defensive prose
and repeated warnings. Preserve useful personality in project introductions
and demos; operational and reference sections should stay direct. Preserve the
README's search vocabulary: project category, language, supported services,
protocols and distinctive capabilities should remain in meaningful sentences.
Merge structural improvements into a strong existing introduction rather than
replacing it with a generic summary. Do not add keyword lists or SEO claims.

For mutating procedures, state where to run them, prerequisites, state or
services affected, how to verify success, and available recovery. Distinguish
fresh installation from an existing deployment and operator configuration from
generated state. Keep credentials out of examples and output artifacts.

Troubleshooting follows symptom, plausible causes, discriminating checks,
then recovery. A past incident is evidence for a hypothesis, not a universal
diagnosis. State the vantage point and protocol coverage of health checks.

Contributor rules distinguish:

- **Enforced:** name the actual runtime guard, compiler constraint or check.
  Describe what it catches and its material limits. A test's existence alone
  does not establish semantic coverage or prove it runs in CI.
- **Invariant:** state what breaks if it is violated; enforcement may be review.
- **Practice:** identify a maintainability preference without implying a guard.

Preserve a repository's existing equivalent labels and checked tag syntax.
Do not invent tests, CI coverage, benchmark results or compatibility guarantees.
Keep detailed API contracts near the API; link from conventions instead of
restating them in several places.

## Applying the pattern

1. Inspect the authorized repositories, their instructions, working trees,
   document links, generators, schemas and documentation checks. Comparing a
   reference repository does not authorize editing it or propagating changes
   to other repositories.
2. Present concrete drift and a proposed ownership map before a broad rewrite.
   Scale the audit to the change; do not manufacture a quota of findings or a
   new approval step after the user has approved the scope and structure.
3. Edit canonical documents first, then README navigation and forwarding pages.
   Keep code changes outside a documentation-only assignment. If a generator
   prevents a proposed move, preserve the contract or explain the dependency.
4. Verify relative links and heading fragments, generated output, referenced
   flags/defaults and relevant existing checks. Use safe local previews; do
   not deploy or run operational examples against live services just to verify
   prose. Report skipped checks and unresolved facts explicitly.
5. Report which repositories changed, substantive corrections and validation.
   Commit, push and deployment follow the user's authorization for this task.

For Melodix specifically, edit `README.md.tmpl` and run `go run ./cmd/readme`.
`internal/conventions` reads convention tags, configuration tables and ownership
test references; preserve their contracts. These are reference-project details,
not requirements to add the same machinery to another repository.

## Repository copies

This skill is versioned at `.agents/skills/docs-pattern/SKILL.md` in Melodix,
Server Domme and 3xui-reality-stack. Melodix is the reference copy for shared
pattern changes. When authorized to propagate a revision, compare the copies
and carry the same shared text into the named repositories. Project-specific
commands and checks belong in each repository's `docs/conventions.md`.
Do not depend on a machine-local installed skill or an absolute local path.
A personal installation is an optional convenience copy.
