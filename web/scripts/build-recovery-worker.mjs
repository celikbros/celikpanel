import { readFile, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { resolve, extname } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = fileURLToPath(new URL('../dist/', import.meta.url));
const manifest = JSON.parse(await readFile(resolve(dist, '.vite/manifest.json'), 'utf8'));
const files = new Set(['recovery-offline.html']);
const seen = new Set();
function collect(key) {
    if (seen.has(key)) return;
    seen.add(key);
    const entry = manifest[key];
    if (!entry || entry.dynamicImports?.length) throw new Error('Recovery shell must be self-contained');
    files.add(entry.file);
    for (const path of [...(entry.css || []), ...(entry.assets || [])]) files.add(path);
    for (const child of entry.imports || []) collect(child);
}
collect('recovery-offline.html');
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' };
const resources = [];
for (const path of [...files].sort()) {
    if (!/^(recovery-offline\.html|assets\/[A-Za-z0-9_.-]+)$/.test(path) || !types[extname(path)]) throw new Error('Unexpected recovery resource');
    const bytes = await readFile(resolve(dist, path));
    resources.push({ path: '/' + path, size: bytes.length, type: types[extname(path)], sha256: createHash('sha256').update(bytes).digest('hex') });
}
const size = resources.reduce((sum, item) => sum + item.size, 0);
if (size > 64 * 1024) throw new Error('Recovery shell exceeds 64 KiB static budget');
const template = await readFile(new URL('./recovery-worker.template.js', import.meta.url), 'utf8');
const id = createHash('sha256').update(template).update(JSON.stringify(resources)).digest('hex');
await writeFile(resolve(dist, 'recovery-worker.js'), template.replace('__BUILD_ID__', id).replace('__RESOURCES__', JSON.stringify(resources)));
console.log(`Recovery shell: ${resources.length} static files, ${size} bytes; no authenticated cache`);
