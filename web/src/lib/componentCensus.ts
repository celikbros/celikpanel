import { useSyncExternalStore } from 'react';

// The number in the sidebar has to be the number the pages found. It used to
// be its own answer: Layout asked the API once, on mount, and then nothing
// could change it — an operator who ran a check on the dashboard or on a
// component page watched the badge keep the dash until the next full page
// load, while the screen beside it already said what was installed.
//
// The fix is not a second fetch and not a poll. Every screen that receives a
// host-wide managed-services payload ALREADY has the answer in its hands;
// this store is where they put it, and the sidebar reads it from there. One
// fetch path, one number, and the badge moves when the operator's check
// lands.
//
// Kenar çubuğundaki sayı, sayfaların bulduğu sayı olmalıdır. Eskiden kendi
// ayrı cevabıydı: Layout API'ye yalnız mount anında sorardı ve sonra hiçbir
// şey onu değiştiremezdi — panoda ya da bileşen sayfasında kontrol çalıştıran
// operatör, yanındaki ekran kurulanı çoktan söylerken rozetin tam sayfa
// yüklemesine dek tirede kaldığını görürdü.
//
// Çözüm ikinci bir istek ya da yoklama değil. Sistem geneli
// managed-services yükünü alan her ekran cevabı ZATEN elinde tutar; bu depo
// onu bıraktıkları yerdir ve kenar çubuğu oradan okur.

// The consumers of this payload decode it into three different row shapes,
// so this store reads the one field it needs and trusts nothing else about
// them. Anything that is not literally true or false is an unobserved row:
// a value this panel cannot read is not an observation it has made.
// Bu yükün tüketicileri onu üç ayrı satır biçimine çözer; bu depo ihtiyacı
// olan tek alanı okur ve gerisine güvenmez. Düpedüz true ya da false olmayan
// her şey gözlenmemiş satırdır: panelin okuyamadığı bir değer, yaptığı bir
// gözlem değildir.
export interface ComponentCensusRow {
    readonly is_installed?: unknown;
}

/**
 * `number` — how many components are known installed.
 * `null`   — this host has rows but none has been observed: no census exists.
 * `undefined` — nothing has been published yet, so there is nothing to show.
 *
 * `null` ile `undefined` aynı şey değildir: biri "sayım yapılmadı", diğeri
 * "henüz cevap gelmedi" demektir; rozet ikisini farklı çizer.
 */
export type ComponentCensus = number | null | undefined;

/**
 * A row nobody has looked at joins NEITHER side of the count (R-040). A host
 * whose every row is unobserved therefore has no number at all, and saying
 * "0 installed" there would report an inventory nobody took.
 *
 * Bakılmamış satır sayımın HİÇBİR yakasına katılmaz (R-040). Her satırı
 * gözlenmemiş makinenin gösterilecek sayısı yoktur.
 */
export function componentCensusFrom(rows: readonly ComponentCensusRow[]): number | null {
    const observed = rows.filter((row) => typeof row.is_installed === 'boolean');
    if (rows.length > 0 && observed.length === 0) return null;
    return observed.filter((row) => row.is_installed === true).length;
}

let census: ComponentCensus = undefined;
const listeners = new Set<() => void>();

/**
 * Publish a host-wide snapshot. Only a full catalogue payload may be passed:
 * a filtered or partial list would under-count the host.
 *
 * Yalnız sistem geneli tam katalog yükü verilebilir; süzülmüş liste makineyi
 * eksik sayardı.
 */
export function publishComponentCensus(rows: readonly ComponentCensusRow[]): void {
    const next = componentCensusFrom(rows);
    if (next === census) return;
    census = next;
    for (const listener of listeners) listener();
}

export function readComponentCensus(): ComponentCensus {
    return census;
}

export function subscribeComponentCensus(listener: () => void): () => void {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
    };
}

/** Test seam: a fresh session starts with no published answer. */
/** Test dikişi: taze oturum yayınlanmış cevapsız başlar. */
export function resetComponentCensus(): void {
    census = undefined;
    for (const listener of listeners) listener();
}

export function useComponentCensus(): ComponentCensus {
    return useSyncExternalStore(
        subscribeComponentCensus,
        readComponentCensus,
        readComponentCensus,
    );
}
