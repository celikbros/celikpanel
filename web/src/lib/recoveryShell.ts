/** Optional static recovery shell; it never caches authenticated responses. */
let preparation: Promise<void> | null = null;
export function prepareRecoveryShell(): Promise<void> {
    if (!globalThis.isSecureContext || typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return Promise.resolve();
    if (preparation) return preparation;
    preparation = new Promise<void>(resolve => {
        const timeout = setTimeout(resolve, 2500);
        Promise.resolve().then(async () => {
            const existing = await navigator.serviceWorker.getRegistration('/');
            const worker = existing?.active ?? existing?.waiting ?? existing?.installing;
            if (worker && new URL(worker.scriptURL).pathname !== '/recovery-worker.js') return;
            await navigator.serviceWorker.register('/recovery-worker.js', { scope: '/', updateViaCache: 'none' });
            await navigator.serviceWorker.ready;
        }).catch(() => { /* Optional support cannot alter update authorization. */ })
            .finally(() => { clearTimeout(timeout); resolve(); });
    });
    return preparation;
}
