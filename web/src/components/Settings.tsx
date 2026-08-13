import { useEffect, useRef, useState } from 'react';
import { useSearchParams } from '../router';
import { ShieldCheck, ShieldOff, Copy, Check, Lock, BadgeCheck, AlertTriangle, Network, ScanSearch } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { PageHeader, Button, inputClass } from './ui';
import { apiErrorText, readApiError } from '../lib/apiError';
import { DNSServerSettings } from './DNSServerSettings';
import { PanelUpdateCard } from './PanelUpdateCard';
import { SecurityAuditCard } from './SecurityAuditCard';

type SettingsSectionID = 'account' | 'panel' | 'security' | 'dns';
type SettingsSection = {
    id: SettingsSectionID;
    icon: React.ComponentType<{ className?: string }>;
    title: string;
    description: string;
};

// Account settings. Today it hosts two-factor authentication; admins also
// manage the panel's own certificate here.
// Hesap ayarları. Bugün iki faktörlü doğrulamayı barındırır; yöneticiler
// panelin kendi sertifikasını da buradan yönetir.
export function Settings() {
    const { t } = useI18n();
    const { role } = useAuth();
    const [searchParams, setSearchParams] = useSearchParams();
    const sections = [
        {
            id: 'account' as const,
            icon: ShieldCheck,
            title: t('settings.section.account'),
            description: t('settings.section.account.desc'),
        },
        ...(role === 'admin'
            ? [
                {
                    id: 'panel' as const,
                    icon: Lock,
                    title: t('settings.section.panel'),
                    description: t('settings.section.panel.desc'),
                },
                {
                    id: 'security' as const,
                    icon: ScanSearch,
                    title: t('settings.section.security'),
                    description: t('settings.section.security.desc'),
                },
                {
                    id: 'dns' as const,
                    icon: Network,
                    title: t('settings.section.dns'),
                    description: t('settings.section.dns.desc'),
                },
            ]
            : []),
    ];
    const requestedSection = searchParams.get('section');
    const activeSection = sections.find((section) => section.id === requestedSection) ?? sections[0];

    useEffect(() => {
        if (requestedSection === activeSection.id) return;
        const next = new URLSearchParams(searchParams);
        next.set('section', activeSection.id);
        setSearchParams(next, { replace: true });
    }, [activeSection.id, requestedSection, searchParams, setSearchParams]);

    const selectSection = (section: SettingsSectionID) => {
        const next = new URLSearchParams(searchParams);
        next.set('section', section);
        setSearchParams(next, { replace: true });
    };

    const moveSection = (event: React.KeyboardEvent<HTMLButtonElement>, currentIndex: number) => {
        const keyOffsets: Record<string, number> = {
            ArrowRight: 1,
            ArrowDown: 1,
            ArrowLeft: -1,
            ArrowUp: -1,
        };
        let nextIndex = currentIndex;
        if (event.key === 'Home') nextIndex = 0;
        else if (event.key === 'End') nextIndex = sections.length - 1;
        else if (event.key in keyOffsets) {
            nextIndex = (currentIndex + keyOffsets[event.key] + sections.length) % sections.length;
        } else {
            return;
        }
        event.preventDefault();
        const nextSection = sections[nextIndex];
        selectSection(nextSection.id);
        window.requestAnimationFrame(() => {
            document.getElementById(`settings-${nextSection.id}-tab`)?.focus();
        });
    };

    return (
        <div className="p-4 sm:p-6 md:p-8">
            <PageHeader title={t('nav.settings')} subtitle={t('settings.subtitle')} breadcrumb={[t('common.home'), t('nav.settings')]} />
            <SettingsWorkspace
                sections={sections}
                activeID={activeSection.id}
                role={role}
                label={t('settings.sections')}
                onSelect={selectSection}
                onKeyDown={moveSection}
            />
        </div>
    );
}

function SettingsWorkspace({
    sections,
    activeID,
    role,
    label,
    onSelect,
    onKeyDown,
}: {
    sections: SettingsSection[];
    activeID: SettingsSectionID;
    role: string;
    label: string;
    onSelect: (section: SettingsSectionID) => void;
    onKeyDown: (event: React.KeyboardEvent<HTMLButtonElement>, index: number) => void;
}) {
    const activeSection = sections.find((section) => section.id === activeID) ?? sections[0];

    return (
        <div className="grid max-w-7xl gap-5 lg:grid-cols-[17rem_minmax(0,1fr)] lg:items-start">
            <SettingsSectionTabs
                sections={sections}
                activeID={activeID}
                label={label}
                onSelect={onSelect}
                onKeyDown={onKeyDown}
            />
            <div className="min-w-0">
                <div className="mb-4 flex items-start gap-3 rounded-xl border border-border bg-surface px-5 py-4 shadow-card">
                    <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <activeSection.icon className="h-5 w-5" />
                    </span>
                    <div className="min-w-0">
                        <h2 className="font-semibold text-fg">{activeSection.title}</h2>
                        <p className="text-sm text-fg-muted">{activeSection.description}</p>
                    </div>
                </div>
                <div id="settings-account-panel" role="tabpanel" aria-labelledby="settings-account-tab" hidden={activeID !== 'account'}>
                    <TwoFactorPanel />
                </div>
                {role === 'admin' && (
                    <>
                        <div id="settings-panel-panel" role="tabpanel" aria-labelledby="settings-panel-tab" hidden={activeID !== 'panel'}>
                            <PanelCertificatePanel />
                            <PanelUpdateCard />
                        </div>
                        <div id="settings-security-panel" role="tabpanel" aria-labelledby="settings-security-tab" hidden={activeID !== 'security'}>
                            {activeID === 'security' && <SecurityAuditCard />}
                        </div>
                        <div id="settings-dns-panel" role="tabpanel" aria-labelledby="settings-dns-tab" hidden={activeID !== 'dns'}>
                            <DNSServerSettings />
                        </div>
                    </>
                )}
            </div>
        </div>
    );
}

function SettingsSectionTabs({
    sections,
    activeID,
    label,
    onSelect,
    onKeyDown,
}: {
    sections: SettingsSection[];
    activeID: SettingsSectionID;
    label: string;
    onSelect: (section: SettingsSectionID) => void;
    onKeyDown: (event: React.KeyboardEvent<HTMLButtonElement>, index: number) => void;
}) {
    return (
        <nav
            aria-label={label}
            className="flex gap-2 overflow-x-auto rounded-xl border border-border bg-surface p-2 shadow-card lg:sticky lg:top-6 lg:flex-col lg:overflow-visible"
            role="tablist"
        >
            {sections.map((section, index) => {
                const Icon = section.icon;
                const active = section.id === activeID;
                return (
                    <button
                        key={section.id}
                        id={`settings-${section.id}-tab`}
                        type="button"
                        role="tab"
                        aria-controls={`settings-${section.id}-panel`}
                        aria-selected={active}
                        tabIndex={active ? 0 : -1}
                        onClick={() => onSelect(section.id)}
                        onKeyDown={(event) => onKeyDown(event, index)}
                        className={`group flex min-h-11 shrink-0 items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 lg:w-full ${active
                            ? 'bg-primary/10 text-primary'
                            : 'text-fg-muted hover:bg-surface-subtle hover:text-fg'
                            }`}
                    >
                        <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${active ? 'bg-primary text-white' : 'bg-surface-subtle text-fg-muted group-hover:text-fg'}`}>
                            <Icon className="h-4 w-4" />
                        </span>
                        <span className="min-w-0">
                            <span className="block whitespace-nowrap text-sm font-semibold lg:whitespace-normal">{section.title}</span>
                            <span className="mt-0.5 hidden text-xs font-normal leading-5 text-fg-muted lg:block">{section.description}</span>
                        </span>
                    </button>
                );
            })}
        </nav>
    );
}

// The certificate the panel itself serves on its HTTPS port. Out of the box
// it is self-signed (every browser warns); one click issues a Let's Encrypt
// certificate for the panel's domain and restarts the panel to serve it.
// Renewal is automatic afterwards (certbot timer + deploy hook).
// Panelin HTTPS portunda bizzat sunduğu sertifika. Kutudan çıkanı kendinden
// imzalıdır (her tarayıcı uyarır); tek tık, panelin alan adı için Let's
// Encrypt sertifikası alır ve sunması için paneli yeniden başlatır. Sonrası
// otomatik yenilenir (certbot zamanlayıcısı + deploy kancası).
const PANEL_CERTIFICATE_OPERATION_KEY = 'celikpanel.panel-certificate-operation.v1';
const PANEL_CERTIFICATE_POLL_MS = 1500;
const PANEL_CERTIFICATE_MISSING_GRACE_MS = 10 * 60 * 1000;

type PanelCertificateOperationMarker = {
    version: 1;
    request_id: string;
    domain: string;
    created_at: number;
};

type PanelCertificateOperation = {
    id: string;
    request_id: string;
    kind: 'panel_certificate_issue';
    service_id: string;
    status: 'queued' | 'running' | 'succeeded' | 'failed';
    error?: { code: string; message: string };
};

function createPanelCertificateRequestID(): string | null {
    try {
        const bytes = new Uint8Array(16);
        crypto.getRandomValues(bytes);
        return Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('');
    } catch {
        return null;
    }
}

function canonicalPanelCertificateMarkerDomain(raw: string): string {
    const trimmed = raw.trim().toLowerCase();
    return trimmed.endsWith('.') ? trimmed.slice(0, -1) : trimmed;
}

function decodePanelCertificateMarker(raw: string | null): PanelCertificateOperationMarker | null {
    if (!raw || raw.length > 1024) return null;
    try {
        const value = JSON.parse(raw) as Record<string, unknown>;
        if (
            value.version !== 1
            || typeof value.request_id !== 'string'
            || !/^[a-f0-9]{32}$/.test(value.request_id)
            || typeof value.domain !== 'string'
            || value.domain !== canonicalPanelCertificateMarkerDomain(value.domain)
            || !value.domain
            || value.domain.length > 253
            || typeof value.created_at !== 'number'
            || !Number.isFinite(value.created_at)
            || value.created_at <= 0
        ) return null;
        return {
            version: 1,
            request_id: value.request_id,
            domain: value.domain,
            created_at: value.created_at,
        };
    } catch {
        return null;
    }
}

function readPanelCertificateMarker(): PanelCertificateOperationMarker | null {
    try {
        const marker = decodePanelCertificateMarker(
            localStorage.getItem(PANEL_CERTIFICATE_OPERATION_KEY),
        );
        if (marker === null) localStorage.removeItem(PANEL_CERTIFICATE_OPERATION_KEY);
        return marker;
    } catch {
        return null;
    }
}

function storePanelCertificateMarker(marker: PanelCertificateOperationMarker): boolean {
    try {
        localStorage.setItem(PANEL_CERTIFICATE_OPERATION_KEY, JSON.stringify(marker));
        return true;
    } catch {
        return false;
    }
}

function clearPanelCertificateMarker() {
    try {
        localStorage.removeItem(PANEL_CERTIFICATE_OPERATION_KEY);
    } catch {
        // The in-memory marker still gives this tab an authoritative poll key.
    }
}

function decodePanelCertificateOperation(
    payload: unknown,
    marker: PanelCertificateOperationMarker,
): PanelCertificateOperation | null {
    if (!payload || typeof payload !== 'object') return null;
    const operation = (payload as { operation?: unknown }).operation;
    if (!operation || typeof operation !== 'object') return null;
    const value = operation as Record<string, unknown>;
    if (
        typeof value.id !== 'string'
        || !/^[a-f0-9]{32}$/.test(value.id)
        || value.request_id !== marker.request_id
        || value.kind !== 'panel_certificate_issue'
        || value.service_id !== marker.domain
        || (
            value.status !== 'queued'
            && value.status !== 'running'
            && value.status !== 'succeeded'
            && value.status !== 'failed'
        )
    ) return null;
    let operationError: PanelCertificateOperation['error'];
    if (value.error !== undefined) {
        if (!value.error || typeof value.error !== 'object') return null;
        const errorValue = value.error as Record<string, unknown>;
        if (typeof errorValue.code !== 'string' || typeof errorValue.message !== 'string') return null;
        operationError = { code: errorValue.code, message: errorValue.message };
    }
    return {
        id: value.id,
        request_id: marker.request_id,
        kind: 'panel_certificate_issue',
        service_id: marker.domain,
        status: value.status,
        ...(operationError ? { error: operationError } : {}),
    };
}

function PanelCertificatePanel() {
    const { t } = useI18n();
    const [info, setInfo] = useState<{
        https_enabled: boolean;
        self_signed: boolean;
        subject?: string;
        issuer?: string;
        expires_at?: string;
    } | null>(null);
    const [domain, setDomain] = useState(() =>
        /^[0-9.]+$/.test(window.location.hostname) ? '' : window.location.hostname,
    );
    const [busy, setBusy] = useState(false);
    const [restarting, setRestarting] = useState(false);
    const [pendingOperation, setPendingOperation] = useState<PanelCertificateOperationMarker | null>(
        () => readPanelCertificateMarker(),
    );
    const issueInFlightRef = useRef(pendingOperation !== null);

    useEffect(() => {
        fetch('/api/v1/panel/certificate')
            .then((r) => (r.ok ? r.json() : null))
            .then(setInfo)
            .catch(() => setInfo(null));
    }, []);

    useEffect(() => {
        const marker = pendingOperation;
        if (marker === null || restarting) return undefined;
        const exactMarker: PanelCertificateOperationMarker = marker;
        let cancelled = false;
        let timer: ReturnType<typeof setTimeout> | undefined;
        setBusy(true);

        function schedule() {
            if (!cancelled) timer = setTimeout(() => void poll(), PANEL_CERTIFICATE_POLL_MS);
        }
        async function poll() {
            let response: Response;
            try {
                response = await fetch(
                    `/api/v1/service/operation?request_id=${encodeURIComponent(exactMarker.request_id)}`,
                    { cache: 'no-store' },
                );
            } catch {
                schedule();
                return;
            }
            if (cancelled) return;
            if (response.status === 404) {
                if (Date.now() - exactMarker.created_at > PANEL_CERTIFICATE_MISSING_GRACE_MS) {
                    clearPanelCertificateMarker();
                    issueInFlightRef.current = false;
                    setPendingOperation(null);
                    setBusy(false);
                    showToast('error', t('panelCert.failed'));
                    return;
                }
                schedule();
                return;
            }
            if (!response.ok) {
                schedule();
                return;
            }
            let payload: unknown;
            try {
                payload = await response.json();
            } catch {
                schedule();
                return;
            }
            const operation = decodePanelCertificateOperation(payload, exactMarker);
            if (operation === null) {
                // A mismatched privileged operation can never authorize
                // clearing or replacing this exact request-id marker.
                schedule();
                return;
            }
            if (operation.status === 'failed') {
                clearPanelCertificateMarker();
                issueInFlightRef.current = false;
                setPendingOperation(null);
                setBusy(false);
                showToast('error', operation.error?.message || t('panelCert.failed'));
                return;
            }
            if (operation.status !== 'succeeded') {
                schedule();
                return;
            }
            clearPanelCertificateMarker();
            issueInFlightRef.current = false;
            setPendingOperation(null);
            showToast('success', t('panelCert.issued'));
            setRestarting(true);
            setTimeout(() => {
                window.location.href = `https://${exactMarker.domain}:${window.location.port || '2083'}/`;
            }, 6000);
        }
        void poll();
        return () => {
            cancelled = true;
            if (timer !== undefined) clearTimeout(timer);
        };
    }, [pendingOperation, restarting, t]);

    const issue = async () => {
        // The ref closes the pre-render double-click window. A marker loaded
        // during the initial render is authoritative and must never be
        // overwritten by a new request id while exact polling is in flight.
        if (issueInFlightRef.current || pendingOperation !== null || restarting || !domain) return;
        issueInFlightRef.current = true;
        const requestID = createPanelCertificateRequestID();
        const marker: PanelCertificateOperationMarker | null = requestID === null
            ? null
            : {
                version: 1,
                request_id: requestID,
                domain: canonicalPanelCertificateMarkerDomain(domain),
                created_at: Date.now(),
            };
        if (marker === null || !marker.domain || !storePanelCertificateMarker(marker)) {
            issueInFlightRef.current = false;
            showToast('error', t('panelCert.failed'));
            return;
        }
        setBusy(true);
        setPendingOperation(marker);
        try {
            const res = await fetch('/api/v1/panel/certificate', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ domain, request_id: marker.request_id }),
            });
            if (!res.ok) {
                // 408/429/5xx and an auth-gate response do not prove the
                // durable operation was rejected: a proxy can lose or replace
                // the response after the row is committed. Keep the exact
                // marker and reconcile through the operation endpoint.
                const rejectionIsDefinitive = res.status >= 400
                    && res.status < 500
                    && res.status !== 401
                    && res.status !== 408
                    && res.status !== 429;
                if (rejectionIsDefinitive) {
                    clearPanelCertificateMarker();
                    issueInFlightRef.current = false;
                    setPendingOperation(null);
                    showToast('error', apiErrorText(await readApiError(res), t, 'panelCert.failed'));
                    setBusy(false);
                }
                return;
            }
            const operation = decodePanelCertificateOperation(await res.json(), marker);
            if (operation === null) throw new Error(t('panelCert.failed'));
        } catch (e) {
            // A lost POST response is not proof that the durable row was not
            // created. Keep the exact marker and let polling reconcile it.
            if (!(e instanceof TypeError)) {
                showToast('error', e instanceof Error && e.message ? e.message : t('panelCert.failed'));
            }
        }
    };

    return (
        <section className="rounded-xl border border-border bg-surface p-6">
            <div className="mb-3 flex items-center gap-2">
                <Lock className="h-5 w-5 text-fg-subtle" />
                <h2 className="text-base font-semibold text-fg">{t('panelCert.title')}</h2>
            </div>

            {info && (
                <div className="mb-4 flex items-start gap-2 rounded-lg border border-border bg-surface-2/50 p-3 text-sm">
                    {info.self_signed ? (
                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                    ) : (
                        <BadgeCheck className="mt-0.5 h-4 w-4 shrink-0 text-success" />
                    )}
                    <div className="min-w-0 text-fg-muted">
                        {info.self_signed ? t('panelCert.selfSigned') : t('panelCert.real', { issuer: info.issuer || '' })}
                        {info.expires_at && !info.self_signed && (
                            <span className="block text-xs text-fg-subtle">
                                {t('panelCert.expires', { date: new Date(info.expires_at).toLocaleDateString() })}
                            </span>
                        )}
                    </div>
                </div>
            )}

            {restarting ? (
                <p className="text-sm font-medium text-fg">{t('panelCert.restarting')}</p>
            ) : (
                <>
                    <label htmlFor="panel-certificate-domain" className="mb-1 block text-xs font-medium text-fg-muted">
                        {t('panelCert.domain')}
                    </label>
                    <div className="flex flex-col gap-2 sm:flex-row">
                        <input
                            id="panel-certificate-domain"
                            value={domain}
                            onChange={(e) => setDomain(e.target.value.trim())}
                            placeholder="panel.example.com"
                            autoComplete="url"
                            className={inputClass + ' min-w-0 flex-1'}
                        />
                        <Button
                            className="w-full sm:w-auto"
                            variant="primary"
                            onClick={issue}
                            disabled={busy || pendingOperation !== null || restarting || !domain}
                        >
                            {busy ? t('panelCert.issuing') : t('panelCert.issue')}
                        </Button>
                    </div>
                    <p className="mt-2 text-xs text-fg-subtle">{t('panelCert.hint')}</p>
                </>
            )}
        </section>
    );
}

function TwoFactorPanel() {
    const { t } = useI18n();
    const [enabled, setEnabled] = useState<boolean | null>(null);
    const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null);
    const [qr, setQr] = useState<string>('');

    // The otpauth:// URI as a QR code — scanning beats typing a 32-char
    // secret. Generated locally in the browser; the secret never leaves.
    // otpauth:// URI'si QR olarak — taramak 32 karakterlik anahtarı yazmayı
    // döver. Tarayıcıda yerel üretilir; anahtar dışarı çıkmaz.
    useEffect(() => {
        let cancelled = false;
        setQr('');
        if (setup?.uri) {
            void import('qrcode')
                .then(({ default: QRCode }) => QRCode.toDataURL(setup.uri, { width: 192, margin: 1 }))
                .then((url) => {
                    if (!cancelled) setQr(url);
                })
                .catch(() => {
                    if (!cancelled) setQr('');
                });
        }

        return () => {
            cancelled = true;
        };
    }, [setup]);
    const [code, setCode] = useState('');
    const [setupPw, setSetupPw] = useState('');
    const [disablePw, setDisablePw] = useState('');
    const [disableCode, setDisableCode] = useState('');
    const [busy, setBusy] = useState(false);
    const [copied, setCopied] = useState(false);

    const loadStatus = () =>
        fetch('/api/v1/auth/2fa/status')
            .then((r) => (r.ok ? r.json() : { enabled: false }))
            .then((d) => setEnabled(!!d.enabled))
            .catch(() => setEnabled(false));

    useEffect(() => {
        loadStatus();
    }, []);

    const startSetup = async () => {
        setBusy(true);
        try {
            const r = await fetch('/api/v1/auth/2fa/setup', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ password: setupPw }),
            });
            if (!r.ok) throw new Error((await readApiError(r)).message);
            setSetup(await r.json());
        } catch (e) {
            showToast('error', (e as Error).message || t('settings.2fa.reauthFailed'));
        } finally {
            setBusy(false);
        }
    };

    const enable = async () => {
        setBusy(true);
        try {
            const r = await fetch('/api/v1/auth/2fa/enable', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ password: setupPw, code: code.trim() }),
            });
            if (!r.ok) throw new Error((await readApiError(r)).message);
            showToast('success', t('settings.2fa.enabled'));
            setSetup(null);
            setSetupPw('');
            setCode('');
            setEnabled(true);
        } catch (e) {
            showToast('error', (e as Error).message || t('settings.2fa.badCode'));
        } finally {
            setBusy(false);
        }
    };

    const disable = async () => {
        setBusy(true);
        try {
            const r = await fetch('/api/v1/auth/2fa/disable', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ password: disablePw, code: disableCode.trim() }),
            });
            if (!r.ok) throw new Error();
            showToast('success', t('settings.2fa.disabled'));
            setDisablePw('');
            setDisableCode('');
            setEnabled(false);
        } catch {
            showToast('error', t('settings.2fa.disableFailed'));
        } finally {
            setBusy(false);
        }
    };

    const copySecret = () => {
        if (!setup) return;
        navigator.clipboard?.writeText(setup.secret).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
        });
    };

    return (
        <section className="rounded-xl border border-border bg-surface p-5 shadow-card">
            <div className="mb-1 flex items-center gap-2">
                {enabled ? <ShieldCheck className="h-5 w-5 text-success" /> : <ShieldOff className="h-5 w-5 text-fg-subtle" />}
                <h2 className="text-base font-semibold text-fg">{t('settings.2fa.title')}</h2>
                {enabled && (
                    <span className="ml-auto rounded-md bg-success/10 px-2 py-0.5 text-xs font-medium text-success">
                        {t('settings.2fa.on')}
                    </span>
                )}
            </div>
            <p className="mb-4 text-sm text-fg-muted">{t('settings.2fa.desc')}</p>

            {enabled === null ? (
                <div className="py-2 text-sm text-fg-subtle">{t('common.loading')}</div>
            ) : enabled ? (
                <div className="space-y-3">
                    <p className="text-sm text-fg-muted">{t('settings.2fa.disableHint')}</p>
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                        <label className="space-y-1 text-sm font-medium text-fg">
                            <span>{t('login.password')}</span>
                            <input
                                type="password"
                                value={disablePw}
                                onChange={(e) => setDisablePw(e.target.value)}
                                autoComplete="current-password"
                                className={inputClass}
                            />
                        </label>
                        <label className="space-y-1 text-sm font-medium text-fg">
                            <span>{t('settings.2fa.enterCode')}</span>
                            <input
                                value={disableCode}
                                onChange={(e) => setDisableCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                                inputMode="numeric"
                                autoComplete="one-time-code"
                                placeholder="000000"
                                className={`${inputClass} font-mono tracking-widest`}
                            />
                        </label>
                    </div>
                    <Button variant="danger" icon={ShieldOff} disabled={busy || !disablePw || disableCode.length < 6} onClick={disable}>
                        {t('settings.2fa.disable')}
                    </Button>
                </div>
            ) : setup ? (
                <div className="space-y-4">
                    <ol className="list-inside list-decimal space-y-1 text-sm text-fg-muted">
                        <li>{t('settings.2fa.step1')}</li>
                        <li>{t('settings.2fa.step2')}</li>
                    </ol>
                    {qr && (
                        <div className="flex justify-center">
                            <img src={qr} alt="TOTP QR" className="rounded-lg border border-border bg-white p-1" width={192} height={192} />
                        </div>
                    )}
                    <div className="flex items-center gap-2 rounded-lg border border-border bg-surface-2/50 p-3">
                        <code className="min-w-0 flex-1 break-all font-mono text-sm text-fg">{setup.secret}</code>
                        <button onClick={copySecret} title={t('common.copy')} className="rounded-md p-1.5 text-fg-subtle hover:bg-surface-2 hover:text-fg">
                            {copied ? <Check className="h-4 w-4 text-success" /> : <Copy className="h-4 w-4" />}
                        </button>
                    </div>
                    <a href={setup.uri} className="text-sm text-primary hover:underline">{t('settings.2fa.openApp')}</a>
                    <div>
                        <label className="mb-1.5 block text-sm font-medium text-fg-muted">{t('settings.2fa.enterCode')}</label>
                        <input value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" className={`${inputClass} max-w-[12rem] font-mono text-lg tracking-[0.3em]`} />
                    </div>
                    <div className="flex gap-2">
                        <Button variant="primary" icon={ShieldCheck} disabled={busy || !setupPw || code.length < 6} onClick={enable}>
                            {t('settings.2fa.verify')}
                        </Button>
                        <Button variant="secondary" onClick={() => { setSetup(null); setSetupPw(''); setCode(''); }}>
                            {t('common.back')}
                        </Button>
                    </div>
                </div>
            ) : (
                <div className="space-y-3">
                    <p className="text-sm text-fg-muted">{t('settings.2fa.reauthHint')}</p>
                    <label className="block max-w-md space-y-1 text-sm font-medium text-fg">
                        <span>{t('login.password')}</span>
                        <input
                            type="password"
                            value={setupPw}
                            onChange={(e) => setSetupPw(e.target.value)}
                            autoComplete="current-password"
                            className={inputClass}
                        />
                    </label>
                    <Button variant="primary" icon={ShieldCheck} disabled={busy || !setupPw} onClick={startSetup}>
                        {t('settings.2fa.setup')}
                    </Button>
                </div>
            )}
        </section>
    );
}
