import { useCallback, useEffect, useState } from 'react';
import { HelpCircle, Lightbulb, Wrench, X } from 'lucide-react';
import { useI18n } from '../i18n';
import type { HelpContent } from '../help/serviceHelp';
import { Checking, CouldNotCheck } from './ui';

// The help drawer: every management page gets a "Help" button that opens
// page-specific guidance — what this component is, practical tips, and
// what-you-see → what-to-do troubleshooting. Requested verbatim by the
// operator (25 Jul): "yönetim sayfasında bir yardım butonu olsun ve sayfa
// ile ilgili yardım alınabilsin... Korkutmasın yardımcı olsun."
//
// Yardım çekmecesi: her yönetim sayfası, sayfaya özgü rehberliği açan bir
// "Yardım" düğmesi alır — bu bileşen nedir, pratik ipuçları ve ne-görüyorsan
// → ne-yapacaksın sorun giderme. Operatörün birebir isteği (25 Tem):
// "yönetim sayfasında bir yardım butonu olsun ve sayfa ile ilgili yardım
// alınabilsin... Korkutmasın yardımcı olsun."
//
// The help texts (about 100 KiB for both languages) are fetched when the drawer
// is first opened, not with every page that carries the button: they were a
// static part of the Settings, domain and service routes and had brought the
// Settings route to 0.29 KiB of its limit (9 Oct 2026). Every component has
// help (its own entry or the one for its kind), so the button is always there;
// the drawer opens at once and says it is fetching, or that it could not.
//
// Yardım metinleri (iki dil için yaklaşık 100 KiB) düğmeyi taşıyan her sayfayla
// değil, çekmece ilk açıldığında getirilir. Her bileşenin yardımı vardır; bu
// yüzden düğme hep yerindedir. Çekmece hemen açılır ve metni getirdiğini ya da
// getiremediğini söyler.
type HelpState = { state: 'loading' } | { state: 'known'; help: HelpContent } | { state: 'unknown' };

export function HelpButton({ serviceId, kind, name }: { serviceId: string; kind?: string; name: string }) {
    const { t, locale } = useI18n();
    const [open, setOpen] = useState(false);
    const [help, setHelp] = useState<HelpState>({ state: 'loading' });
    const [attempt, setAttempt] = useState(0);

    useEffect(() => {
        if (!open) return;
        let current = true;
        setHelp({ state: 'loading' });
        import('../help/serviceHelp').then(
            (module) => {
                if (!current) return;
                const found = module.getServiceHelp(serviceId, kind ?? 'service', locale);
                setHelp(found ? { state: 'known', help: found } : { state: 'unknown' });
            },
            () => {
                if (current) setHelp({ state: 'unknown' });
            },
        );
        return () => {
            current = false;
        };
    }, [open, attempt, serviceId, kind, locale]);

    const close = useCallback(() => setOpen(false), []);

    return (
        <>
            <button
                onClick={() => setOpen(true)}
                title={t('help.buttonHint')}
                className="inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1.5 text-xs font-medium text-fg transition-colors hover:bg-surface-2"
            >
                <HelpCircle className="h-3.5 w-3.5 text-primary" />
                {t('help.button')}
            </button>
            {open && <HelpDrawer name={name} help={help} onRetry={() => setAttempt((n) => n + 1)} onClose={close} />}
        </>
    );
}

function HelpDrawer({ name, help, onRetry, onClose }: { name: string; help: HelpState; onRetry: () => void; onClose: () => void }) {
    const { t } = useI18n();

    // Esc closes — help must be effortless to leave, or nobody opens it twice.
    // Esc kapatır — yardımdan çıkmak zahmetsiz olmalı, yoksa kimse ikinci kez açmaz.
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onClose]);

    return (
        <div className="fixed inset-0 z-50" role="dialog" aria-modal="true">
            {/* Informational drawer: backdrop click MAY close it — nothing is
                lost, unlike the destructive dialogs. / Bilgilendirme çekmecesi:
                arka plana tıklamak kapatabilir — yıkıcı pencerelerin aksine
                kaybolacak bir şey yok. */}
            <div className="absolute inset-0 bg-scrim/70" onClick={onClose} />
            <aside className="absolute inset-y-0 right-0 flex w-full max-w-md flex-col border-l border-border bg-surface shadow-2xl">
                <header className="flex items-center gap-2.5 border-b border-border px-5 py-4">
                    <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <HelpCircle className="h-4.5 w-4.5" />
                    </span>
                    <div className="min-w-0">
                        <h2 className="truncate text-sm font-semibold text-fg">{t('help.title', { name })}</h2>
                        <p className="text-xs text-fg-subtle">{t('help.subtitle')}</p>
                    </div>
                    <button onClick={onClose} className="ml-auto rounded-lg p-1.5 text-fg-muted transition-colors hover:bg-surface-2 hover:text-fg" aria-label={t('common.back')}>
                        <X className="h-4 w-4" />
                    </button>
                </header>

                <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-5 py-5">
                    {help.state === 'loading' && <Checking label={t('help.loading')} />}
                    {help.state === 'unknown' && <CouldNotCheck text={t('help.unknown')} onRetry={onRetry} />}
                    {help.state === 'known' && <HelpText help={help.help} />}
                </div>
            </aside>
        </div>
    );
}

function HelpText({ help }: { help: HelpContent }) {
    const { t } = useI18n();
    return (
        <>
            <section>
                <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-fg-subtle">{t('help.what')}</h3>
                <p className="text-sm leading-relaxed text-fg">{help.what}</p>
            </section>

            {help.tips.length > 0 && (
                <section>
                    <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-fg-subtle">{t('help.tips')}</h3>
                    <ul className="space-y-2.5">
                        {help.tips.map((tip, i) => (
                            <li key={i} className="flex gap-2.5 text-sm leading-relaxed text-fg">
                                <Lightbulb className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                                <span>{tip}</span>
                            </li>
                        ))}
                    </ul>
                </section>
            )}

            {help.troubleshoot.length > 0 && (
                <section>
                    <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-fg-subtle">{t('help.troubleshoot')}</h3>
                    <ul className="space-y-3">
                        {help.troubleshoot.map((item, i) => (
                            <li key={i} className="rounded-xl border border-border bg-surface-2/50 p-3">
                                <p className="mb-1 flex items-start gap-2 text-sm font-medium text-fg">
                                    <Wrench className="mt-0.5 h-3.5 w-3.5 shrink-0 text-fg-muted" />
                                    {item.symptom}
                                </p>
                                <p className="pl-5.5 text-sm leading-relaxed text-fg-muted">{item.fix}</p>
                            </li>
                        ))}
                    </ul>
                </section>
            )}

            <p className="rounded-xl bg-primary/5 p-3 text-xs leading-relaxed text-fg-muted">{t('help.footer')}</p>
        </>
    );
}
