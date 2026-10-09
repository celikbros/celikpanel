import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import ts from 'typescript';

// The shared remote-state layer, compiled from source for a mounted test:
// lib/remote.ts, lib/hostingCapabilities.ts, lib/apiError.ts and the REAL
// components/ui.tsx. A screen under test imports these exactly as it does in
// the application, so "checking", "could not check" and the gate around an
// empty state are the shipped ones and not a stand-in that could agree with a
// broken screen.
//
// Everything else a screen imports (icons, the router, the catalogue, toasts)
// comes from the stub the test supplies. That stub must export `useI18n`,
// `useNavigate` and `AlertTriangle`, which ui.tsx itself needs.
//
// Paylaşılan uzak-durum katmanı, bağlanan bir test için kaynaktan derlenir.
// Test edilen ekran bunları uygulamadaki gibi içe aktarır; böylece "kontrol
// ediliyor", "kontrol edilemedi" ve boş durumun çevresindeki kapı, bozuk bir
// ekranla anlaşabilecek bir vekil değil, gönderilen kodun kendisidir.
const require = createRequire(import.meta.url);
export const reactURL = pathToFileURL(require.resolve('react')).href;
export const dataModule = (text) => 'data:text/javascript;base64,' + Buffer.from(text).toString('base64');

const compilerOptions = { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 };
const read = (path) => readFileSync(new URL('../../src/' + path, import.meta.url), 'utf8');

// compileSource turns one file under web/src into an importable module.
// `resolve` maps each of its import specifiers to a module URL.
export function compileSource(path, resolve) {
  const compiled = ts.transpileModule(read(path), { compilerOptions }).outputText
    .replace(/from ['"]([^'"]+)['"]/g, (_, specifier) => `from '${specifier === 'react' ? reactURL : resolve(specifier)}'`);
  return dataModule(`import React from '${reactURL}';\n${compiled}`);
}

const unexpected = (file) => (specifier) => {
  throw new Error(`${file} imports ${specifier}, which the shared-layer fixture does not provide`);
};

export const apiErrorURL = compileSource('lib/apiError.ts', unexpected('lib/apiError.ts'));
export const remoteURL = compileSource('lib/remote.ts', (specifier) => (
  specifier.endsWith('/apiError') ? apiErrorURL : unexpected('lib/remote.ts')(specifier)
));
export const capabilitiesURL = compileSource('lib/hostingCapabilities.ts', (specifier) => (
  specifier.endsWith('/remote') ? remoteURL : unexpected('lib/hostingCapabilities.ts')(specifier)
));

// Since the fourth batch (9 Oct 2026) the shared layer also has the answer
// that did not arrive (lib/lostAnswer.ts), the one decoder of a domain's
// databases (lib/domainDatabases.ts) and the file download (lib/download.ts).
// They are the shipped modules for the same reason as the rest: a stand-in for
// "the result is unknown" could agree with a screen that sends twice.
// Dördüncü partiden beri paylaşılan katmanda, gelmeyen yanıt, bir alan adının
// veritabanlarının tek çözücüsü ve dosya indirme de vardır; hepsi gönderilen
// modüllerdir.
// The answer that did not arrive shares its definition of a lost answer, and
// the list of routes that carry an identity, with the request identity
// (D-029, 10 Oct 2026): the shipped module again.
// Gelmeyen yanıt, kaybolan yanıt tanımını ve kimlik taşıyan rotaların listesini
// istek kimliğiyle paylaşır: yine gönderilen modül.
export const requestIdentityURL = compileSource('lib/requestIdentity.ts', unexpected('lib/requestIdentity.ts'));
export const lostAnswerURL = compileSource('lib/lostAnswer.ts', (specifier) => (
  specifier.endsWith('/requestIdentity') ? requestIdentityURL : unexpected('lib/lostAnswer.ts')(specifier)
));
export const domainDatabasesURL = compileSource('lib/domainDatabases.ts', (specifier) => (
  specifier.endsWith('/remote') ? remoteURL : unexpected('lib/domainDatabases.ts')(specifier)
));
export const downloadURL = compileSource('lib/download.ts', (specifier) => (
  specifier.endsWith('/apiError') ? apiErrorURL : unexpected('lib/download.ts')(specifier)
));

// sharedLayer returns the resolver a screen is compiled with: the real shared
// modules by their import suffix, `extra` for a test's own real modules, and
// the stub for the rest.
// The readers of one explained answer each (12 Oct 2026) are the shipped ones in
// every mounted test: a site the web server refused, and the note of a Stop
// that left the unit marked as failed. They import types only.
export const siteWebServerRefusedURL = compileSource('lib/siteWebServerRefused.ts', unexpected('lib/siteWebServerRefused.ts'));
export const serviceActionNoteURL = compileSource('lib/serviceActionNote.ts', unexpected('lib/serviceActionNote.ts'));

export function sharedLayer(stubURL, extra = {}) {
  const uiURL = compileSource('components/ui.tsx', (specifier) => (
    specifier.endsWith('/apiError') ? apiErrorURL : stubURL
  ));
  const table = {
    '/lib/apiError': apiErrorURL,
    '/lib/remote': remoteURL,
    '/lib/hostingCapabilities': capabilitiesURL,
    '/lib/lostAnswer': lostAnswerURL,
    '/lib/domainDatabases': domainDatabasesURL,
    '/lib/download': downloadURL,
    '/lib/siteWebServerRefused': siteWebServerRefusedURL,
    '/lib/serviceActionNote': serviceActionNoteURL,
    '/ui': uiURL,
    ...extra,
  };
  const resolve = (specifier) => {
    for (const [suffix, url] of Object.entries(table)) if (specifier.endsWith(suffix)) return url;
    return stubURL;
  };
  return { uiURL, resolve, compile: (path, more = {}) => compileSource(path, (specifier) => {
    for (const [suffix, url] of Object.entries(more)) if (specifier.endsWith(suffix)) return url;
    return resolve(specifier);
  }) };
}
