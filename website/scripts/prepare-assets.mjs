import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../', import.meta.url));
const tracked = execFileSync('git', ['ls-files', '-z', 'assets'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean);
let copied = 0;
for (const name of tracked) {
  const source = path.join(root, name);
  if (!fs.existsSync(source) || !fs.statSync(source).isFile() || name.endsWith('.gitkeep')) continue;
  const destination = path.join(root, 'website/public', name);
  fs.mkdirSync(path.dirname(destination), { recursive: true });
  fs.copyFileSync(source, destination);
  copied++;
}
console.log(`Prepared ${copied} tracked shared assets; runtime uploads are excluded.`);
