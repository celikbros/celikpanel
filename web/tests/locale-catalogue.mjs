import { readFileSync } from 'node:fs';

// The product's copy lives in two files per locale since register R-060: the
// shell half, which boots with the application, and the screen half, which
// arrives beside the screen that needs it. A contract about the product's copy
// is a contract about both halves, so every test reads them as one catalogue
// and no test has to know which half a key landed in.
//
// Ürünün metni R-060'tan bu yana her dil için iki dosyada yaşar: uygulamayla
// birlikte açılan kabuk yarısı ve ihtiyaç duyan ekranla birlikte gelen ekran
// yarısı. Metne dair bir sözleşme her iki yarıya dairdir; bu yüzden testler
// ikisini tek katalog olarak okur.
const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

export const englishCatalogue = read('../src/i18n/en.ts') + read('../src/i18n/screens/en.ts');
export const turkishCatalogue = read('../src/i18n/tr.ts') + read('../src/i18n/screens/tr.ts');
