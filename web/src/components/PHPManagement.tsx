import { useState } from 'react';
import { FileCode, Puzzle, FileText, type LucideIcon } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { PHPExtendedConfig } from './PHPExtendedConfig';
import { KnownEmpty, RemoteGate } from './ui';
import { decodeList, useRemote } from '../lib/remote';
import { showToast } from './Toast';
import { useI18n } from '../i18n';

interface PHPManagementProps {
    versions: string[];
    onBack: () => void;
}

interface PHPExtension {
    name: string;
    enabled: boolean;
}

// PHP on ServiceShell. The shell shows the default php-fpm status + start/stop;
// the version selector below drives per-version config: real extension toggles
// and the ini editor. No mock data — extensions come from the live install.
//
// PHP, ServiceShell üzerinde. Kabuk, varsayılan php-fpm durumunu + başlat/durdur
// gösterir; alttaki sürüm seçici, sürüm-başına yapılandırmayı sürer: gerçek
// eklenti anahtarları ve ini düzenleyici. Sahte veri yok — eklentiler canlı
// kurulumdan gelir.
export function PHPManagement({ versions, onBack }: PHPManagementProps) {
    const { t } = useI18n();
    // versions[] now carries only REAL versions (B3b: the "default" sentinel
    // is dead — on Arch the single php-fpm reports its true version too).
    // Empty means PHP is not installed; the shell's install button handles it.
    // versions[] artık yalnız GERÇEK sürümler taşır (B3b: "default" sentinel'i
    // öldü — Arch'ta tek php-fpm de gerçek sürümünü bildirir). Boşsa PHP
    // kurulu değildir; kabuğun kurulum düğmesi bunu karşılar.
    const [version, setVersion] = useState(versions[0] ?? '');
    const [tab, setTab] = useState<'extensions' | 'config'>('extensions');
    // The extensions of the chosen version are being read, could not be read,
    // or known (9 Oct 2026). Before, a read that failed or had not answered
    // was an empty list: the page said "No extensions found" on a server that
    // has them. A switch exists only for an extension the server listed, and
    // it is off while the list is the earlier answer or is being read again.
    // Seçili sürümün eklentileri okunuyor, okunamadı ya da biliniyor. Önceden
    // başarısız ya da yanıtlanmamış okuma boş listeydi. Anahtar yalnız
    // sunucunun listelediği eklenti için vardır.
    const extensions = useRemote(
        version ? `/api/v1/php/extensions?version=${encodeURIComponent(version)}` : null,
        decodeList<PHPExtension>,
    );
    // The switch a person has just pressed, until the server's answer to it
    // has been read back.
    // Kişinin az önce bastığı anahtar; sunucunun yanıtı geri okunana dek.
    const [pending, setPending] = useState<{ name: string; enabled: boolean } | null>(null);

    const toggleExt = async (ext: PHPExtension) => {
        if (pending) return;
        const enabled = !ext.enabled;
        setPending({ name: ext.name, enabled });
        try {
            const res = await fetch('/api/v1/php/extensions', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ version, extension: ext.name, enabled }),
            });
            if (!res.ok) throw new Error();
        } catch {
            showToast('error', t('php.toggleFailed'));
        }
        // What the server now says is what the switch shows, whether the
        // change was accepted or refused.
        // Değişiklik kabul edilse de reddedilse de anahtar sunucunun şimdi
        // söylediğini gösterir.
        await extensions.retry();
        setPending(null);
    };

    return (
        <ServiceShell serviceId="php-fpm" name="PHP-FPM" icon={FileCode} onBack={onBack}>
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center gap-1 border-b border-border">
                    <Tab active={tab === 'extensions'} onClick={() => setTab('extensions')} icon={Puzzle} label={t('php.tab.extensions')} />
                    <Tab active={tab === 'config'} onClick={() => setTab('config')} icon={FileText} label={t('php.tab.config')} />
                </div>
                {versions.length > 1 && (
                    <label className="flex items-center gap-2 text-sm text-fg-muted">
                        {t('php.version')}
                        <select
                            value={version}
                            onChange={(e) => setVersion(e.target.value)}
                            className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-sm font-medium text-fg outline-none focus:border-primary"
                        >
                            {versions.map((v) => (
                                <option key={v} value={v}>
                                    PHP {v}
                                </option>
                            ))}
                        </select>
                    </label>
                )}
            </div>

            {tab === 'extensions' ? (
                <div className="min-h-[11rem]">
                    <RemoteGate
                        remote={extensions.remote}
                        checking={t('php.extensions.checking', { version })}
                        failed={t('php.extensions.unknown', { version })}
                        onRetry={() => void extensions.retry()}
                        busy={extensions.reading}
                        className="py-2"
                    >
                        {(shown) => (shown.value.length === 0 ? (
                            <KnownEmpty of={shown} icon={Puzzle} title={t('php.emptyExtensions')} />
                        ) : (
                            <div className="rounded-xl border border-border bg-surface p-5">
                                <h3 className="mb-4 text-sm font-semibold text-fg">{t('php.installedExtensions')}</h3>
                                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
                                    {shown.value.map((ext) => (
                                        <label
                                            key={ext.name}
                                            className="flex min-h-[2.75rem] cursor-pointer items-center justify-between gap-3 rounded-lg border border-border bg-surface-2/50 px-3 py-2 hover:bg-surface-2"
                                        >
                                            <span className="truncate font-mono text-sm text-fg-muted">{ext.name}</span>
                                            <input
                                                type="checkbox"
                                                checked={pending?.name === ext.name ? pending.enabled : ext.enabled}
                                                disabled={shown.stale || pending !== null}
                                                onChange={() => void toggleExt(ext)}
                                                className="h-4 w-8 shrink-0 cursor-pointer appearance-none rounded-full bg-surface-3 transition-colors checked:bg-primary relative before:absolute before:top-0.5 before:left-0.5 before:h-3 before:w-3 before:rounded-full before:bg-white before:transition-transform checked:before:translate-x-4 disabled:cursor-default disabled:opacity-60"
                                            />
                                        </label>
                                    ))}
                                </div>
                            </div>
                        ))}
                    </RemoteGate>
                </div>
            ) : (
                <PHPExtendedConfig version={version} />
            )}
        </ServiceShell>
    );
}

function Tab({ active, onClick, icon: Icon, label }: { active: boolean; onClick: () => void; icon: LucideIcon; label: string }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" />
            {label}
        </button>
    );
}
