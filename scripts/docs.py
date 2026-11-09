"""Build the Jekyll documentation site and select the same human docs for archives."""
import argparse
from html.parser import HTMLParser
import json
import os
import posixpath
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
from urllib.parse import quote, unquote, urlsplit, urljoin

ROOT = Path(__file__).resolve().parents[1]
ORIGIN = 'https://melodix.keshon.ru'
REPO = 'https://github.com/keshon/melodix'
PAGES = json.loads((ROOT / '_data/docs.json').read_text(encoding='utf-8'))
BY_SOURCE = {p['source']: p for p in PAGES}
STAGE = ROOT / 'tools/site-source'
SITE = ROOT / '_site'
# Supported Markdown links are rewritten only outside fenced/inline code.
TOKENS = re.compile(
    r'(?P<fence>^ {0,3}(?P<marker>`{3,}|~{3,})[^\n]*\n[\s\S]*?^ {0,3}(?P=marker)[ \t]*$)'
    r'|(?P<code>(?P<ticks>`+)[^\n]*?(?P=ticks))'
    r'|(?P<link>!?\[(?:[^\]\\]|\\.)*\]\(\s*(?P<dest><[^>\n]+>|[^\s)]+))', re.M)


def read(path):
    return path.read_text(encoding='utf-8')


def write(path, text):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text.rstrip() + '\n', encoding='utf-8', newline='\n')


def revision():
    return subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()


def base_path(value):
    if not re.fullmatch(r'/(?:[a-zA-Z0-9_-]+/)*', value):
        raise ValueError('Base must be / or a directory path with a trailing slash')
    return value


def local_target(source, href):
    url = urlsplit(href)
    if url.scheme or url.netloc:
        return None
    if url.path.startswith('/'):
        raise ValueError(f'Repository link must be relative: {source}: {href}')
    parts = list(PurePosixPath(source).parent.parts)
    for part in unquote(url.path).split('/') if url.path else [PurePosixPath(source).name]:
        if part == '..':
            if not parts:
                raise ValueError(f'Link escapes repository: {source}: {href}')
            parts.pop()
        elif part not in ('', '.'):
            parts.append(part)
    target = '/'.join(parts)
    if not (ROOT / target).exists():
        raise ValueError(f'Missing destination: {source}: {href}')
    return target, url


def rewrite(source, text, mode, rev, base='/', included=None):
    def replace(match):
        if match.group('dest') is None:
            return match.group()
        href = match.group('dest').strip('<>')
        url = urlsplit(href)
        raw_prefix = '/keshon/melodix/main/'
        if mode == 'archive' and url.netloc == 'raw.githubusercontent.com' and url.path.startswith(raw_prefix):
            asset = unquote(url.path[len(raw_prefix):])
            if asset in included and asset.startswith('assets/'):
                relative = posixpath.relpath(asset, str(PurePosixPath(source).parent))
                return match.group()[:match.start('dest') - match.start()] + '<' + quote(relative, safe='/') + '>'
        found = local_target(source, href)
        if found is None:
            return match.group()
        target, url = found
        suffix = ('?' + url.query if url.query else '') + ('#' + url.fragment if url.fragment else '')
        if mode == 'archive' and target in included:
            return match.group()
        if target in BY_SOURCE:
            dest = ORIGIN + '/' + target if mode == 'raw' else base + BY_SOURCE[target]['route'].lstrip('/')
        elif target.startswith('assets/') and mode != 'archive':
            dest = (ORIGIN + '/' if mode == 'raw' else base) + target
        else:
            kind = 'tree' if (ROOT / target).is_dir() else 'blob'
            dest = f'{REPO}/{kind}/{rev}/{quote(target, safe="/")}'
        return match.group()[:match.start('dest') - match.start()] + '<' + dest + suffix + '>'
    return TOKENS.sub(replace, text)


def validate_manifest():
    if len(BY_SOURCE) != len(PAGES) or len({p['route'] for p in PAGES}) != len(PAGES):
        raise ValueError('Duplicate publication source or route')
    for page in PAGES:
        if not re.fullmatch(r'(?:docs/[^/]+\.md|docker/README\.md|pkg/music/README\.md)', page['source']):
            raise ValueError('Source outside publication boundary')
        if not re.fullmatch(r'/(?:[a-z-]+/)*(?:[a-z-]+\.html)?', page['route']):
            raise ValueError('Invalid public route')
        if not all(page.get(k, '').strip() for k in ('title', 'description', 'section')):
            raise ValueError('Missing publication metadata')
    maintained = {p.relative_to(ROOT).as_posix() for p in (ROOT / 'docs').glob('*.md')}
    if maintained != {p for p in BY_SOURCE if p.startswith('docs/')}:
        raise ValueError('Top-level human docs must be explicitly published')


def prepare(base):
    validate_manifest()
    # Clean only a fixed, generated directory inside this checkout.
    if STAGE.exists():
        if STAGE.is_symlink() or STAGE.resolve().parent != (ROOT / 'tools').resolve():
            raise ValueError('Unsafe staging directory')
        shutil.rmtree(STAGE)
    STAGE.mkdir(parents=True)
    for name in ['_config.yml', '_data', '_layouts', '_includes', 'assets', 'CNAME', 'robots.txt', 'sitemap.xml', 'llms.txt']:
        source = ROOT / name
        if source.is_dir():
            shutil.copytree(source, STAGE / name)
        else:
            shutil.copy2(source, STAGE / name)
    rev = revision()
    dirty = bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT, text=True).strip())
    with (STAGE / '_config.yml').open('a', encoding='utf-8') as stream:
        stream.write(f'\nrevision: {rev}\nlocal_changes: {str(dirty).lower()}\n')
    homepage = read(ROOT / 'index.html').replace('href="/assets/', f'href="{base}assets/')
    homepage = homepage.replace('<link rel="canonical"', f'<link rel="describedby" href="{ORIGIN}/llms.txt">\n  <link rel="canonical"', 1)
    write(STAGE / 'index.html', homepage)
    for page in PAGES:
        frontmatter = '---\nlayout: docs\npermalink: ' + page['route'] + '\n---\n'
        write(STAGE / page['source'], frontmatter + rewrite(page['source'], read(ROOT / page['source']), 'site', rev, base))
    return rev


def finish(rev):
    for page in PAGES:
        write(SITE / page['source'], rewrite(page['source'], read(ROOT / page['source']), 'raw', rev))


class Document(HTMLParser):
    def __init__(self, text):
        super().__init__()
        self.ids, self.links, self.meta, self.head_links = set(), [], {}, []
        self.feed(text)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if attrs.get('id'):
            self.ids.add(attrs['id'])
        if tag == 'a' and attrs.get('name'):
            self.ids.add(attrs['name'])
        for key in ('href', 'src'):
            if attrs.get(key):
                self.links.append(attrs[key])
        if tag == 'meta':
            self.meta[attrs.get('name', attrs.get('property'))] = attrs.get('content')
        if tag == 'link':
            self.head_links.append(attrs)


def verify(base):
    expected = {'index.html'} | {p['route'].lstrip('/') + ('index.html' if p['route'].endswith('/') else '') for p in PAGES}
    actual = {p.relative_to(SITE).as_posix() for p in SITE.rglob('*.html')}
    if actual != expected:
        raise ValueError(f'Unexpected/missing HTML: {actual ^ expected}')
    allowed = expected | set(BY_SOURCE) | {'robots.txt', 'sitemap.xml', 'llms.txt', 'CNAME'}
    for path in SITE.rglob('*'):
        if path.is_file() and path.relative_to(SITE).as_posix() not in allowed and not path.is_relative_to(SITE / 'assets'):
            raise ValueError(f'Unexpected published file: {path}')
    documents = {file: Document(read(SITE / file)) for file in expected}
    for file, doc in documents.items():
        route = base + file.removesuffix('index.html')
        for href in doc.links:
            url = urlsplit(href)
            if url.scheme or url.netloc:
                continue
            url = urlsplit(urljoin('https://local.test' + route, href))
            if not url.path.startswith(base):
                raise ValueError(f'Link outside base: {file}: {href}')
            target = unquote(url.path[len(base):])
            if not target or target.endswith('/'):
                target += 'index.html'
            if not (SITE / target).is_file():
                raise ValueError(f'Broken published link: {file}: {href}')
            if url.fragment and target in documents and unquote(url.fragment) not in documents[target].ids:
                raise ValueError(f'Broken published anchor: {file}: {href}')
    for page in PAGES:
        file = page['route'].lstrip('/') + ('index.html' if page['route'].endswith('/') else '')
        doc = documents[file]
        if doc.meta.get('description') != page['description']:
            raise ValueError(f'Wrong description: {file}')
        canonical = [t['href'] for t in doc.head_links if t.get('rel') == 'canonical']
        if canonical != [ORIGIN + page['route']]:
            raise ValueError(f'Wrong canonical: {file}')
        if not any(t.get('type') == 'text/markdown' and t.get('href') == ORIGIN + '/' + page['source'] for t in doc.head_links):
            raise ValueError(f'Missing Markdown alternative: {file}')
        raw = read(SITE / page['source'])
        if raw.startswith('---\n') or not raw.startswith('# '):
            raise ValueError(f'Raw Markdown contains publishing metadata: {file}')
    sitemap = read(SITE / 'sitemap.xml')
    if set(re.findall(r'<loc>([^<]+)</loc>', sitemap)) != {ORIGIN + '/'} | {ORIGIN + p['route'] for p in PAGES}:
        raise ValueError('Sitemap differs from publication list')
    index = read(SITE / 'llms.txt')
    if set(re.findall(r'\]\(([^)]+)\)', index)) != {ORIGIN + '/' + p['source'] for p in PAGES}:
        raise ValueError('Agent index differs from publication list')
    print(f'Verified {len(expected)} HTML routes and {len(PAGES)} raw articles at {base}; publication, metadata, assets, links and anchors')


def archive(destination):
    validate_manifest()
    destination = destination.resolve()
    if not destination.is_relative_to(ROOT / 'dist') or destination == ROOT / 'dist':
        raise ValueError('Bundle destination must be a directory under dist/')
    if not destination.is_dir():
        raise ValueError('Create the binary bundle directory first')
    rev = revision()
    selected = {'README.md', 'LICENSE', 'pkg/music/LICENSE'} | set(BY_SOURCE)
    selected |= {p.relative_to(ROOT).as_posix() for p in (ROOT / 'assets').rglob('*') if p.is_file() and p.suffix.lower() not in ('.js', '.css')}
    allowed = selected | {'.env', 'DOCUMENTATION.md', 'melodix-discord', 'melodix-cli', 'melodix-discord.exe', 'melodix-cli.exe'}
    unexpected = {p.relative_to(destination).as_posix() for p in destination.rglob('*') if p.is_file()} - allowed
    if unexpected:
        raise ValueError(f'Unexpected bundle files; use a clean binary directory: {sorted(unexpected)}')
    for file in selected:
        source = ROOT / file
        if source.suffix == '.md':
            write(destination / file, rewrite(file, read(source), 'archive', rev, included=selected))
        else:
            (destination / file).parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, destination / file)
    shutil.copy2(ROOT / '.env.example', destination / '.env')
    (destination / 'data').mkdir(exist_ok=True)
    write(destination / 'DOCUMENTATION.md', f'# Bundled documentation\n\nThese guides describe source revision `{rev}`. The installed binaries belong\nto this bundle. Read [Running Melodix](docs/running.md) to start.\n\nDocumentation links work offline; external services and remote demos require a\nnetwork. Online development docs may describe newer features.\n')
    print(f'Bundled {len(selected)} human documentation/license/asset files from {rev}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['build', 'prepare', 'finish', 'verify', 'archive'])
    parser.add_argument('--base', default='/')
    parser.add_argument('--destination', type=Path)
    args = parser.parse_args()
    base = base_path(args.base)
    if args.action in ('build', 'prepare'):
        prepare(base)
    if args.action == 'build':
        bundle = shutil.which('bundle') or shutil.which('bundle.bat')
        if not bundle:
            raise SystemExit('Install Ruby/Bundler and run bundle install first; see scripts/README.md')
        env = dict(os.environ, BUNDLE_GEMFILE=(ROOT / 'Gemfile').as_posix())
        subprocess.run([bundle, 'exec', 'jekyll', 'build', '--source', '.', '--destination', SITE.as_posix(), '--baseurl', base.rstrip('/')], cwd=STAGE, env=env, check=True)
    if args.action in ('build', 'finish'):
        finish(revision())
    if args.action in ('build', 'verify'):
        verify(base)
    if args.action == 'archive':
        if args.destination is None:
            parser.error('--destination is required for archive')
        archive(args.destination)
