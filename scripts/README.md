# Documentation tooling

`_data/docs.json` selects the human articles used by the site and release bundles.
Edit the canonical Markdown, not generated files under `_site/` or `dist/`.
Change README copy in `README.md.tmpl` and run `go run ./cmd/readme` from the root.

## Build and check the site

Install Ruby 3.3 and Bundler, then run from the repository root:

```sh
bundle install
python scripts/docs.py build
python -B -m unittest discover -s scripts -p 'test_*.py'
```

The pinned Jekyll dependencies are for website tooling only. On Windows,
RubyInstaller needs its MSYS2 development tools to build native gem dependencies.
The bot does not need Ruby or Python at runtime.

The build stages only approved articles, publication templates and assets in
`tools/site-source`, runs Jekyll, writes clean Markdown alternatives and verifies
the entire output. Unknown HTML or non-asset output fails the build, including
raw or rendered internal plans, application source and operational skills.
Local changes are labelled alongside the source revision in article footers.

To preview:

```sh
python -m http.server 5177 --bind 127.0.0.1 --directory _site
```

Open `http://127.0.0.1:5177/`. For a project-path build, use
`python scripts/docs.py build --base /melodix/` and serve `_site` under that prefix.
Canonical metadata still names the production custom-domain routes. Rebuild the
default root variant afterwards. Test direct article reloads, tables, diagram
fallback, keyboard navigation and copying on a narrow viewport.

## Publication

The Website workflow builds and checks the same staged output before uploading
`_site`. For its first deployment, select **GitHub Actions** in repository
Settings → Pages instead of the legacy branch publisher. This change is needed
when the implementation is committed and deployed; it has no effect on local builds.
Keep the custom domain and HTTPS configuration.

`_config.yml` also excludes internal notes and templates from a legacy root
build, but only the staged build enforces the whole publication allow-list.
Do not use a raw checkout as a development web server: it contains local data
and configuration. Serve `_site` only.

HTML pages advertise the generated `llms.txt` index and their clean Markdown
alternative. Sitemap and index use the same manifest. Article links use website
routes in HTML and absolute Markdown URLs in raw articles. Omitted code/history
links point to the build revision on GitHub. The two Mermaid diagrams load a
pinned CDN module only on pages that need it; without it the source and surrounding
text remain available. No full-site text dump or ranking guarantee is implied.

## Bundle documentation

Both build/release workflows create their binary directory under `dist/`, then:

```sh
python scripts/docs.py archive --destination dist/melodix-linux-amd64
```

The script adds only approved human guides, licenses, media assets and the example
configuration as `.env`. It does not read the local `.env` or runtime data. Local
guide links remain relative; omitted sources link to the immutable source revision.
`DOCUMENTATION.md` records that revision and the offline entry point.
Run `test_docs.py` to check selection and links without a token, network or audio.
