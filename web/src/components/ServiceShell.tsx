import { useEffect, useState, type ReactNode } from 'react';
import { ArrowLeft, Play, Square, RotateCw, Download, type LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { EmptyState, StatusDot } from './ui';
import { HelpButton } from './HelpDrawer';
import { useComponentOperation } from './ComponentOperation';

interface ManagedService {
    id: string;
    name: string;
    is_installed: boolean;
    status: string;
}

interface HostMutationReadiness {
    ready: boolean;
    code?: 'HOST_MUTATION_BUSY' | 'HOST_MUTATION_UNAVAILABLE';
    reason?: 'panel_operation_active' | 'agent_mutation_active' | 'host_lock_busy' | 'package_manager_active' | 'state_unverified';
}

function parseHostMutationReadiness(value: unknown): HostMutationReadiness | null {
    if (!value) return null;
    if (typeof value !== 'object') return null;
    const payload = value as Record<string, unknown>;
    if (typeof payload.ready !== 'boolean') return null;
    if (payload.ready) {
        return payload.code === undefined && payload.reason === undefined ? { ready: true } : null;
    }
    if (payload.code !== 'HOST_MUTATION_BUSY' && payload.code !== 'HOST_MUTATION_UNAVAILABLE') return null;
    if (
        payload.reason !== 'panel_operation_active'
        && payload.reason !== 'agent_mutation_active'
        && payload.reason !== 'host_lock_busy'
        && payload.reason !== 'package_manager_active'
        && payload.reason !== 'state_unverified'
    ) return null;
    return {
        ready: false,
        code: payload.code,
        reason: payload.reason,
    };
}

function unverifiedHostMutationReadiness(): HostMutationReadiness {
    return {
        ready: false,
        code: 'HOST_MUTATION_UNAVAILABLE',
        reason: 'state_unverified',
    };
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
    const { startInstall, locked: installLocked, catalogSnapshot } = useComponentOperation();
    const [svc, setSvc] = useState<ManagedService | null>(null);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [installConfirmationOpen, setInstallConfirmationOpen] = useState(false);
    const [installReadiness, setInstallReadiness] = useState<HostMutationReadiness | null>(null);
    const [installReadinessLoading, setInstallReadinessLoading] = useState(false);

    const load = () =>
        fetch('/api/v1/managed-services')
            .then((r) => (r.ok ? r.json() : { services: [] }))
            .then((data: { services: ManagedService[] }) => setSvc((data.services || []).find((s) => s.id === serviceId) ?? null))
            .catch(() => {})
            .finally(() => setLoading(false));

    useEffect(() => {
        load();
    }, [serviceId]);

    useEffect(() => {
        if (!catalogSnapshot) return;
        const services = catalogSnapshot.services as unknown as ManagedService[];
        setSvc(services.find((service) => service.id === serviceId) ?? null);
        setLoading(false);
    }, [catalogSnapshot, serviceId]);

    useEffect(() => {
        if (!installConfirmationOpen) return;
        let cancelled = false;
        setInstallReadiness(null);
        setInstallReadinessLoading(true);

        const checkReadiness = async () => {
            let readiness = unverifiedHostMutationReadiness();
            try {
                const response = await fetch('/api/v1/host-mutation-readiness', {
                    cache: 'no-store',
                });
                if (response.ok) {
                    readiness = parseHostMutationReadiness(await response.json())
                        ?? unverifiedHostMutationReadiness();
                }
            } catch {
                // Fail closed: an unreadable readiness response never unlocks install.
            }
            if (cancelled) return;
            setInstallReadiness(readiness);
            setInstallReadinessLoading(false);
        };

        void checkReadiness();
        return () => {
            cancelled = true;
        };
    }, [installConfirmationOpen, serviceId]);

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
    const requestInstall = () => {
        setInstallReadiness(null);
        setInstallConfirmationOpen(true);
    };

    const install = async () => {
        if (installReadiness?.ready !== true) return;
        if (installLocked) return;
        setInstallConfirmationOpen(false);
        await startInstall({ serviceId, name });
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

                {/* Help is ALWAYS reachable — installed or not, running or
                    failed. The moment a page scares its reader is exactly when
                    the button must be there (operator, 25 Jul: "Korkutmasın
                    yardımcı olsun"). / Yardım HER ZAMAN erişilebilir — kurulu
                    ya da değil, çalışıyor ya da düşmüş. Sayfanın okuyucusunu
                    korkuttuğu an, düğmenin orada olması gereken andır
                    (operatör, 25 Tem: "Korkutmasın yardımcı olsun"). */}
                <div className="ml-auto flex items-center gap-2">
                    <HelpButton serviceId={serviceId} name={name} />
                    {installed && (
                        <>
                            <CtrlButton icon={Play} label={t('services.start')} tone="success" solid disabled={busy || running} onClick={() => act('start')} />
                            <CtrlButton icon={Square} label={t('services.stop')} tone="danger" solid disabled={busy || !running} onClick={() => act('stop')} />
                            <CtrlButton icon={RotateCw} label={t('services.restart')} tone="warning" disabled={busy} onClick={() => act('restart')} />
                        </>
                    )}
                </div>
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
                                label={installLocked ? t('svc.installing') : t('svc.install', { name })}
                                tone="success"
                                disabled={installLocked}
                                onClick={requestInstall}
                            />
                        ) : undefined
                    }
                />
            ) : (
                children
            )}

            {installConfirmationOpen && (
                <ServiceInstallConfirmationDialog
                    name={name}
                    readiness={installReadiness}
                    readinessLoading={installReadinessLoading}
                    confirmDisabled={
                        installLocked
                            ? true
                            : installReadiness?.ready !== true
                    }
                    onCancel={() => setInstallConfirmationOpen(false)}
                    onConfirm={() => void install()}
                />
            )}
        </div>
    );
}

function ServiceInstallConfirmationDialog({
    name,
    readiness,
    readinessLoading,
    confirmDisabled,
    onCancel,
    onConfirm,
}: {
    name: string;
    readiness: HostMutationReadiness | null;
    readinessLoading: boolean;
    confirmDisabled: boolean;
    onCancel: () => void;
    onConfirm: () => void;
}) {
    const { t } = useI18n();
    const readinessMessage = readinessLoading
        ? t('services.mutationReadiness.checking')
        : readiness === null
            ? t('services.mutationReadiness.checking')
            : readiness.ready
                ? null
                : t(
                    `services.mutationReadiness.${readiness.reason ?? 'state_unverified'}` as Parameters<typeof t>[0],
                );

    useEffect(() => {
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') onCancel();
        };
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, [onCancel]);

    return (
        <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
            onMouseDown={(event) => {
                if (event.currentTarget === event.target) onCancel();
            }}
        >
            <div
                role="dialog"
                aria-modal="true"
                aria-labelledby="service-install-confirm-title"
                aria-describedby="service-install-confirm-description"
                className="w-full max-w-md rounded-2xl border border-border bg-surface p-6 shadow-xl"
            >
                <div className="mb-4 flex items-start gap-3">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <Download className="h-5 w-5" />
                    </span>
                    <div className="min-w-0">
                        <h3 id="service-install-confirm-title" className="text-lg font-semibold text-fg">
                            {t('services.confirm.install.title', { name })}
                        </h3>
                        <p id="service-install-confirm-description" className="mt-1 text-sm leading-5 text-fg-muted">
                            {t('services.confirm.install.description', { name })}
                        </p>
                    </div>
                </div>

                {readinessMessage && (
                    <div
                        role="status"
                        aria-live="polite"
                        className="mb-4 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning"
                    >
                        {readiness?.ready === false && (
                            <span className="font-semibold">{t('services.mutationReadiness.title')} </span>
                        )}
                        {readinessMessage}
                    </div>
                )}

                <div className="flex justify-end gap-2">
                    <button
                        type="button"
                        autoFocus
                        onClick={onCancel}
                        className="rounded-lg border border-border bg-surface px-4 py-2 text-sm font-medium text-fg hover:bg-surface-subtle"
                    >
                        {t('common.cancel')}
                    </button>
                    <button
                        type="button"
                        disabled={confirmDisabled}
                        onClick={onConfirm}
                        className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-white hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-40"
                    >
                        <Download className="h-4 w-4" />
                        {t('services.confirm.install.button', { name })}
                    </button>
                </div>
            </div>
        </div>
    );
}

function CtrlButton({
    icon: Icon,
    label,
    tone,
    disabled,
    onClick,
    solid,
}: {
    icon: LucideIcon;
    label: string;
    tone: 'success' | 'danger' | 'warning';
    disabled?: boolean;
    onClick: () => void;
    solid?: boolean;
}) {
    // The original outline elegance; only the GLYPH is solid (user feedback:
    // 'the old buttons were classier — just fill the icon').
    // Orijinal kontur zarafeti; yalnız GLİF dolu (kullanıcı geri bildirimi:
    // 'eski düğmeler daha şıktı — sadece ikonun içi dolsun').
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
            <Icon className="h-4 w-4" {...(solid ? { fill: 'currentColor' } : {})} />
            {label}
        </button>
    );
}
