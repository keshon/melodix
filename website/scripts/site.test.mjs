import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { pages, article, siteLink, localTarget, renderMarkdown, publishMarkdown, renderSitemap, validateManifest } from './documentation.mjs';

test('website has a custom domain and keeps application state outside its sources', () => {
  assert(fs.readFileSync(new URL('../public/CNAME', import.meta.url), 'utf8').trim().endsWith('.keshon.ru'));
  assert(!fs.existsSync(new URL('../.env', import.meta.url)));
});
test('publication list and local Markdown destinations are valid', () => {
  validateManifest();
  for (const page of pages) {
    for (const href of article(page.source).links) localTarget(page.source, href);
    assert(renderMarkdown(page).startsWith('# '));
  }
});
test('routes preserve the existing URLs and respect the Pages base', () => {
  assert.equal(siteLink('docs/README.md', 'running.md#configuration', '/melodix/'), '/melodix/docs/running.html#configuration');
  assert.throws(() => localTarget('README.md', '../outside'), /escapes/);
  assert(siteLink('docs/README.md', '../cmd/discord/main.go').includes('/blob/'));
});

test('raw Markdown rewrites article links without changing examples', () => {
  const source = '[Guide](running.md)\n`[Guide](running.md)`\n\n```md\n[Guide](running.md)\n```';
  const result = publishMarkdown('docs/README.md', source);
  assert(result.includes('[Guide](<https://melodix.keshon.ru/docs/running.md>)'));
  assert(result.includes('`[Guide](running.md)`'));
  assert(result.includes('```md\n[Guide](running.md)\n```'));
});

test('sitemap is generated from the same publication manifest', () => {
  const sitemap = renderSitemap();
  assert(!sitemap.includes('{{'));
  for (const page of pages) assert(sitemap.includes('https://melodix.keshon.ru'+page.route));
});
