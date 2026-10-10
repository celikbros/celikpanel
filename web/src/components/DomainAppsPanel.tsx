import { useState } from 'react';
import { LayoutGrid, Download, ExternalLink, Loader2 } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, KnownEmpty, RemoteGate, ResultUnknown } from './ui';
import { apiErrorText, readApiError } from '../lib/apiError';
import { decodeListIn, useRemote } from '../lib/remote';
import { useLostAnswer } from '../lib/lostAnswer';

interface App {
    id: string;
    name: string;
    description: string;
    icon: string;
    requires_db: boolean;
    requires_php: boolean;
}

const APPS_URL = '/api/v1/apps';
const decodeApps = (raw: unknown) => decodeListIn<App>(raw, 'apps');

// The application catalog for one domain: pick an app, install it. Each entry
// is a curated recipe (site + database) — not a third-party marketplace. The
// install is a real download+configure on the server; the result is a link to
// finish the app's own setup.
//
// "No applications available" is said only for a catalogue the server sent
// empty. An install whose answer was lost is not offered again blind: the
// server keeps no identity for it, so a second request would install a second
// time over whatever the first one did.
// Bir domain için uygulama kataloğu: bir uygulama seç, kur. Her giriş kürlü
// bir reçetedir (site + veritabanı) — üçüncü parti pazar yeri değil. Kurulum
// sunucuda gerçek indirme+yapılandırmadır; sonuç, uygulamanın kendi
// kurulumunu bitirmek için bir bağlantıdır.
// "Kullanılabilir uygulama yok" yalnız sunucunun boş gönderdiği katalog için
// söylenir. Yanıtı yiten kurulum körlemesine yeniden sunulmaz: sunucu onun
// için kimlik saklamaz.
export function DomainAppsPanel({ domainId }: { domainId: number; domainName: string }) {
    const { t } = useI18n();
    const apps = useRemote(APPS_URL, decodeApps);
    const answer = useLostAnswer(() => apps.retry());
    const [installing, setInstalling] = useState<string | null>(null);
    const [setupUrl, setSetupUrl] = useState<string | null>(null);

    const install = async (app: App) => {
        if (answer.holding || apps.remote.state !== 'known') return;
        if (!confirm(t('apps.confirmInstall', { name: app.name }))) return;
        setInstalling(app.id);
        setSetupUrl(null);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/apps/install`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ app: app.id }),
            });
            if (!res) return;
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t, 'apps.installFailed'));
                return;
            }
            // An accepted request whose body says the install failed is the
            // server's own verdict; one whose body cannot be read is no
            // verdict at all, so what happened is not known.
            // Gövdesi kurulumun başarısız olduğunu söyleyen kabul edilmiş
            // istek, sunucunun kendi hükmüdür; gövdesi okunamayan ise hüküm
            // değildir: ne olduğu bilinmez.
            let data: { success?: boolean; error?: string; setup_url?: string };
            try {
                data = await res.json();
            } catch {
                answer.lose();
                return;
            }
            if (!data.success) {
                showToast('error', data.error || t('apps.installFailed'));
                return;
            }
            showToast('success', t('apps.installed', { name: app.name }));
            answer.settle();
            if (data.setup_url) setSetupUrl(data.setup_url);
        } finally {
            setInstalling(null);
        }
    };

    return (
        <RemoteGate
            remote={apps.remote}
            checking={t('apps.checking')}
            failed={t('apps.unknown')}
            onRetry={() => void apps.retry()}
            busy={apps.reading}
        >
            {(shown) => shown.value.length === 0 ? (
                <KnownEmpty of={shown} icon={LayoutGrid} title={t('apps.empty')} />
            ) : (
                <div>
                    <p className="mb-4 text-sm text-fg-muted">{t('apps.hint')}</p>

                    <ResultUnknown answer={answer} where={t('apps.resultUnknownWhere')} className="mb-4" />

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
                        {shown.value.map((app) => (
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
                                        disabled={installing !== null || shown.stale || answer.holding}
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
            )}
        </RemoteGate>
    );
}
