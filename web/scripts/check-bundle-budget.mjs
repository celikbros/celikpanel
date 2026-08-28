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
  async: { raw: 160 * 1024, gzip: 45 * 1024 },
  // The global, fail-closed update tracker is boot-critical by design. Its
  // canonical cross-tab fence adds less than 1 KiB and must not be lazy-loaded.
  boot: { raw: 361 * 1024, gzip: 110 * 1024 },
  route: { raw: 280 * 1024, gzip: 80 * 1024 },
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

const failures = []
for (const item of measurements) {
  const limit = budgets[item.kind]
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

const entryManifest = Object.entries(manifest).find(([, item]) => item.isEntry)
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

if (failures.length > 0) {
  console.error('Bundle budget exceeded:')
  for (const failure of failures) console.error(`- ${failure}`)
  process.exitCode = 1
}

function format(bytes) {
  return `${(bytes / 1024).toFixed(2)} KiB`
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
