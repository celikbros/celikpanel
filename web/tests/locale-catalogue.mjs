import { readFileSync } from 'node:fs';

// The product's copy lives in several files per locale since register R-060:
// the shell half, which boots with the application, and the screen half, which
// arrives beside the screen that needs it. The screen half is itself stored in
// two files (./screens and ./screens/server) because one file outgrew the
// per-chunk bundle budget; the provider loads both before any screen renders.
// A contract about the product's copy is a contract about every part, so every
// test reads them as one catalogue and no test has to know which part a key
// landed in.
//
// Ürünün metni R-060'tan bu yana her dil için birden çok dosyada yaşar:
// uygulamayla birlikte açılan kabuk yarısı ve ihtiyaç duyan ekranla birlikte
// gelen ekran yarısı. Ekran yarısı da paket bütçesi yüzünden iki dosyadadır
// (./screens ve ./screens/server). Metne dair bir sözleşme her parçaya dairdir;
// bu yüzden testler hepsini tek katalog olarak okur.
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

// Every file of the screen half, in load order. index.tsx must import each one.
// Ekran yarısının her dosyası; index.tsx her birini yüklemelidir.
export const screenCatalogueFiles = {
  en: ['../src/i18n/screens/en.ts', '../src/i18n/screens/server/en.ts'],
  tr: ['../src/i18n/screens/tr.ts', '../src/i18n/screens/server/tr.ts'],
};

export const englishScreens = screenCatalogueFiles.en.map(read).join('');
export const turkishScreens = screenCatalogueFiles.tr.map(read).join('');

export const englishCatalogue = read('../src/i18n/en.ts') + englishScreens + read('../src/i18n/setupDNS/en.ts');
export const turkishCatalogue = read('../src/i18n/tr.ts') + turkishScreens + read('../src/i18n/setupDNS/tr.ts');
