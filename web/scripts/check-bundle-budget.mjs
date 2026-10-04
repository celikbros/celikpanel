import { readFile, readdir } from 'node:fs/promises'
import { basename, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

const distDir = fileURLToPath(new URL('../dist/', import.meta.url))
const assetsDir = join(distDir, 'assets')
const html = await readFile(join(distDir, 'index.html'), 'utf8')
const manifest = JSON.parse(await readFile(join(distDir, '.vite', 'manifest.json'), 'utf8'))
const entryMatch = html.match(/<script\b[^>]*\bsrc=["']\/?(?:\.\/)?assets\/([^"']+\.js)["'][^>]*>/i)

if (!entryMatch) {
  throw new Error('Bundle budget: JavaScript entry was not found in dist/index.html')
}

const entryName = basename(entryMatch[1])
const budgets = {
  entry: { raw: 300 * 1024, gzip: 90 * 1024 },
  // Actionable deletion and DNS-setup guidance add less than 0.25 KiB to the Turkish locale.
  async: { raw: 160.25 * 1024, gzip: 45.25 * 1024 },
  // The global, fail-closed update tracker is boot-critical by design. Its
  // canonical cross-tab fence adds less than 1 KiB and must not be lazy-loaded.
  boot: { raw: 361 * 1024, gzip: 110 * 1024 },
  route: { raw: 280 * 1024, gzip: 80 * 1024 },
}

// Translation chunks, named by source. Each is an individual async chunk and is
// held to the same per-chunk async ceiling as every other one; naming them makes
// the check fail loudly if one disappears or is merged back into another. When a
// part outgrows the ceiling it is split again, as the screen catalogue was into
// screens/ and screens/server/; the ceiling is not raised.
// Çeviri parçaları kaynak adıyla listelenir; her biri aynı async tavanına
// tabidir. Tavanı aşan parça yeniden bölünür, tavan yükseltilmez.
const namedChunkBudgets = {
  'src/i18n/en.ts': budgets.async,
  'src/i18n/tr.ts': budgets.async,
  'src/i18n/screens/en.ts': budgets.async,
  'src/i18n/screens/tr.ts': budgets.async,
  'src/i18n/screens/server/en.ts': budgets.async,
  'src/i18n/screens/server/tr.ts': budgets.async,
}

const jsNames = (await readdir(assetsDir))
  .filter((name) => name.endsWith('.js'))
  .sort()

if (!jsNames.includes(entryName)) {
  throw new Error(`Bundle budget: entry ${entryName} is missing from dist/assets`)
}

const measurements = await Promise.all(jsNames.map(async (name) => {
  const source = await readFile(join(assetsDir, name))
  return {
    name,
    kind: name === entryName ? 'entry' : 'async',
    raw: source.byteLength,
    gzip: gzipSync(source, { level: 9 }).byteLength,
  }
}))
const measurementByName = new Map(measurements.map((item) => [item.name, item]))
const sourceByFile = new Map(Object.values(manifest)
  .filter((item) => item.file?.endsWith('.js'))
  .map((item) => [basename(item.file), item.src ?? item.name]))

const failures = []
for (const source of Object.keys(namedChunkBudgets)) {
  if (![...sourceByFile.values()].includes(source)) {
    failures.push(`expected a separate chunk for ${source}, found none`)
  }
}

const chunkRows = []
for (const item of measurements) {
  const source = sourceByFile.get(item.name)
  const limit = namedChunkBudgets[source] ?? budgets[item.kind]
  chunkRows.push({ label: `${item.name}${source ? ` (${source})` : ''}`, ...item, limit })
  if (item.raw > limit.raw || item.gzip > limit.gzip) {
    failures.push(
      `${item.name}: ${format(item.raw)} raw / ${format(item.gzip)} gzip ` +
      `(limit ${format(limit.raw)} / ${format(limit.gzip)})`,
    )
  }
}

const entry = measurements.find((item) => item.kind === 'entry')
const largestAsync = measurements
  .filter((item) => item.kind === 'async')
  .sort((a, b) => b.raw - a.raw)[0]

const entryManifest = Object.entries(manifest).find(([, item]) => item.isEntry && item.src === 'index.html')
if (!entryManifest) {
  throw new Error('Bundle budget: manifest has no entry')
}
const entryFiles = collectStaticFiles(entryManifest[0])

const localeEntries = Object.entries(manifest)
  .filter(([, item]) => item.src === 'src/i18n/en.ts' || item.src === 'src/i18n/tr.ts')
if (localeEntries.length !== 2) {
  failures.push(`expected two locale entries, found ${localeEntries.length}`)
}

const bootPayloads = localeEntries.map(([key]) => measureFiles(
  new Set([...entryFiles, ...collectStaticFiles(key)]),
))
const bootRaw = Math.max(0, ...bootPayloads.map((item) => item.raw))
const bootGzip = Math.max(0, ...bootPayloads.map((item) => item.gzip))

if (bootRaw > budgets.boot.raw || bootGzip > budgets.boot.gzip) {
  failures.push(
    `boot path: ${format(bootRaw)} raw / ${format(bootGzip)} gzip ` +
    `(limit ${format(budgets.boot.raw)} / ${format(budgets.boot.gzip)})`,
  )
}

const routePayloads = Object.entries(manifest)
  .filter(([, item]) => item.isDynamicEntry && item.src?.startsWith('src/components/'))
  .map(([key, item]) => {
    // The manifest links shared route chunks back to the already-loaded HTML
    // entry. Route budgets measure the incremental navigation payload only.
    const files = collectStaticFiles(key)
    for (const entryFile of entryFiles) files.delete(entryFile)
    return {
      name: item.src,
      ...measureFiles(files),
    }
  })

for (const route of routePayloads) {
  if (route.raw > budgets.route.raw || route.gzip > budgets.route.gzip) {
    failures.push(
      `${route.name} route payload: ${format(route.raw)} raw / ${format(route.gzip)} gzip ` +
      `(limit ${format(budgets.route.raw)} / ${format(budgets.route.gzip)})`,
    )
  }
}

const largestRoute = routePayloads.sort((a, b) => b.raw - a.raw)[0]

console.log(
  `Bundle budget: entry file ${format(entry.raw)} raw / ${format(entry.gzip)} gzip; ` +
  `critical boot ${format(bootRaw)} / ${format(bootGzip)}; ` +
  `largest route ${largestRoute ? `${largestRoute.name} ${format(largestRoute.raw)} / ${format(largestRoute.gzip)}` : 'none'}; ` +
  `largest individual async ${largestAsync ? `${largestAsync.name} ${format(largestAsync.raw)} / ${format(largestAsync.gzip)}` : 'none'}`,
)

// Every measured part with its limit and the headroom left, largest share of
// its raw ceiling first, so the next part to approach its line is at the top.
// Her ölçülen parça, sınırı ve kalan payıyla; sınırına en yakın olan en üstte.
const rows = [
  ...chunkRows.map((row) => ({ ...row, label: `chunk ${row.label}` })),
  { label: 'critical boot path', raw: bootRaw, gzip: bootGzip, limit: budgets.boot },
  ...routePayloads.map((route) => ({ label: `route ${route.name}`, ...route, limit: budgets.route })),
].sort((a, b) => b.raw / b.limit.raw - a.raw / a.limit.raw)
console.log('Bundle budget per part: raw size / limit (headroom) | gzip size / limit (headroom)')
for (const row of rows) {
  console.log(
    `  ${row.label}: ${format(row.raw)} / ${format(row.limit.raw)} (${headroom(row.raw, row.limit.raw)}) | ` +
    `${format(row.gzip)} / ${format(row.limit.gzip)} (${headroom(row.gzip, row.limit.gzip)})`,
  )
}

if (failures.length > 0) {
  console.error('Bundle budget exceeded:')
  for (const failure of failures) console.error(`- ${failure}`)
  process.exitCode = 1
}

function format(bytes) {
  return `${(bytes / 1024).toFixed(2)} KiB`
}

function headroom(size, limit) {
  const left = limit - size
  return `${left < 0 ? 'OVER ' : ''}${format(Math.abs(left))}${left < 0 ? '' : ' left'}, ${((left / limit) * 100).toFixed(1)}%`
}

function collectStaticFiles(key, files = new Set(), seen = new Set()) {
  if (seen.has(key)) return files
  seen.add(key)

  const item = manifest[key]
  if (!item) throw new Error(`Bundle budget: missing manifest node ${key}`)
  if (item.file?.endsWith('.js')) files.add(basename(item.file))
  for (const importedKey of item.imports ?? []) {
    collectStaticFiles(importedKey, files, seen)
  }
  return files
}

function measureFiles(names) {
  let raw = 0
  let gzip = 0
  for (const name of names) {
    const item = measurementByName.get(name)
    if (!item) throw new Error(`Bundle budget: no measurement for ${name}`)
    raw += item.raw
    gzip += item.gzip
  }
  return { raw, gzip }
}
