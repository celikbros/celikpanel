import { useEffect, useState, type ReactNode } from 'react';
import { ArrowLeft, Play, Square, RotateCw, Download, type LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { EmptyState, StatusDot } from './ui';

interface ManagedService {
    id: string;
    name: string;
    is_installed: boolean;
    status: string;
}

// One honest shell for every service-management page: a header with the
// real status (sourced from managed-services, not the unreliable per-unit
// status endpoint) and start/stop/restart. When a service isn't installed,
// it says so instead of rendering empty controls over invented data.
//
// Her servis-yönetim sayfası için tek dürüst kabuk: gerçek durumlu bir
// başlık (güvenilmez birim-durum uç noktasından değil, managed-services'ten)
// ve başlat/durdur/yeniden başlat. Bir servis kurulu değilse, uydurma veri
// üstüne boş kontroller çizmek yerine bunu söyler.
export function ServiceShell({
    serviceId,
    unitName,
    name,
    icon: Icon,
    onBack,
    children,
}: {
    serviceId: string;
    unitName?: string;
    name: string;
    icon: LucideIcon;
    onBack: () => void;
    children: ReactNode;
}) {
    const { t } = useI18n();
    const { role } = useAuth();
    const [svc, setSvc] = useState<ManagedService | null>(null);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [installing, setInstalling] = useState(false);

    const load = () =>
        fetch('/api/v1/managed-services')
            .then((r) => (r.ok ? r.json() : { services: [] }))
            .then((data: { services: ManagedService[] }) => setSvc((data.services || []).find((s) => s.id === serviceId) ?? null))
            .catch(() => {})
            .finally(() => setLoading(false));

    useEffect(() => {
        load();
    }, [serviceId]);

    const running = svc?.status?.includes('running') ?? false;
    const installed = svc?.is_installed ?? false;

    // One-click install of an absent service (admin only). The panel ships
    // with nothing installed; the agent apt-installs the whitelisted packages
    // for this host and starts the unit. Honest failures (non-root, distro
    // unsupported) surface as the real error.
    // Eksik bir servisi tek tıkla kur (yalnız admin). Panel hiçbir şey kurulu
    // gelmez; agent bu makine için whitelist'teki paketleri kurar ve unit'i
    // başlatır. Dürüst hatalar (root değil, dağıtım desteklenmiyor) gerçek
    // hata olarak görünür.
    const install = async () => {
        setInstalling(true);
        try {
            const r = await fetch('/api/v1/service/install', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ service_id: serviceId }),
            });
            if (!r.ok) {
                showToast('error', (await r.text()).trim() || t('svc.actionFailed'));
                return;
            }
            showToast('success', t('svc.installed', { name }));
            await load();
        } finally {
            setInstalling(false);
        }
    };

    const act = async (action: 'start' | 'stop' | 'restart') => {
        const key = action === 'start' ? 'svc.confirmStart' : action === 'stop' ? 'svc.confirmStop' : 'svc.confirmRestart';
        if (!confirm(t(key, { name }))) return;
        setBusy(true);
        try {
            const r = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: unitName ?? serviceId, action }),
            });
            if (!r.ok) throw new Error();
            await new Promise((res) => setTimeout(res, 1500));
            await load();
        } catch {
            showToast('error', t('svc.actionFailed'));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="p-6 md:p-8">
            {/* Header */}
            <button onClick={onBack} className="mb-3 inline-flex items-center gap-1.5 text-sm text-fg-muted hover:text-fg">
                <ArrowLeft className="h-4 w-4" />
                {t('nav.services')}
            </button>

            <div className="mb-6 flex flex-wrap items-center gap-3">
                <span className="flex h-11 w-11 items-center justify-center rounded-xl bg-primary/10 text-primary">
                    <Icon className="h-6 w-6" />
                </span>
                <div>
                    <h1 className="text-2xl font-bold tracking-tight">{name}</h1>
                    <p className="flex items-center gap-1.5 text-sm text-fg-muted">
                        {loading ? (
                            t('common.loading')
                        ) : !installed ? (
                            <span className="text-fg-subtle">{t('svc.notInstalled')}</span>
                        ) : (
                            <>
                                <StatusDot ok={running} />
                                <span className={running ? 'text-success' : 'text-fg-subtle'}>
                                    {running ? t('services.running') : t('services.stopped')}
                                </span>
                            </>
                        )}
                    </p>
                </div>

                {installed && (
                    <div className="ml-auto flex items-center gap-2">
                        <CtrlButton icon={Play} label={t('services.start')} tone="success" disabled={busy || running} onClick={() => act('start')} />
                        <CtrlButton icon={Square} label={t('services.stop')} tone="danger" disabled={busy || !running} onClick={() => act('stop')} />
                        <CtrlButton icon={RotateCw} label={t('services.restart')} tone="warning" disabled={busy} onClick={() => act('restart')} />
                    </div>
                )}
            </div>

            {loading ? (
                <div className="flex items-center justify-center py-16">
                    <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
                </div>
            ) : !installed ? (
                <EmptyState
                    icon={Icon}
                    title={t('svc.notInstalled')}
                    hint={t('svc.notInstalledHint', { name })}
                    action={
                        role === 'admin' ? (
                            <CtrlButton
                                icon={Download}
                                label={installing ? t('svc.installing') : t('svc.install', { name })}
                                tone="success"
                                disabled={installing}
                                onClick={install}
                            />
                        ) : undefined
                    }
                />
            ) : (
                children
            )}
        </div>
    );
}

function CtrlButton({
    icon: Icon,
    label,
    tone,
    disabled,
    onClick,
}: {
    icon: LucideIcon;
    label: string;
    tone: 'success' | 'danger' | 'warning';
    disabled?: boolean;
    onClick: () => void;
}) {
    const tones = {
        success: 'text-success border-success/30 hover:bg-success/10',
        danger: 'text-danger border-danger/30 hover:bg-danger/10',
        warning: 'text-warning border-warning/30 hover:bg-warning/10',
    }[tone];
    return (
        <button
            onClick={onClick}
            disabled={disabled}
            className={`inline-flex items-center gap-1.5 rounded-lg border bg-surface px-3 py-1.5 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${tones}`}
        >
            <Icon className="h-4 w-4" />
            {label}
        </button>
    );
}
