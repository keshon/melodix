import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const root = fileURLToPath(new URL('../', import.meta.url));
export const pages = JSON.parse(fs.readFileSync(path.join(root, 'website/docs-nav.json'), 'utf8'));
const repository = 'https://github.com/keshon/melodix';
const encodePath = value => value.split('/').map(encodeURIComponent).join('/');
export const revision = () => execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
export const markdownTokens = () => /(^ {0,3}(?<marker>`{3,}|~{3,})[^\n]*\n[\s\S]*?^ {0,3}\k<marker>[ \t]*$)|((?<ticks>`+)[^\n]*?\k<ticks>)|(!?\[(?:[^\]\\]|\\.)*\]\(\s*(?<dest><[^>\n]+>|[^\s)]+))/gm;

export function localTarget(source, href) {
  if (/^[a-z][a-z\d+.-]*:|^\/\//i.test(href)) return null;
  if (href.startsWith('/')) throw new Error(`Repository link must be relative: ${source}: ${href}`);
  const url = new URL(href, 'https://local.invalid/');
  const location = href.split(/[?#]/)[0];
  const file = path.posix.normalize(path.posix.join(path.posix.dirname(source), decodeURIComponent(location || path.posix.basename(source))));
  if (file === '..' || file.startsWith('../') || path.posix.isAbsolute(file)) throw new Error(`Link escapes repository: ${source}: ${href}`);
  if (!fs.existsSync(path.join(root, file))) throw new Error(`Missing destination: ${source}: ${href}`);
  return { file, suffix: url.search+url.hash };
}

export function rewrite(source, text, rev, included) {
  return text.replaceAll('\r\n', '\n').replace(markdownTokens(), (...args) => {
    const match = args[0], { dest } = args.at(-1);
    if (!dest) return match;
    const href = dest.replace(/^<|>$/g, '');
    const url = new URL(href, 'https://local.invalid/');
    const prefix = '/keshon/melodix/main/';
    let replacement;
    if (url.hostname === 'raw.githubusercontent.com' && url.pathname.startsWith(prefix)) {
      const asset = decodeURIComponent(url.pathname.slice(prefix.length));
      if (asset.startsWith('assets/') && included.has(asset)) replacement = encodePath(path.posix.relative(path.posix.dirname(source), asset));
    }
    if (!replacement) {
      const target = localTarget(source, href);
      if (!target || included.has(target.file)) return match;
      const kind = fs.statSync(path.join(root, target.file)).isDirectory() ? 'tree' : 'blob';
      replacement = `${repository}/${kind}/${rev}/${encodePath(target.file)}${target.suffix}`;
    }
    const offset = match.lastIndexOf(dest);
    return match.slice(0, offset) + `<${replacement}>` + match.slice(offset+dest.length);
  });
}

export function validateManifest() {
  if (new Set(pages.map(page => page.source)).size !== pages.length || new Set(pages.map(page => page.route)).size !== pages.length) throw new Error('Duplicate publication source or route');
  for (const page of pages) {
    if (!/^(docs\/[^/]+\.md|docker\/README\.md|pkg\/music\/README\.md)$/.test(page.source)) throw new Error(`Source outside publication boundary: ${page.source}`);
  }
  const maintained = fs.readdirSync(path.join(root, 'docs')).filter(name => name.endsWith('.md')).map(name => `docs/${name}`).sort();
  const published = pages.filter(page => page.source.startsWith('docs/')).map(page => page.source).sort();
  if (JSON.stringify(maintained) !== JSON.stringify(published)) throw new Error('Top-level human docs must be explicitly selected');
}

export function archive(destination) {
  validateManifest();
  destination = fs.realpathSync(destination);
  const dist = fs.realpathSync(path.join(root, 'dist'));
  const relative = path.relative(dist, destination);
  if (!relative || relative.startsWith('..') || path.isAbsolute(relative)) throw new Error('Bundle destination must be a directory under dist/');
  if (!fs.statSync(destination).isDirectory()) throw new Error('Create the binary bundle directory first');
  const rev = revision();
  const selected = new Set(['README.md', 'LICENSE', 'pkg/music/LICENSE', 'start-discord.cmd', 'start-discord.sh', 'start-cli.cmd', 'start-cli.sh', 'scripts/ensure-build.cmd', 'scripts/ensure-build.sh', ...pages.map(page => page.source)]);
  const assets = execFileSync('git', ['ls-files', '-z', 'assets'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean);
  for (const file of assets) if (fs.existsSync(path.join(root, file)) && !file.endsWith('.gitkeep')) selected.add(file);
  const allowed = new Set([...selected, '.env', 'DOCUMENTATION.md', 'melodix-discord', 'melodix-cli', 'melodix-discord.exe', 'melodix-cli.exe']);
  for (const file of fs.readdirSync(destination, { recursive: true })) {
    const info = fs.lstatSync(path.join(destination, file));
    if (info.isSymbolicLink() || (!info.isDirectory() && !allowed.has(file.replaceAll('\\', '/')))) throw new Error(`Unexpected bundle file; use a clean binary directory: ${file}`);
  }
  for (const file of selected) {
    const source = path.join(root, file), output = path.join(destination, file);
    if (!fs.lstatSync(source).isFile()) throw new Error(`Payload must be a regular file: ${file}`);
    fs.mkdirSync(path.dirname(output), { recursive: true });
    if (file.endsWith('.md')) fs.writeFileSync(output, rewrite(file, fs.readFileSync(source, 'utf8'), rev, selected).trimEnd()+'\n');
    else fs.copyFileSync(source, output);
    if (file.endsWith('.sh')) fs.chmodSync(output, 0o755);
  }
  fs.copyFileSync(path.join(root, '.env.example'), path.join(destination, '.env'));
  fs.mkdirSync(path.join(destination, 'data'), { recursive: true });
  fs.writeFileSync(path.join(destination, 'DOCUMENTATION.md'), `# Bundled documentation\n\nThese guides describe source revision \`${rev}\`. The installed binaries belong\nto this bundle. Read [Running Melodix](docs/running.md) to start.\n\nDocumentation links work offline; external services and remote demos require a\nnetwork. Online development docs may describe newer features.\n`);
  console.log(`Bundled ${selected.size} documentation, license, launcher and asset files from ${rev}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    if (process.argv.length !== 4 || process.argv[2] !== '--destination') throw new Error('Usage: node scripts/archive-docs.mjs --destination dist/<bundle>');
    archive(process.argv[3]);
  } catch (error) {
    console.error('ERROR:', error.message);
    process.exitCode = 1;
  }
}
