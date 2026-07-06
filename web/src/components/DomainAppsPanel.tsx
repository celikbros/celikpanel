import { useEffect, useState } from 'react';
import { LayoutGrid, Download, ExternalLink, Loader2 } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState } from './ui';

interface App {
    id: string;
    name: string;
    description: string;
    icon: string;
    requires_db: boolean;
    requires_php: boolean;
}

// The application catalog for one domain: pick an app, install it. Each entry
// is a curated recipe (site + database) — not a third-party marketplace. The
// install is a real download+configure on the server; the result is a link to
// finish the app's own setup.
// Bir domain için uygulama kataloğu: bir uygulama seç, kur. Her giriş kürlü
// bir reçetedir (site + veritabanı) — üçüncü parti pazar yeri değil. Kurulum
// sunucuda gerçek indirme+yapılandırmadır; sonuç, uygulamanın kendi
// kurulumunu bitirmek için bir bağlantıdır.
export function DomainAppsPanel({ domainId }: { domainId: number; domainName: string }) {
    const { t } = useI18n();
    const [apps, setApps] = useState<App[]>([]);
    const [installing, setInstalling] = useState<string | null>(null);
    const [setupUrl, setSetupUrl] = useState<string | null>(null);

    useEffect(() => {
        fetch('/api/v1/apps')
            .then((r) => (r.ok ? r.json() : { apps: [] }))
            .then((d) => setApps(d.apps || []))
            .catch(() => {});
    }, []);

    const install = async (app: App) => {
        if (!confirm(t('apps.confirmInstall', { name: app.name }))) return;
        setInstalling(app.id);
        setSetupUrl(null);
        try {
            const r = await fetch(`/api/v1/domains/${domainId}/apps/install`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ app: app.id }),
            });
            const data = await r.json();
            if (!r.ok || !data.success) throw new Error(data.error || '');
            showToast('success', t('apps.installed', { name: app.name }));
            if (data.setup_url) setSetupUrl(data.setup_url);
        } catch (e) {
            showToast('error', (e as Error).message || t('apps.installFailed'));
        } finally {
            setInstalling(null);
        }
    };

    if (apps.length === 0) {
        return <EmptyState icon={LayoutGrid} title={t('apps.empty')} />;
    }

    return (
        <div>
            <p className="mb-4 text-sm text-fg-muted">{t('apps.hint')}</p>

            {setupUrl && (
                <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-success/30 bg-success/10 p-3">
                    <span className="text-sm text-success">{t('apps.finishSetup')}</span>
                    <a
                        href={setupUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1.5 rounded-lg border border-success/40 bg-surface px-3 py-1.5 text-sm font-medium text-fg hover:bg-surface-2"
                    >
                        <ExternalLink className="h-4 w-4" />
                        {t('apps.openSetup')}
                    </a>
                </div>
            )}

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {apps.map((app) => (
                    <div key={app.id} className="flex items-start gap-3 rounded-xl border border-border bg-surface p-4">
                        <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                            <LayoutGrid className="h-5 w-5" />
                        </span>
                        <div className="min-w-0 flex-1">
                            <div className="font-semibold text-fg">{app.name}</div>
                            <p className="mb-3 text-xs text-fg-muted">{app.description}</p>
                            <Button
                                variant="primary"
                                icon={installing === app.id ? undefined : Download}
                                disabled={installing !== null}
                                onClick={() => install(app)}
                            >
                                {installing === app.id ? (
                                    <>
                                        <Loader2 className="h-4 w-4 animate-spin" />
                                        {t('apps.installing')}
                                    </>
                                ) : (
                                    t('apps.install')
                                )}
                            </Button>
                        </div>
                    </div>
                ))}
            </div>
        </div>
    );
}
