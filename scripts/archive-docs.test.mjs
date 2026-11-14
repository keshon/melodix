import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import assert from 'node:assert/strict';
import { archive, root, pages, rewrite, localTarget, revision, markdownTokens, validateManifest } from './archive-docs.mjs';

function fixture(t) {
  const dist = path.join(root, 'dist');
  fs.mkdirSync(dist, { recursive: true });
  const directory = fs.mkdtempSync(path.join(dist, '.archive-test-'));
  t.after(() => {
    assert(fs.realpathSync(directory).startsWith(fs.realpathSync(dist)+path.sep));
    fs.rmSync(directory, { recursive: true });
  });
  return directory;
}

test('archive links preserve examples and included local destinations', () => {
  const source = '[Guide](running.md)\n`[Guide](running.md)`\n\n```md\n[Guide](running.md)\n```';
  assert.equal(rewrite('docs/README.md', source, 'abc123', new Set(['docs/running.md'])), source);
  assert(rewrite('docs/README.md', '[Code](../cmd/discord/main.go)', 'abc123', new Set()).includes('/blob/abc123/cmd/discord/main.go'));
  assert.throws(() => localTarget('README.md', '../outside'), /escapes/);
});

test('release manifest uses only maintained articles', () => {
  validateManifest();
  for (const page of pages) {
    assert(!page.source.includes('/internal/'));
    rewrite(page.source, fs.readFileSync(path.join(root, page.source), 'utf8'), 'abc123', new Set());
  }
});

test('bundles retain binaries and launchers, use example config and keep guide links local', t => {
  const destination = fixture(t);
  fs.writeFileSync(path.join(destination, 'melodix-discord.exe'), 'synthetic binary');
  fs.copyFileSync(path.join(root, 'start-discord.cmd'), path.join(destination, 'start-discord.cmd'));
  archive(destination);
  assert.equal(fs.readFileSync(path.join(destination, 'melodix-discord.exe'), 'utf8'), 'synthetic binary');
  assert.deepEqual(fs.readFileSync(path.join(destination, '.env')), fs.readFileSync(path.join(root, '.env.example')));
  for (const name of ['scripts/ensure-build.sh', 'start-cli.sh']) assert(fs.existsSync(path.join(destination, name)));
  for (const name of ['docs/internal', '.agents', 'README.md.tmpl']) assert(!fs.existsSync(path.join(destination, name)));
  assert(fs.readFileSync(path.join(destination, 'README.md'), 'utf8').includes('(<assets/readme-banner.webp>)'));
  assert(fs.readFileSync(path.join(destination, 'docs/architecture.md'), 'utf8').includes(`/blob/${revision()}/docs/internal/implementation-notes.md`));
  for (const source of ['README.md', ...pages.map(page => page.source)]) {
    const text = fs.readFileSync(path.join(destination, source), 'utf8');
    for (const match of text.matchAll(markdownTokens())) {
      if (!match.groups.dest) continue;
      const href = match.groups.dest.replace(/^<|>$/g, '');
      if (/^[a-z][a-z\d+.-]*:|^\/\//i.test(href)) continue;
      const location = decodeURIComponent(href.split(/[?#]/)[0] || path.basename(source));
      assert(fs.existsSync(path.resolve(destination, path.dirname(source), location)), `${source}: ${href}`);
    }
  }
});

test('unexpected private payload is rejected before bundling', t => {
  const destination = fixture(t);
  fs.writeFileSync(path.join(destination, 'private.txt'), 'synthetic private data');
  assert.throws(() => archive(destination), /Unexpected bundle/);
});
