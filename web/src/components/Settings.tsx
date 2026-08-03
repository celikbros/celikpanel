import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ShieldCheck, ShieldOff, Copy, Check, Lock, BadgeCheck, AlertTriangle, Network } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { PageHeader, Button, inputClass } from './ui';
import { readApiError } from '../lib/apiError';
import { DNSServerSettings } from './DNSServerSettings';

type SettingsSectionID = 'account' | 'panel' | 'dns';
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

    useEffect(() => {
        fetch('/api/v1/panel/certificate')
            .then((r) => (r.ok ? r.json() : null))
            .then(setInfo)
            .catch(() => setInfo(null));
    }, []);

    const issue = async () => {
        setBusy(true);
        try {
            const res = await fetch('/api/v1/panel/certificate', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ domain }),
            });
            const data = await res.json();
            if (!res.ok || data.error) throw new Error(data.error || t('panelCert.failed'));
            showToast('success', t('panelCert.issued'));
            setRestarting(true);
            // The panel restarts to serve the new certificate; move the
            // browser to the domain the certificate is actually valid for.
            // Panel yeni sertifikayı sunmak için yeniden başlar; tarayıcıyı
            // sertifikanın gerçekten geçerli olduğu alan adına taşı.
            setTimeout(() => {
                window.location.href = `https://${domain}:${window.location.port || '2083'}/`;
            }, 6000);
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('panelCert.failed'));
            setBusy(false);
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
                        <Button className="w-full sm:w-auto" variant="primary" onClick={issue} disabled={busy || !domain}>
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
