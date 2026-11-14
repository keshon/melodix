import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { pages, origin } from './documentation.mjs';
const base = `/${(process.env.SITE_BASE_PATH || '/').split('/').filter(Boolean).join('/')}${(process.env.SITE_BASE_PATH || '/').split('/').filter(Boolean).length ? '/' : ''}`;
const dist = new URL('../dist/', import.meta.url);
const files = fs.readdirSync(dist, { recursive: true }).map(name => name.replaceAll('\\', '/')).filter(name => fs.statSync(new URL(name, dist)).isFile());
const expected = ['index.html', ...pages.map(page => page.route.slice(1) + (page.route.endsWith('/') ? 'index.html' : ''))].sort();
assert.deepEqual(files.filter(name => name.endsWith('.html')).sort(), expected, 'Published HTML must match the allow-list');
assert(!files.some(name => /(^|\/)(node_modules|scripts|cmd|internal|\.git|docs\/internal)(\/|$)/.test(name)), 'Private source must not be published');
const html = new Map(expected.map(file => [file, fs.readFileSync(new URL(file, dist), 'utf8')]));
for (const [file, text] of html) {
  for (const match of text.matchAll(/\b(?:href|src)=["']([^"']+)["']/g)) {
    const href = match[1].replaceAll('&amp;', '&');
    if (/^[a-z][a-z\d+.-]*:|^\/\//i.test(href)) continue;
    const url = new URL(href, 'https://local.invalid'+base+file);
    assert(url.pathname.startsWith(base), `Link outside base: ${file}: ${href}`);
    let target = decodeURIComponent(url.pathname.slice(base.length));
    if (!target || target.endsWith('/')) target += 'index.html';
    assert(fs.existsSync(new URL(target, dist)), `Missing target: ${file}: ${href}`);
    if (url.hash && html.has(target)) {
      const ids = new Set([...html.get(target).matchAll(/\b(?:id|name)=["']([^"']+)["']/g)].map(m => m[1]));
      assert(ids.has(decodeURIComponent(url.hash.slice(1))), `Missing anchor: ${file}: ${href}`);
    }
  }
}
for (const page of pages) {
  const file = page.route.slice(1)+(page.route.endsWith('/') ? 'index.html' : '');
  const text = html.get(file);
  assert(text.includes(`href="${origin+page.route}"`), `Missing canonical: ${file}`);
  assert(text.includes(`href="${origin}/${page.source}"`), `Missing raw Markdown alternate: ${file}`);
  assert(fs.readFileSync(new URL(page.source, dist), 'utf8').startsWith('# '), `Invalid raw Markdown: ${file}`);
}
console.log(`Verified ${expected.length} HTML routes, local links and anchors at ${base}; only website output is published.`);

const sitemap = fs.readFileSync(new URL('sitemap.xml', dist), 'utf8');
assert(!sitemap.includes('{{') && !sitemap.includes('{%'), 'Sitemap contains template syntax');
const locations = [...sitemap.matchAll(/<loc>([^<]+)<\/loc>/g)].map(match => match[1]).sort();
assert.deepEqual(locations, [origin+'/', ...pages.map(page => origin+page.route)].sort());
const index = fs.readFileSync(new URL('llms.txt', dist), 'utf8');
for (const page of pages) assert(index.includes(origin+'/'+page.source), `Missing agent index entry: ${page.source}`);
