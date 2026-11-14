import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { Marked } from 'marked';
import GithubSlugger from 'github-slugger';
import navigation from '../docs-nav.json' with { type: 'json' };

export const root = fileURLToPath(new URL('../../', import.meta.url));
export const pages = navigation;
export const origin = 'https://melodix.keshon.ru';
const repository = 'https://github.com/keshon/melodix';
const bySource = new Map(pages.map(page => [page.source, page]));
export const revision = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
const localChanges = execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim() !== '';
export const escape = value => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;');
export const basePath = value => `/${(value || '/').split('/').filter(Boolean).join('/')}${(value || '/').split('/').filter(Boolean).length ? '/' : ''}`;
export const htmlFile = page => page.route.slice(1) + (page.route.endsWith('/') ? 'index.html' : '');

export function validateManifest() {
  if (bySource.size !== pages.length || new Set(pages.map(page => page.route)).size !== pages.length) throw new Error('Duplicate documentation source or route');
  for (const page of pages) {
    if (!/^(docs\/[^/]+\.md|docker\/README\.md|pkg\/music\/README\.md)$/.test(page.source)) throw new Error(`Source outside publication boundary: ${page.source}`);
    if (!/^\/(?:[a-z-]+\/)*(?:[a-z-]+\.html)?$/.test(page.route)) throw new Error(`Invalid public route: ${page.route}`);
    if (!['title', 'description', 'section'].every(key => page[key]?.trim())) throw new Error(`Missing publication metadata: ${page.source}`);
  }
  const maintained = fs.readdirSync(path.join(root, 'docs')).filter(name => name.endsWith('.md')).map(name => `docs/${name}`).sort();
  const published = pages.filter(page => page.source.startsWith('docs/')).map(page => page.source).sort();
  if (JSON.stringify(maintained) !== JSON.stringify(published)) throw new Error('Top-level human docs must be explicitly published');
}

export function localTarget(source, href) {
  if (/^[a-z][a-z\d+.-]*:|^\/\//i.test(href)) return null;
  const url = new URL(href, 'https://local.invalid/');
  if (href.startsWith('/')) throw new Error(`Repository link must be relative: ${source}: ${href}`);
  const location = href.split(/[?#]/)[0];
  const file = path.posix.normalize(path.posix.join(path.posix.dirname(source), decodeURIComponent(location || path.posix.basename(source))));
  if (file === '..' || file.startsWith('../') || path.posix.isAbsolute(file)) throw new Error(`Link escapes repository: ${source}: ${href}`);
  if (!fs.existsSync(path.join(root, file))) throw new Error(`Missing destination: ${source}: ${href}`);
  return { file, suffix: url.search + url.hash, fragment: decodeURIComponent(url.hash.slice(1)) };
}

export function siteLink(source, href, base = '/') {
  const target = localTarget(source, href);
  if (!target) return href;
  const page = bySource.get(target.file);
  if (page) return base + page.route.slice(1) + target.suffix;
  if (target.file.startsWith('assets/')) return base + target.file + target.suffix;
  if (target.file.startsWith('website/public/assets/')) return base + target.file.slice('website/public/'.length) + target.suffix;
  const kind = fs.statSync(path.join(root, target.file)).isDirectory() ? 'tree' : 'blob';
  return `${repository}/${kind}/${revision}/${target.file.split('/').map(encodeURIComponent).join('/')}${target.suffix}`;
}

export function publishMarkdown(sourcePath, source) {
  const tokens = /(^ {0,3}(?<marker>`{3,}|~{3,})[^\n]*\n[\s\S]*?^ {0,3}\k<marker>[ \t]*$)|((?<ticks>`+)[^\n]*?\k<ticks>)|(!?\[(?:[^\]\\]|\\.)*\]\(\s*(?<dest><[^>\n]+>|[^\s)]+))/gm;
  return source.replaceAll('\r\n', '\n').replace(tokens, (...args) => {
    const match = args[0], { dest } = args.at(-1);
    if (!dest) return match;
    const href = dest.replace(/^<|>$/g, '');
    const target = localTarget(sourcePath, href);
    if (!target) return match;
    const published = bySource.get(target.file);
    const replacement = published ? origin + '/' + target.file + target.suffix : new URL(siteLink(sourcePath, href), origin).href;
    const offset = match.lastIndexOf(dest);
    return match.slice(0, offset) + `<${replacement}>` + match.slice(offset+dest.length);
  });
}

export function renderMarkdown(page) {
  return publishMarkdown(page.source, fs.readFileSync(path.join(root, page.source), 'utf8'));
}

export function article(source, base = '/') {
  const ids = new Set(), links = [], slugger = new GithubSlugger();
  const marked = new Marked({
    renderer: {
      heading({ tokens, depth }) {
        const html = this.parser.parseInline(tokens);
        const text = html.replace(/<[^>]*>/g, '').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&#39;/g, "'");
        const id = slugger.slug(text);
        ids.add(id);
        return `<h${depth} id="${escape(id)}">${html}</h${depth}>\n`;
      },
      link(token) { return `<a href="${escape(siteLink(source, token.href, base))}">${this.parser.parseInline(token.tokens)}</a>`; },
      image(token) { return `<img src="${escape(siteLink(source, token.href, base))}" alt="${escape(token.text)}" loading="lazy">`; },
    },
    walkTokens(token) {
      if (token.type === 'link' || token.type === 'image') links.push(token.href);
      if (token.type === 'html') for (const match of token.text.matchAll(/(?:id|name)=["']([^"']+)["']/g)) ids.add(match[1]);
    },
  });
  const html = marked.parse(fs.readFileSync(path.join(root, source), 'utf8'));
  return { html, ids, links };
}

export function renderPage(page, base = '/') {
  const { html } = article(page.source, base);
  const groups = [...new Set(pages.map(p => p.section))];
  const nav = groups.map(group => `<section><h2>${escape(group)}</h2><ul>${pages.filter(p => p.section === group).map(p => `<li><a href="${base+p.route.slice(1)}"${p === page ? ' aria-current="page"' : ''}>${escape(p.title)}</a></li>`).join('')}</ul></section>`).join('');
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light dark">
<title>${escape(page.title)} · Melodix</title><meta name="description" content="${escape(page.description)}"><link rel="canonical" href="${origin+page.route}"><link rel="alternate" type="text/markdown" href="${origin}/${page.source}"><link rel="describedby" href="${origin}/llms.txt">
<meta property="og:type" content="website"><meta property="og:site_name" content="Melodix"><meta property="og:title" content="${escape(page.title)} · Melodix"><meta property="og:description" content="${escape(page.description)}"><meta property="og:url" content="${origin+page.route}"><meta property="og:image" content="${origin}/assets/og-image.jpg"><meta name="twitter:card" content="summary_large_image"><meta name="twitter:title" content="${escape(page.title)} · Melodix"><meta name="twitter:description" content="${escape(page.description)}"><meta name="twitter:image" content="${origin}/assets/og-image.jpg">
<link rel="icon" href="${base}assets/favicon/favicon.svg"><link rel="stylesheet" href="${base}assets/docs.css"><script>try{var t=localStorage.getItem('melodix-theme');if(t==='light'||t==='dark')document.documentElement.dataset.theme=t}catch(e){}</script><script src="${base}assets/docs.js" defer></script></head><body>
<a class="skip" href="#content">Skip to content</a><header class="docs-header"><a class="wordmark" href="${base}"><img src="${base}assets/favicon/favicon.svg" alt="" width="28" height="28">Melodix</a><nav aria-label="Main navigation"><a href="${base}docs/">Documentation</a><a href="${repository}/releases">Downloads</a><a href="${repository}">GitHub</a></nav><button id="theme" type="button" aria-label="Change color theme">Theme: auto</button></header>
<div class="docs-layout"><aside class="docs-sidebar"><details id="docs-menu" open><summary>Browse documentation</summary><nav aria-label="Documentation">${nav}</nav></details></aside><main id="content" tabindex="-1"><article>${html}</article><footer class="source-note"><p>Development documentation${localChanges ? ' · local changes' : ''} · <a href="${repository}/tree/${revision}">${revision.slice(0,7)}</a></p><p><a href="${repository}/blob/${revision}/${page.source}">Source at this revision</a> · <a href="${repository}/edit/main/${page.source}">Edit on GitHub</a> · <a href="${base+page.source}">Read as Markdown</a></p><p>Release archives include documentation for their own revision.</p></footer></main></div><p id="copy-status" class="sr-only" role="status" aria-live="polite"></p></body></html>`;
}

export function renderLLMs() {
  return `# Melodix\n\nDevelopment documentation; release archives include guides from their own revision.\n\n${pages.map(page => `- [${page.title}](${origin}/${page.source}): ${page.description}`).join('\n')}\n`;
}

export function renderSitemap() {
  const routes = ['/', ...pages.map(page => page.route)];
  return '<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n' + routes.map(route => `  <url><loc>${origin+route}</loc></url>`).join('\n') + '\n</urlset>\n';
}

export function documentationPlugin() {
  let base = '/';
  return {
    name: 'melodix-documentation',
    configResolved(config) { base = basePath(config.base); validateManifest(); },
    transformIndexHtml(html) {
      html = html.replace(/(href|src)="\/(?!\/)([^"]*)"/g, (match, key, value) => value.startsWith(base.slice(1)) ? match : `${key}="${base}${value}"`);
      return html.replace('<link rel="canonical"', `<link rel="describedby" href="${origin}/llms.txt">\n<link rel="canonical"`); },
    generateBundle() {
      for (const page of pages) {
        this.emitFile({ type: 'asset', fileName: htmlFile(page), source: renderPage(page, base) });
        this.emitFile({ type: 'asset', fileName: page.source, source: renderMarkdown(page) });
      }
      this.emitFile({ type: 'asset', fileName: 'llms.txt', source: renderLLMs() });
      this.emitFile({ type: 'asset', fileName: 'sitemap.xml', source: renderSitemap() });
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const url = new URL(req.url, 'http://localhost').pathname;
        const page = pages.find(p => base+p.route.slice(1) === url);
        if (page) { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(renderPage(page, base)); return; }
        if (url === base+'sitemap.xml') { res.setHeader('Content-Type', 'application/xml; charset=utf-8'); res.end(renderSitemap()); return; }
        const raw = pages.find(p => base+p.source === url);
        if (raw || url === base+'llms.txt') { res.setHeader('Content-Type', raw ? 'text/markdown; charset=utf-8' : 'text/plain; charset=utf-8'); res.end(raw ? renderMarkdown(raw) : renderLLMs()); return; }
        next();
      });
    },
  };
}
