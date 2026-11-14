# Application launcher helpers

`ensure-build.cmd` and `ensure-build.sh` are shared by the root application
launchers. In a source checkout they rebuild the requested application; in an
extracted distribution they use the supplied executable. Sharing this logic
keeps the launchers small and gives both shells the same release fallback.
The directory is functional, not required just to match another repository.

`build.cmd` and `build.sh` use `cmd/build` for development commands. Website
tooling lives in [`website/`](../website/README.md).

`archive-docs.mjs` selects public guides, licenses, launchers, example config and
shared media for application distributions and rewrites omitted source links to
GitHub. It does not read the live `.env` or runtime data. The article allow-list
is `website/docs-nav.json`; no website build is needed to make a release bundle.

```sh
node --test scripts/archive-docs.test.mjs
node scripts/archive-docs.mjs --destination dist/melodix-windows-amd64
```

Create a clean binary bundle directory before running the archive helper.
Normally `build dist` handles that staging for you.
