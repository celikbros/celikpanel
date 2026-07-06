import { useEffect, useState } from 'react';
import { Settings, Play, Square, RotateCw, RefreshCw, ScanSearch } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { PageHeader, StatusDot, EmptyState } from './ui';

interface ManagedService {
    id: string;
    name: string;
    description: string;
    icon: string;
    category: string;
    versions: string[];
    status: string;
    is_installed: boolean;
}

interface ServiceListProps {
    onSelectConfig: (path: string) => void;
    onManageService?: (serviceId: string, versions: string[]) => void;
}

// Category pills: categorical colors that stay readable in both themes.
// Kategori etiketleri: iki temada da okunur kalan kategorik renkler.
const categoryStyle: Record<string, string> = {
    web: 'bg-blue-100 text-blue-700 dark:bg-blue-500/15 dark:text-blue-300',
    database: 'bg-violet-100 text-violet-700 dark:bg-violet-500/15 dark:text-violet-300',
    email: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
    security: 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300',
    dns: 'bg-teal-100 text-teal-700 dark:bg-teal-500/15 dark:text-teal-300',
    ftp: 'bg-slate-200 text-slate-700 dark:bg-slate-500/20 dark:text-slate-300',
    cache: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
};

// Services as a Plesk-style data table. Status comes straight from
// managed-services (the reliable source: "active (running)" vs
// "inactive (dead)"), not the wrapper-unit endpoint that misreports.
//
// Servisler Plesk tarzı bir veri tablosu olarak. Durum doğrudan
// managed-services'ten gelir (güvenilir kaynak), yanlış raporlayan
// wrapper-unit uç noktasından değil.
export function ServiceList({ onManageService }: ServiceListProps) {
    const { t } = useI18n();
    const [services, setServices] = useState<ManagedService[]>([]);
    const [scannedAt, setScannedAt] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);
    const [scanning, setScanning] = useState(false);
    const [busy, setBusy] = useState<string | null>(null);

    useEffect(() => {
        loadServices();
    }, []);

    // Reads the CACHED scan — instant, never probes the system. A fresh
    // probe is the explicit user action below (scan).
    // ÖNBELLEKTEKİ taramayı okur — anlık, sistemi asla yoklamaz. Taze
    // yoklama aşağıdaki açık kullanıcı eylemidir (scan).
    const loadServices = () => {
        setLoading(true);
        fetch('/api/v1/managed-services')
            .then((res) => res.json())
            .then((data) => {
                setServices(data?.services || []);
                setScannedAt(data?.scanned_at ?? null);
            })
            .catch(() => showToast('error', t('common.error')))
            .finally(() => setLoading(false));
    };

    const scan = async () => {
        setScanning(true);
        try {
            const res = await fetch('/api/v1/managed-services/scan', { method: 'POST' });
            if (!res.ok) throw new Error();
            const data = await res.json();
            setServices(data?.services || []);
            setScannedAt(data?.scanned_at ?? null);
        } catch {
            showToast('error', t('services.scanFailed'));
        } finally {
            setScanning(false);
        }
    };

    const resolveUnit = (serviceId: string, version?: string) => {
        if (serviceId === 'php-fpm') return version && version !== 'default' ? `php${version}-fpm` : 'php-fpm';
        return serviceId;
    };

    const handleAction = async (service: ManagedService, action: 'start' | 'stop' | 'restart') => {
        setBusy(service.id);
        try {
            const res = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: resolveUnit(service.id, service.versions[0]), action }),
            });
            if (!res.ok) throw new Error();
            await new Promise((r) => setTimeout(r, 1000));
            loadServices();
        } catch {
            showToast('error', t('services.actionFailed'));
        } finally {
            setBusy(null);
        }
    };

    const isRunning = (s: ManagedService) => s.status?.toLowerCase().includes('running');

    return (
        <div className="p-6 md:p-8">
            <PageHeader
                title={t('nav.services')}
                subtitle={t('services.subtitle')}
                breadcrumb={[t('common.home'), t('nav.services')]}
            />

            <div className="mb-4 flex flex-wrap items-center gap-3">
                <button
                    onClick={scan}
                    disabled={scanning}
                    className="inline-flex items-center gap-2 rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-sm font-medium text-fg transition-colors hover:bg-surface-2 disabled:opacity-50"
                >
                    <RefreshCw className={`h-4 w-4 ${scanning ? 'animate-spin' : ''}`} />
                    {scanning ? t('services.scanning') : scannedAt ? t('services.rescan') : t('services.scanNow')}
                </button>
                <span className="text-xs text-fg-subtle">
                    {scannedAt
                        ? t('services.lastScan', { time: new Date(scannedAt).toLocaleString() })
                        : t('services.neverScannedShort')}
                </span>
            </div>

            {loading ? (
                <div className="flex items-center justify-center py-16">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : !scannedAt && services.length === 0 ? (
                <EmptyState
                    icon={ScanSearch}
                    title={t('services.neverScanned')}
                    hint={t('services.neverScannedHint')}
                />
            ) : (
                <div className="rounded-xl border border-border bg-surface shadow-card">
                    <p className="px-4 pt-3 text-xs text-fg-subtle">{t('common.itemsTotal', { n: services.length })}</p>
                    <div className="overflow-x-auto">
                        <table className="w-full text-sm">
                            <thead>
                                <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                    <th className="px-4 py-2.5">{t('services.col.service')}</th>
                                    <th className="px-4 py-2.5">{t('services.col.category')}</th>
                                    <th className="px-4 py-2.5">{t('services.col.version')}</th>
                                    <th className="px-4 py-2.5">{t('services.col.status')}</th>
                                    <th className="px-4 py-2.5 text-right">{''}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {services.map((s) => {
                                    const running = isRunning(s);
                                    return (
                                        <tr key={s.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                            <td className="px-4 py-3">
                                                <div className="flex items-center gap-3">
                                                    <span className="text-xl leading-none">{s.icon}</span>
                                                    <div className="min-w-0">
                                                        <div className="font-medium text-fg">{s.name}</div>
                                                        <div className="truncate text-xs text-fg-subtle">{s.description}</div>
                                                    </div>
                                                </div>
                                            </td>
                                            <td className="px-4 py-3">
                                                <span
                                                    className={`rounded-md px-2 py-0.5 text-xs font-medium ${
                                                        categoryStyle[s.category] ?? 'bg-surface-2 text-fg-muted'
                                                    }`}
                                                >
                                                    {s.category}
                                                </span>
                                            </td>
                                            <td className="px-4 py-3">
                                                <div className="flex flex-wrap gap-1">
                                                    {s.versions.length > 1 ? (
                                                        s.versions.map((v) => (
                                                            <span
                                                                key={v}
                                                                className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-xs text-fg-muted"
                                                            >
                                                                {v}
                                                            </span>
                                                        ))
                                                    ) : (
                                                        <span className="font-mono text-xs text-fg-muted">
                                                            {s.versions[0] === 'default' ? '—' : s.versions[0]}
                                                        </span>
                                                    )}
                                                </div>
                                            </td>
                                            <td className="px-4 py-3">
                                                <span className="inline-flex items-center gap-1.5 text-fg-muted">
                                                    <StatusDot ok={running} />
                                                    {running ? t('services.running') : t('services.stopped')}
                                                </span>
                                            </td>
                                            <td className="px-4 py-3">
                                                <div className="flex items-center justify-end gap-1">
                                                    <ActionIcon
                                                        title={t('services.start')}
                                                        onClick={() => handleAction(s, 'start')}
                                                        disabled={busy === s.id}
                                                        tone="success"
                                                    >
                                                        <Play className="h-4 w-4" />
                                                    </ActionIcon>
                                                    <ActionIcon
                                                        title={t('services.stop')}
                                                        onClick={() => handleAction(s, 'stop')}
                                                        disabled={busy === s.id}
                                                        tone="danger"
                                                    >
                                                        <Square className="h-4 w-4" />
                                                    </ActionIcon>
                                                    <ActionIcon
                                                        title={t('services.restart')}
                                                        onClick={() => handleAction(s, 'restart')}
                                                        disabled={busy === s.id}
                                                        tone="warning"
                                                    >
                                                        <RotateCw className="h-4 w-4" />
                                                    </ActionIcon>
                                                    <button
                                                        onClick={() => onManageService?.(s.id, s.versions)}
                                                        className="ml-1 inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1.5 text-xs font-medium text-fg transition-colors hover:bg-surface-2"
                                                    >
                                                        <Settings className="h-3.5 w-3.5" />
                                                        {t('services.manage')}
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    );
                                })}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}
        </div>
    );
}

function ActionIcon({
    children,
    title,
    onClick,
    disabled,
    tone,
}: {
    children: React.ReactNode;
    title: string;
    onClick: () => void;
    disabled?: boolean;
    tone: 'success' | 'danger' | 'warning';
}) {
    const hover = {
        success: 'hover:text-success',
        danger: 'hover:text-danger',
        warning: 'hover:text-warning',
    }[tone];
    return (
        <button
            title={title}
            onClick={onClick}
            disabled={disabled}
            className={`rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 disabled:opacity-40 ${hover}`}
        >
            {children}
        </button>
    );
}
