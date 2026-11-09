# Melodix documentation audit and migration plan

Date: 2026-11-09. Baseline: `adabca96fa98f4d4240f222c47631fd568155d70`.
Status: migration approved on 2026-11-09 and implemented locally. Commit, push and production deployment have not been requested for this migration.

## Recommendation

Keep Melodix's existing document ownership and paths. Improve the route from
installation to first playback, give ordinary playback and troubleshooting their
own guides, and make the existing Jekyll documentation part of the landing site.
Retain the generated command catalog and the checked contributor references.

Melodix already has a coherent starting structure. Its strongest material is the
explicit account of lifecycle contracts and what the tests actually enforce.
The problems are uneven reader priorities, repeated implementation explanations,
some factual drift, and an implicit website publication boundary. A wholesale
move into GEKTOR's directory tree would create compatibility work with little
benefit. A new Vite application is unnecessary: GitHub Pages already renders
these Markdown sources with Jekyll.

## Scope and evidence

The working tree was clean at the baseline. Read all 11 tracked Markdown or
Markdown-template files in full, including the operational skill and historical
notes; those two categories are not treated as prose to rewrite. Also read the
landing page's visible copy, metadata, interactive parser descriptions, both
example environment files, Docker configuration and deployment script, root
launch/check scripts, README generator, documentation-reading tests, relevant
entry points, help implementations and playback contracts. Read both licenses.

Checked the live homepage, generated setup/contributor/library/Docker routes,
raw Markdown delivery, metadata and historical-page exposure. The homepage
matches the local source after newline normalization. GitHub's Pages API reports
legacy Jekyll publication from `main:/`. The latest release is `v2026.09.21`;
current development documentation must not silently imply release parity.

The human-first editorial method follows the current
[Humanizer instructions](https://github.com/blader/humanizer/blob/main/SKILL.md)
(3.1.0), with neutral technical prose and preserved facts, identifiers, commands,
contracts and examples. The repository's
[documentation pattern](../../.agents/skills/docs-pattern/SKILL.md) also applies.
Its useful distinctions remain; its operational text stays unchanged.

### Inventory and treatment

Approximate sizes use the checked-out UTF-8 files. Word counts include code and
tables and are inventory aids, not readability scores. Each heading sequence
below identifies the reviewed content and its destination.

- `README.md`: 10.2 KB, 1,398 words, generated reader entry point. Headings:
  introduction; capabilities; demos; limits; start; Discord/terminal commands;
  internals; documentation; license. Regenerate from the template. Keep demos,
  useful product vocabulary and the collapsed catalog. Link tasks instead of
  expanding operational detail here.
- `README.md.tmpl`: 8.4 KB, 1,122 words, maintained source for that entry point.
  Keep path and both catalog placeholders. Edit the introduction/setup/navigation
  here, then run the existing generator. The output is not a second maintained
  article.
- `docs/running.md`: 10.2 KB, 1,532 words. Requirements; yt-dlp/JavaScript;
  proxies; create/invite/configure/run bot; CLI; Docker; first-start verification;
  data/restarts. Keep setup, configuration and data ownership here. Move launch
  and verification before optional tuning. Move symptom diagnosis into the new
  troubleshooting guide, with links back to requirements and configuration.
- `docker/README.md`: 2.9 KB, 386 words. Prerequisites; configuration; deployment.
  Keep adjacent to Compose. Make fresh setup, rebuild/update, data mount,
  verification and rollback consequences explicit. Replace the long variable
  enumeration with the shared reference and container-specific overrides.
- `pkg/music/README.md`: 13.2 KB, 1,908 words. Pipeline; install; quick start;
  six algorithm stages; Discord UI/errors; cache/buffer; extension points;
  requirements; package links; license. Keep as the library-consumer entry.
  Bring prerequisites and a complete example before the implementation tour.
  Consolidate system algorithms into architecture and Discord UI details into
  architecture's status-delivery section. Retain consumer obligations near the
  example, especially status consumption and cleanup.
- `docs/architecture.md`: 30.7 KB, 4,378 words. Overview; design goals; glossary;
  package map; extension interfaces; resolution/fallback; cache/buffer; playback;
  status delivery; three failure domains; commands; persistence; extension links;
  limits; history/ownership/testing links. Keep the path and subject ownership.
  Add a compact topic index, correct stale sink/build details and shorten
  repeated incident narratives. Keep diagrams and technical explanations useful
  to someone changing the system. Detailed measured rationale already owned by
  source comments remains there, with a link.
- `docs/conventions.md`: 27.4 KB, 4,216 words. Tier legend; design principles;
  naming; frozen identifiers; concurrency; errors/logging; comments; extensions;
  tests/manual checks; formatting/CI; releases; documentation. Keep the path,
  enforcement tags and check coverage. Put development commands and the route
  for a first change near the start. Keep naming/extension/release reference
  sections below them. Trim repeated arguments and historical narratives around
  rules without losing the specific failure each rule prevents.
- `docs/ownership.md`: 15.0 KB, 2,294 words. Enforcement grades; 13 numbered
  lifecycle contracts; known gaps; adding a rule. Keep the path, rule numbers,
  test references, enforcement distinctions and known limits. Replace the
  language-comparison introduction with the reference's practical purpose;
  use a short topic index and consistent rule/why/enforcement presentation.
- `docs/brand.md`: 2.7 KB, 458 words. Mark; colours; favicons. Keep specialist
  source and asset paths. Distinguish mark fallback colours from actual website
  tokens rather than claiming they already match. Retain small-size constraints.
- `docs/internal/implementation-notes.md`: 1.9 KB, 309 words. Historical failed
  approaches and measurements. Keep historical and out of public docs navigation
  and the generated site. Link to GitHub history/source when appropriate.
- `.agents/skills/docs-pattern/SKILL.md`: 7.5 KB, 1,063 words. Operational
  documentation skill, not a public article. Freeze; do not propagate changes
  to its copies in other repositories.

Additional surfaces:

- `index.html`: 75.8 KB including CSS/JS; homepage, simulated playback demo,
  parser tabs, setup, commands, download matrix, FAQ and JSON-LD. Keep the visual
  design and interactions; align factual claims and guide links. Its initial
  parser description is repeated in JS, and FAQ text is repeated in JSON-LD:
  both copies must be reconciled during changes.
- `.env.example`, `docker/.env.example`: maintained operator examples, not
  defaults authorities. Preserve configuration coverage and clearly label
  intentional deployment overrides.
- `robots.txt`, `sitemap.xml`, `CNAME`: existing root-hosted publication inputs.
  Preserve domain and crawler policy; expand the sitemap to approved pages when
  publication is consolidated. Do not invent modification dates.
- `LICENSE`, `pkg/music/LICENSE`: preserve legal text without editorial cleanup.
- Go package/API comments and runtime help: inspect when they support a claim;
  retain near their implementations. This is not a repository-wide comment rewrite.
- Ignored `.env`, binaries, logs and data are local runtime material. Do not read
  credentials, package those files or treat them as documentation sources. No
  generated docs directory or separate site-source tree was found.

## Findings

### 1. First playback arrives too late

`docs/running.md:21` starts yt-dlp failure details and `:42` starts proxies
before bot creation at `:74`. A 26-variable table at `:110` separates minimal
configuration from the launch step at `:140`. Both bot and terminal launch
examples compile from source even though releases are introduced as a normal
entry path. Windows downloads exist but there is no equivalent Windows launch
sequence. A novice must assemble the actual release instructions from several
places.

Treatment: complete extracted-release paths for Windows and Unix, then the
source-build variant. Move launch and a short playback check immediately after
the minimal configuration. Put the full variable reference later in this same
file, grouped by purpose. Keep `#configuration` and the table row shape because
tests consume them. Separate mandatory prerequisites from optional capabilities.

### 2. The homepage promises more than the implementation

`index.html:1792` says queues survive restarts. `docs/running.md:195` correctly
says session restarts preserve them and process restarts lose them. The player's
queue is in memory, while persistent storage records history/settings/cache
index; restarting the process does not reconstruct the queue.

`index.html`'s thin-connection advice also assumes Discord carries 64 kbps.
`internal/config/config.go:79` and the running reference explain why encrypted
voice relays the selected source bitrate instead. The source setup snippet
omits `INIT_SLASH_COMMANDS=true`, whose code default is false. The extracted
release's supplied `.env` does enable registration, so those two paths need
different explanations.

Treatment: qualify reconnect recovery, remove the fixed-bitrate explanation,
and make the source snippet complete. Bound claims such as “Nobody can revoke
it” to control of hosting and updates; self-hosting still depends on Discord and
media services. Keep the recovery demonstrations, with clear illustrative status.

### 3. Reader tasks and internal explanations repeat

The README's playback examples, homepage commands and in-app help serve related
but different needs. They can stay concise summaries with links. The longer
duplication is between `pkg/music/README.md`'s algorithms/Discord UI discussion
and architecture's pipeline, recovery and status sections. This creates several
places to update an implementation change.

Treatment: a new playback guide owns everyday examples and semantics. Command
names/descriptions/options remain owned by catalogs and runtime help, with the
existing generated listing retained in the README. Architecture owns system
algorithms; the library README owns how a Go consumer starts, observes and stops
playback. Ownership remains the contract reference. Do not create a second
hand-maintained exhaustive command catalog.

### 4. Troubleshooting lacks an entry point

Useful diagnostic details are scattered across requirements, architecture,
environment comments and logs. The `-check` option in `cmd/discord/main.go:29`
is absent from the human guides. `internal/discord/session/check.go` shows what
it actually checks: gateway READY, identity/guilds, heartbeat latency and a
command-list REST request for a cached guild when one exists. It does not join
voice, prove UDP/DAVE audio, or verify every guild's permissions.

Treatment: a symptom-first guide covering missing commands, permission refusal,
bot connected without audio, interrupted media/live playback, storage locking,
and Docker settings not taking effect. Each section has discriminating checks,
then a bounded recovery action. Document `-check` as networked diagnostics using
the configured token, not an offline self-test or proof of voice playback.

### 5. Some specialist prose describes previous code

`docs/architecture.md:141` attributes six-target release builds to
`CGO_ENABLED=0`; both workflows explicitly enable CGO for Linux. Its playback
mechanics describe `OpusSend`, but the current sink installs an
`OpusFrameProvider` through `SetOpusFrameProvider`. Ownership rules 8 and 9
already describe the current pull-based sender and DAVE hold obligations.

Treatment: reconcile architecture with the implementation and those contracts,
then keep source-level protocol details at their existing owners. Preserve
historical explanations only where they explain a current constraint.

Important non-finding: the library README says both optional layers are off by
default. That is true for direct library consumption: the package starts with
zero read-ahead, while the application applies a 30-second config default through
`internal/music/layers.go`. Explain the boundary instead of “correcting” one
default to match the other.

### 6. Docker overrides and update effects are underexplained

Compose and its example set read-ahead to 10 seconds, while application config
defaults to 30 seconds. This is an actual deployment override, not necessarily
an error. Compose also forwards `COMMAND_TIMEOUT`, which config no longer reads.
Existing coverage tests check variable names/default claims in docs and examples,
but do not establish complete Compose parity.

`docker/build-n-deploy.sh` deletes/reclones the source directory in Git mode,
stops the stack, removes the old image before rebuilding and prunes dangling
images. The guide describes a convenient deployment without explaining downtime
or how a failed rebuild affects recovery.

Treatment: document the 10-second override and effective configuration checks.
Explain the script's actual effects, preserving its existing behavior in this
documentation assignment. Describe a recoverable update procedure only after it
has been validated in an isolated stack. Removing the unused Compose setting or
changing deployment defaults is a separate configuration cleanup, not a prose edit.

### 7. Releases cannot follow their own documentation links offline

Both workflow bundle steps copy README, license, `.env` and assets, but no
`docs/` or package/Docker READMEs. The supplied README points to those missing
files. The homepage says bundles contain “the docs”, which overstates the
current selection. This finding comes from workflow selection; no release ZIP
was downloaded or unpacked during this audit.

Treatment: bundle the maintained human docs from the same release revision,
exclude internal history and skills, and rewrite links to omitted source files
to that immutable revision on GitHub. Keep local human-document links usable
offline. Explain that external links and embedded remote demos need a network.
Apply the same packaging selection to build artifacts and tagged releases.

### 8. The website already publishes docs, without a maintained docs surface

Live `/docs/running.html`, `/docs/architecture.html`, `/docs/conventions.html`,
`/docs/ownership.html`, `/docker/` and `/pkg/music/` return generated HTML.
The live setup page has a generic repository header, no docs topic navigation,
and a long desktop reading column. Mermaid blocks are not provided a renderer
by the current default layout. This was a desktop inspection; narrow-screen
behavior has not been tested.

`/docs/internal/implementation-notes.html` also returns 200. “Internal” in a path
does not exclude it from Jekyll. Generated docs inherit the repository description
containing “Reliable as AK-47 (actually no)” in description/Open Graph/JSON-LD;
the standalone homepage has its own, different metadata. The current sitemap
lists only the homepage with a manually written date.

Treatment: configure the existing Jekyll publisher, explicitly exclude internal
material and templates, and add one docs layout/navigation layer. Give published
pages accurate descriptions. Include approved canonical HTML routes in the
sitemap. Raw `/docs/running.md` already returns Markdown, so agent access does
not require a duplicate Markdown export system.

## Reader journeys

1. Prospective user: landing or README -> capabilities and relevant limits ->
   matching release -> running guide -> first audible track -> playback tasks.
2. Server operator: running guide's data/config sections -> backup or update
   procedure; Docker operators branch to the adjacent Docker guide.
3. User with a failure: troubleshooting -> symptom checks -> relevant setting or
   dependency -> verification. Architecture is optional background.
4. Contributor: conventions -> local checks and generated-file rules -> relevant
   architecture/ownership section -> edit -> focused tests and manual acceptance.
5. Library consumer: package README -> prerequisites -> complete runnable example
   -> lifecycle/status contracts -> package API reference and extension points.

## Proposed source layout

```text
README.md.tmpl                   existing introduction and generated catalog source
README.md                        existing generated entry point
index.html                       existing landing page, factual copy and guide links
docs/
  README.md                      NEW task index; also the website's /docs/ entry
  running.md                     install, first playback, config reference, data/updates
  playback.md                    NEW search, playlists, queue, history and command use
  troubleshooting.md             NEW symptom-led diagnosis and recovery
  architecture.md                existing system explanation and technical diagrams
  conventions.md                 existing contributor workflow and checked rules
  ownership.md                   existing numbered lifecycle contract reference
  brand.md                       existing asset/mark guidance
  internal/
    implementation-notes.md       historical, excluded from generated site
    docs-audit-plan.md            this proposal, excluded from generated site
docker/README.md                 existing container-specific operations
pkg/music/README.md              existing Go library entry point
.agents/skills/docs-pattern/      unchanged operational guidance
```

Three new human pages have distinct jobs: a site/repository task index, ordinary
playback and failure diagnosis. Keep configuration in `running.md`; a separate
configuration file would add consumers/compatibility work without removing much
content. No wholesale `guide/`, `reference/`, `development/` migration is needed.

The running page's order becomes: choose release or source -> minimal prerequisites
-> create/invite bot -> configure -> launch -> verify -> terminal alternative ->
optional dependency/proxy detail -> data/updates -> grouped configuration lookup.
Keep existing anchors when headings move; retain explicit IDs when renaming them.

## Website, source and release boundaries

Use current Markdown sources for GitHub, Jekyll and archives. The manifest for
navigation/metadata contains only paths, routes, titles, sections and descriptions.
It contains no second copies of articles.

Recommended small site additions, subject to the publication proof below:

- `_config.yml`: site identity/base URL, docs layout defaults and explicit
  exclusions for `docs/internal`, templates and operational material.
- `_data/docs.json`: the 10 approved article/index entries (eight under `docs/`,
  plus Docker and library). Add only the narrow `.gitignore` exception needed
  for this file; the repository currently ignores `*.json`.
- `_layouts/docs.html`, `_includes/docs-navigation.html`, `assets/docs.css`:
  shared article shell, task navigation, readable measure and overflow-safe
  tables/code. Preserve the landing design; no animation or product redesign.
- A small publication/check script under `scripts/`, with a brief adjacent README
  if needed. Reuse existing Jekyll to build; do not add a Node app or CMS.

Prove Jekyll's route rewriting, raw Markdown retention, exclusions and source
link behavior before fixing the implementation details. GitHub Pages currently
rewrites some Markdown links but leaves the generated README command link as
`/README.md#commands`; the published counterpart must lead to the intended
human-readable section. Test HTML and raw-source delivery separately.

Keep current `.html` article URLs, `/docker/`, `/pkg/music/`, and source paths.
Do not introduce directory-style replacements for all existing docs. If any
route must change, emit a redirect/forwarding page and preserve old anchors.
Existing internal HTML can disappear from the site because it is deliberately
historical; link its public GitHub source when needed. This is publication
organization, not a claim that committed internal notes are secret.

Validate the generated article set against the publication list; configuration
exclusions must actually prevent raw and rendered historical/template output.
Do not treat navigation membership alone as a publication boundary. If legacy
Pages cannot enforce the required output validation, use a dedicated Jekyll Pages
workflow that checks and uploads `_site`. That is a bounded delivery change,
not a reason to replace Jekyll. Build proof must settle this before migration.

Add an optional generated `/llms.txt` from that same publication list, linked
to the existing clean Markdown endpoints. The file should let an agent choose
individual pages. Add HTML discovery links and check every destination. No
`llms-full.txt`, duplicate prose, or promise of ranking gains. Account for the
existing `*.txt` ignore rule with a specific exception. Keep robots policy open
unless separately changed by the owner.

Archives contain README, licenses, public docs, Docker/library README entry
points and their required images at their existing relative paths. Exclude
this plan, historical notes, operational skills, real configuration and data.
Use source-pinned GitHub fallbacks for omitted implementation files. A plain
Markdown archive remains useful without the website or a browser script.

Website docs describe a named development revision; bundled docs describe the
release revision. Keep both labels visible. No version selector or old-release
hosting is required. Existing released ZIPs remain as they were.

## Affected non-prose consumers

- `cmd/readme/main.go`, `internal/readme/{readme,discord,cli}.go`,
  `build-readme.bat`: keep generator behavior and placeholders. Regenerate README
  after template edits; verify a second generation has no diff. No generator
  refactor is needed to retain the collapsed catalog.
- `internal/conventions/config_doc_test.go`: keep reading the table in
  `docs/running.md`. Grouping tables must preserve all 26 keys, third-column
  defaults and regex-compatible rows. No coverage relaxation.
- `internal/discord/perm/recommended_test.go`: keep a real OAuth invitation URL
  with its permissions mask in `docs/running.md`; its test reads this exact path.
- `internal/conventions/conventions_test.go`: preserve `project.docPath`, every
  `**[enforced: name]**` tag and the required claims in tagged paragraphs.
  Rewording an enforced paragraph changes build failure text; review it as a
  checked interface, not cosmetic prose.
- `internal/conventions/ownership_doc_test.go`: preserve the ownership path and
  genuine test references. Keeping names real does not prove the tests enforce
  every claim; retain the document's limits.
- `.github/workflows/build.yml` and `release.yml`: use one packaging selection
  and link-rewrite implementation for both bundle steps. Preserve binaries,
  asset placement, `.env` first-start behavior and tag-derived release notes.
- `index.html`: update guide navigation, setup and recovery copy; reconcile
  initial HTML/parser-tab JS and visible FAQ/JSON-LD. Keep six release download
  URLs and existing demo interactions.
- `robots.txt`, `sitemap.xml`, `.gitignore` and proposed Jekyll inputs: maintain
  production identity, metadata, explicit publication and narrowly scoped ignore
  exceptions. No Search Console or analytics account changes.
- Root `.env.example`, Docker example/Compose/Dockerfile and runtime help are
  authorities to check, not targets for a redesign. Document existing overrides.
  Application/source comments change only if a named factual doc comment must be
  corrected; no bulk comment editing.
- Brand SVGs, favicon variants, GIF/WebP media, the reusable skill, licenses,
  persisted identifiers and protocol contracts remain unchanged.

## Editorial examples

These are proposed openings and corrections, not edits already made.

### Running guide

Current: “Melodix runs two ways: as a Discord bot, or as a standalone CLI player.
This guide covers both.” Optional extractor/proxy details then precede setup.

Proposed opening:

> Run Melodix from an extracted release or build it from source. The Discord bot
> needs your bot token; the terminal player plays through your speakers without
> a Discord account. Choose your platform below and start one track before
> changing optional settings.

The next sections supply complete Windows and Unix release steps, not another
paragraph introducing the document. Put the source-build command in its own
variant. Confirm audio and stop behavior before presenting the full reference.

### Ownership reference

Current introduction compares Go with Rust/Erlang and argues that documented
rules will be broken by the next person in a hurry.

Proposed opening:

> These contracts define who can change playback and voice state, how long that
> state lives, and which checks protect it. Use the numbered rules when changing
> a player, stream, session or status message. Each rule names its enforcement
> and any known gap.

Keep the actual rules and named failures. Remove the language comparison and
general claims about maintainer behavior.

### Homepage recovery statement

Current: “Queues survive restarts.”

Proposed:

> Queues survive Discord session reconnects. Restarting the Melodix process
> clears the queue; playback history and server settings remain on disk.

### Configuration reference

The `MAX_AUDIO_BITRATE` row currently includes design motivation, quality policy,
encrypted relay behavior and bandwidth/re-fetch advice in one cell.

Proposed description cell, retaining the key and default:

> Maximum bitrate selected by the native YouTube parser, in bits per second.
> `0` selects the best available format. Set a cap for a constrained connection.

Put the encrypted-relay explanation and tuning tradeoff in a short subsection
linked from that row. Do not change runtime behavior or hide the default.

## Migration sequence

1. Establish the publication boundary. Exclude `docs/internal` and templates
   before any implementation commit containing this proposal is pushed. Prove
   the current Jekyll build, old route/anchor behavior and raw Markdown delivery
   in a safe local build. Keep the chosen toolchain reproducible and separate
   from the bot runtime.
2. Correct current behavior claims and unblock first playback. Reorder running
   in place, add complete release variants, align landing setup/recovery copy
   and correct stale architecture descriptions. Preserve checked doc formats.
3. Add playback/troubleshooting guides and the task index. Consolidate library
   consumer material and architecture explanations. Make the contributor route
   visible near the start of conventions. Regenerate the README from its template.
4. Finish the Jekyll docs shell, navigation, metadata, source/version labels,
   sitemap and optional llms index using the same canonical articles. Validate
   publication output, direct links and the long rendered references.
5. Bring offline bundle selection into both workflows and exercise an extracted
   package. Validate the tutorial paths that can be tested without a real token;
   report real Discord/audio acceptance separately.

Relocation/link corrections and prose cleanup should be reviewable separately
within that sequence. No unrequested changes to playback architecture, parser
selection, recovery budgets, application defaults or operational skills.

## Acceptance checks

- Every public guide has a reachable reader task and one canonical owner. All
  existing source paths remain, and existing public anchors resolve or have
  explicit compatibility targets.
- Local Markdown paths/anchors and generated site links are checked with the
  actual renderer's slug rules, including headings containing `&`. Do not report
  a broken link caused only by a checker leaving HTML entities undecoded.
- `go test ./internal/conventions ./internal/readme ./internal/discord/perm`
  passes. Run affected package tests, vet and the normal project gates if
  executable packaging/publication code changes. Do not infer voice success
  from any offline result.
- The README regenerates reproducibly and catalog output still matches both
  frontends. No hand-maintained replacement command catalog appears.
- Public site output contains exactly the intended documentation pages, with
  no raw/rendered `docs/internal`, template or skill output. Public sources can
  still be reached on GitHub. Metadata is page-specific and excludes the stale
  repository description.
- Inspect desktop and roughly 390px layouts: reasonable prose measure, visible
  topic navigation, long table identifiers intact, code/table scroll contained
  within the article, usable keyboard focus and copy controls if added. Render
  Mermaid diagrams or provide a readable fallback; do not leave diagram syntax
  as the only published explanation.
- Direct reloads work on old `.html` routes, `/docker/`, `/pkg/music/` and new
  docs entries. Check root custom-domain hosting and a project base path before
  claiming both are supported. Preserve existing favicons and landing assets.
- Both bundle producers use the same selection. Inspect an extracted package
  with networking disabled: setup/config/playback/troubleshooting links work,
  required local images exist, real data/secrets/internal notes are absent and
  omitted source links name the bundled revision.
- Use fresh disposable directories and synthetic inputs for offline tutorial
  checks. Do not run deployment scripts, connect with the real local `.env`,
  register commands or play to a live guild merely to check prose.
- Live acceptance, when separately available: connect/register as instructed,
  play a track, search/pick, queue/skip/stop, and confirm permission/voice
  behavior. Record the tested release, platform and source. An owner-confirmed
  live run is still required before claiming the entire first-run path works.

## Checks performed for this audit

- Full inventory and reading coverage recorded above; baseline working tree clean.
- 57 local Markdown link/image destinations across all 11 tracked Markdown and
  template files checked, with no missing local paths or heading anchors. The
  temporary checker used marked/github-slugger already installed in the GEKTOR
  workspace; it added no Melodix dependency or script.
- `go test ./internal/conventions ./internal/readme` passed. The invitation-mask
  test was inspected but not included in that command.
- Current Pages configuration and latest release asset names inspected through
  GitHub's read-only API. Bundle inclusion inspected in both workflows.
- Selected live HTML/raw Markdown routes and current metadata checked; setup
  page inspected in the browser at desktop size. No mobile/browser interaction
  study, complete external-link crawl or Jekyll build was performed.
- No bot session, real token, audio output, Docker build/deployment, release ZIP
  extraction, README regeneration, production change, commit or push performed.

Only this plan is added by the audit. Because the current publisher serves
`docs/internal`, configure its exclusions before pushing the plan with an
implementation. Approval should cover the migration sequence and its bounded
publication/packaging work; application changes remain outside it.

## Implementation record — 2026-11-09

The approved migration is implemented locally:

- Retained all existing document paths, numbered ownership contracts, checked
  convention tags, configuration rows/defaults and generated README catalogs.
- Added the task index, playback and symptom-first troubleshooting guides.
  Reordered setup for complete Windows/Unix release starts before optional tuning.
- Corrected restart/bitrate/registration claims, current voice sink/build details,
  and mark-versus-page palette descriptions. Consolidated library consumption
  around a complete example and links to architecture/ownership. Moved a previous
  gateway incident into the excluded historical notes.
- Added a pinned Jekyll toolchain and staged publication build. Its allow-list
  supplies navigation, descriptions, canonical/social metadata, clean Markdown,
  sitemap and llms.txt. Unknown generated output fails verification.
- Added the readable article shell, responsive menu/tables, theme control,
  copying feedback, and progressive Mermaid rendering with source/text fallback.
- Both binary workflows use one documentation selection/rewrite function.
  Bundles retain local guides/media and pin omitted sources to their revision.
  Packaging rejects unexpected pre-existing files such as runtime/private data.
- No application Go files, configuration defaults, original assets, licenses
  or operational skills were changed. No new bot runtime dependency was added.

Validation:

- Pinned Jekyll builds passed at `/` and `/melodix/`: 11 HTML routes, 10 clean
  Markdown articles, complete publication/metadata/link/anchor checks.
- Every previous heading anchor from the seven existing published guides is
  preserved, including explicit compatibility anchors in the library README.
- Five tooling tests passed, covering code-safe link rewriting, manifest
  coverage, offline bundle links, rejection of private/runtime files and a
  negative test proving the site rejects an unlisted HTML page.
- `go vet ./...`, `go test -race ./...`, `golangci-lint run ./...` (zero findings), and the focused documentation,
  generator and permission checks passed. README regeneration was idempotent.
- The complete library README example compiled without running audio/network.
  Both application binaries were built and placed in a synthetic ZIP; it was
  extracted and the CLI ran `help`, `queue`, `stop`, `quit` successfully from
  that isolated release directory, using the example configuration and fresh data.
- All 10 article HTML/Markdown pairs were checked over local HTTP. Sample
  internal, skill, template and application-source URLs correctly returned 404.
- All 10 guides were checked at 390px with no page overflow. Both diagrams
  rendered. Desktop reading layout, theme cycling, keyboard focus and copy-success
  feedback were checked. Clipboard API readback was empty in the browser harness,
  so copying bytes was not independently verified through that API.
- All 21 original tracked media/skill/license files remain byte-for-byte unchanged.
  The mechanical design detector reported inherited landing-page styling issues;
  this migration preserves that existing visual design rather than expanding
  scope into a landing redesign.

Ruby and its build tools were installed only under ignored `tools/` for local
Jekyll validation. The Website workflow builds the same staged output on Linux.
Before deployment, switch the repository's Pages source from legacy branch
publication to GitHub Actions. No Pages setting, commit, push or deployment was
changed. No real Discord session, token, audible playback, Docker deployment,
or cross-platform audio acceptance was exercised.
