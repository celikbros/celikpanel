import { useEffect, useRef, useState, type ReactNode } from 'react';
import { ArrowLeft, Play, Square, RotateCw, Download, ScanSearch, type LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { Button, EmptyState, StatusDot } from './ui';
import { HelpButton } from './HelpDrawer';
import { readApiError, apiErrorText } from '../lib/apiError';
import { decodeManagedServicesSnapshot, useComponentOperation } from './ComponentOperation';
import { publishComponentCensus } from '../lib/componentCensus';

interface ManagedService {
    id: string;
    name: string;
    /** null = never observed on this host, which is not the same as absent. */
    /** null = bu makinede hiç gözlenmedi; bu, yok demek değildir. */
    is_installed: boolean | null;
    status: string;
}

/**
 * Three answers, not two. `present` and `absent` are things this panel has
 * looked at and knows; `unknown` is everything else — a host nobody has
 * scanned, a component added to the catalogue since the last scan, and a
 * payload this page could not read. Folding `unknown` into `absent` is what
 * put a one-click Install on a page for a component that may already be
 * running (R-040).
 *
 * Üç yanıt, iki değil. `present` ve `absent` bu panelin bakıp bildiği
 * şeylerdir; `unknown` geri kalan her şeydir. `unknown`u `absent` sayarak
 * katlamak, zaten çalışıyor olabilecek bir bileşenin sayfasına tek tıkla
 * "Kur" düğmesi koymaktı (R-040).
 */
type ObservedState = 'unknown' | 'absent' | 'present';

function observedStateOf(service: ManagedService | null): ObservedState {
    if (service === null || service.is_installed === null) return 'unknown';
    return service.is_installed ? 'present' : 'absent';
}

function findService(
    services: Record<string, unknown>[],
    serviceId: string,
): ManagedService | null {
    const service = services.find((candidate) => candidate.id === serviceId);
    return service ? (service as unknown as ManagedService) : null;
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
// it says so instead of rendering empty controls over invented data — and
// when nobody has looked at this host for it, it says THAT instead, and
// offers the check rather than an install.
//
// Her servis-yönetim sayfası için tek dürüst kabuk: gerçek durumlu bir
// başlık (güvenilmez birim-durum uç noktasından değil, managed-services'ten)
// ve başlat/durdur/yeniden başlat. Bir servis kurulu değilse, uydurma veri
// üstüne boş kontroller çizmek yerine bunu söyler; bu makinede ona hiç
// bakılmamışsa BUNU söyler ve kurulum yerine kontrolü sunar.
export function ServiceShell({
    serviceId,
    unitName,
    name,
    icon: Icon,
    onBack,
    onServiceRefreshed,
    children,
}: {
    serviceId: string;
    unitName?: string;
    name: string;
    icon: LucideIcon;
    onBack: () => void;
    /**
     * Fired whenever a fresh, decoded catalogue record reaches this shell —
     * after the operator's check, and after an install finishes. A page that
     * keeps its own copy of the record (ComponentDetail) must reread it here,
     * or it would draw the pre-check facts under a resolved header.
     * Taze ve çözümlenmiş katalog kaydı kabuğa ulaştığında tetiklenir.
     */
    onServiceRefreshed?: () => void;
    children: ReactNode;
}) {
    const { t } = useI18n();
    const { role } = useAuth();
    const { startInstall, locked: installLocked, catalogSnapshot } = useComponentOperation();
    const [svc, setSvc] = useState<ManagedService | null>(null);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [checking, setChecking] = useState(false);
    const [installConfirmationOpen, setInstallConfirmationOpen] = useState(false);
    const [installReadiness, setInstallReadiness] = useState<HostMutationReadiness | null>(null);
    const [installReadinessLoading, setInstallReadinessLoading] = useState(false);
    const refreshedRef = useRef(onServiceRefreshed);

    useEffect(() => {
        refreshedRef.current = onServiceRefreshed;
    });

    // The cached observation only — this GET never probes the host, and
    // opening a component page must not probe it either. A payload this page
    // cannot decode leaves the record untouched, so an unreadable answer
    // stays "not checked yet" instead of becoming a fabricated absence.
    // Yalnız önbellekteki gözlem — bu GET makineyi yoklamaz ve bir bileşen
    // sayfasını açmak da yoklamamalıdır. Çözülemeyen yük kaydı olduğu gibi
    // bırakır; okunamayan yanıt uydurma bir yokluğa değil, "henüz bakılmadı"
    // durumunda kalır.
    const load = async () => {
        try {
            const response = await fetch('/api/v1/managed-services');
            if (!response.ok) return;
            const snapshot = decodeManagedServicesSnapshot(await response.json());
            if (!snapshot) return;
            publishComponentCensus(snapshot.services);
            setSvc(findService(snapshot.services, serviceId));
        } catch {
            // Fail closed: no answer is not an answer about this host.
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void load();
    }, [serviceId]);

    useEffect(() => {
        if (!catalogSnapshot) return;
        publishComponentCensus(catalogSnapshot.services);
        setSvc(findService(catalogSnapshot.services, serviceId));
        setLoading(false);
        refreshedRef.current?.();
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

    const observed = observedStateOf(svc);
    const installed = observed === 'present';
    const running = installed && (svc?.status?.includes('running') ?? false);

    // The operator's check, never the page's. A page visit must not probe the
    // host by itself, so nothing here runs on mount: the scan runs when it is
    // asked for, exactly as on the components list. The answer goes through
    // the same fail-closed decoder as the load, so a payload this panel cannot
    // read never becomes a state claim about this component, and a successful
    // check resolves the page to whatever it actually found — no reload.
    // Kontrolü operatör çalıştırır, sayfa değil. Sayfayı açmak makineyi
    // yoklamamalıdır; burada mount anında hiçbir şey koşmaz. Yanıt yüklemeyle
    // aynı fail-closed çözücüden geçer ve başarılı kontrol, sayfayı yeniden
    // yüklemeden gerçekte bulunan duruma çözer.
    const runCheck = async () => {
        if (checking) return;
        setChecking(true);
        try {
            const response = await fetch('/api/v1/managed-services/scan', {
                method: 'POST',
                cache: 'no-store',
            });
            if (!response.ok) {
                showToast('error', apiErrorText(await readApiError(response), t, 'services.scanFailed'));
                return;
            }
            const snapshot = decodeManagedServicesSnapshot(await response.json());
            if (!snapshot) {
                showToast('error', t('services.scanFailed'));
                return;
            }
            // This page's check is a host-wide answer, not a per-component
            // one. The sidebar badge is entitled to the same payload, so it
            // moves with the check instead of waiting for a page load.
            // Bu sayfanın kontrolü bileşene değil sisteme ait bir cevaptır;
            // kenar çubuğu rozeti de aynı yükü hak eder.
            publishComponentCensus(snapshot.services);
            setSvc(findService(snapshot.services, serviceId));
            refreshedRef.current?.();
        } catch {
            showToast('error', t('services.scanFailed'));
        } finally {
            setChecking(false);
        }
    };

    // One-click install of an absent service (admin only). The panel ships
    // with nothing installed; the agent apt-installs the whitelisted packages
    // for this host and starts the unit. Honest failures (non-root, distro
    // unsupported) surface as the real error.
    // Eksik bir servisi tek tıkla kur (yalnız admin). Panel hiçbir şey kurulu
    // gelmez; agent bu makine için whitelist'teki paketleri kurar ve unit'i
    // başlatır. Dürüst hatalar (root değil, dağıtım desteklenmiyor) gerçek
    // hata olarak görünür.
    // Both halves are gated on an actual observation of absence. The unknown
    // state renders no install button at all, and this is the second lock:
    // installing over a component nobody has looked at is the failure this
    // page exists to stop.
    // Her iki yarı da gerçek bir "yok" gözlemine bağlıdır. Bilinmeyen durumda
    // düğme zaten çizilmez; bu ikinci kilittir.
    const requestInstall = () => {
        if (observed !== 'absent') return;
        setInstallReadiness(null);
        setInstallConfirmationOpen(true);
    };

    const install = async () => {
        if (observed !== 'absent') return;
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
                        ) : observed === 'unknown' ? (
                            /* "Not checked yet" is information the operator
                               acts on, so it takes fg-muted; fg-subtle is for
                               placeholder and disabled text only. No dot: a
                               status dot beside it would assert a state.
                               "Henüz bakılmadı" operatörün eyleme dökeceği bir
                               bilgidir; fg-muted alır. Nokta yok: yanındaki
                               durum noktası bir durum iddia ederdi. */
                            <span className="text-fg-muted">{t('services.notChecked')}</span>
                        ) : observed === 'absent' ? (
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
            ) : observed === 'unknown' ? (
                /* The honest first state of a component on a host this panel
                   has never looked at: it says so, and offers the check —
                   never an install, which would offer to put down something
                   that may already be running. Neutral, exactly as on the
                   components list: a server nobody has checked yet is a
                   normal beginning, not a warning.
                   Panelin hiç bakmadığı bir makinedeki bileşenin dürüst ilk
                   durumu: bunu söyler ve kontrolü sunar — zaten çalışıyor
                   olabilecek bir şeyi kurmayı öneren düğmeyi değil. Nötr,
                   tıpkı bileşenler listesindeki gibi. */
                <div role="status">
                    <EmptyState
                        icon={ScanSearch}
                        title={t('services.notCheckedTitle')}
                        hint={t('svc.notCheckedHint', { name })}
                        action={
                            <Button
                                variant="primary"
                                icon={ScanSearch}
                                disabled={checking}
                                onClick={() => void runCheck()}
                            >
                                {checking ? t('services.scanning') : t('services.scanNow')}
                            </Button>
                        }
                    />
                </div>
            ) : observed === 'absent' ? (
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
