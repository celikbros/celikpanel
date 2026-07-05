import { useState, useEffect } from 'react';
import { FileCode, Puzzle, FileText, type LucideIcon } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { PHPExtendedConfig } from './PHPExtendedConfig';
import { EmptyState } from './ui';
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
    const [version, setVersion] = useState(versions[0] ?? 'default');
    const [tab, setTab] = useState<'extensions' | 'config'>('extensions');
    const [extensions, setExtensions] = useState<PHPExtension[]>([]);

    useEffect(() => {
        fetch(`/api/v1/php/extensions?version=${version}`)
            .then((r) => (r.ok ? r.json() : []))
            .then((data) => setExtensions(data || []))
            .catch(() => setExtensions([]));
    }, [version]);

    const toggleExt = async (ext: PHPExtension) => {
        const enabled = !ext.enabled;
        setExtensions((prev) => prev.map((e) => (e.name === ext.name ? { ...e, enabled } : e)));
        try {
            const res = await fetch('/api/v1/php/extensions', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ version, extension: ext.name, enabled }),
            });
            if (!res.ok) throw new Error();
        } catch {
            setExtensions((prev) => prev.map((e) => (e.name === ext.name ? { ...e, enabled: ext.enabled } : e)));
            showToast('error', t('php.toggleFailed'));
        }
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
                            className="rounded-lg border border-border bg-surface-2 px-3 py-1.5 text-sm font-medium text-fg outline-none focus:border-primary focus:ring-2 focus:ring-primary/30"
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
                extensions.length === 0 ? (
                    <EmptyState icon={Puzzle} title={t('php.emptyExtensions')} />
                ) : (
                    <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
                        <h3 className="mb-4 text-sm font-semibold text-fg">{t('php.installedExtensions')}</h3>
                        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
                            {extensions.map((ext) => (
                                <label
                                    key={ext.name}
                                    className="flex cursor-pointer items-center justify-between gap-3 rounded-lg border border-border bg-surface-2/50 px-3 py-2 hover:bg-surface-2"
                                >
                                    <span className="truncate font-mono text-sm text-fg-muted">{ext.name}</span>
                                    <input
                                        type="checkbox"
                                        checked={ext.enabled}
                                        onChange={() => toggleExt(ext)}
                                        className="h-4 w-8 shrink-0 cursor-pointer appearance-none rounded-full bg-surface-3 transition-colors checked:bg-primary relative before:absolute before:top-0.5 before:left-0.5 before:h-3 before:w-3 before:rounded-full before:bg-white before:transition-transform checked:before:translate-x-4"
                                    />
                                </label>
                            ))}
                        </div>
                    </div>
                )
            ) : (
                <div className="rounded-xl border border-border bg-surface shadow-card">
                    <PHPExtendedConfig version={version} />
                </div>
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
