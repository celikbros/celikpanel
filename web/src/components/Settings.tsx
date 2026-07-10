import { useEffect, useState } from 'react';
import QRCode from 'qrcode';
import { ShieldCheck, ShieldOff, Copy, Check, Lock, BadgeCheck, AlertTriangle } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { PageHeader, Button, inputClass } from './ui';

// Account settings. Today it hosts two-factor authentication; admins also
// manage the panel's own certificate here.
// Hesap ayarları. Bugün iki faktörlü doğrulamayı barındırır; yöneticiler
// panelin kendi sertifikasını da buradan yönetir.
export function Settings() {
    const { t } = useI18n();
    const { role } = useAuth();
    return (
        <div className="p-6 md:p-8">
            <PageHeader title={t('nav.settings')} subtitle={t('settings.subtitle')} breadcrumb={[t('common.home'), t('nav.settings')]} />
            <div className="max-w-2xl space-y-6">
                <TwoFactorPanel />
                {role === 'admin' && <PanelCertificatePanel />}
            </div>
        </div>
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
                    <label className="mb-1 block text-xs font-medium text-fg-subtle">{t('panelCert.domain')}</label>
                    <div className="flex gap-2">
                        <input
                            value={domain}
                            onChange={(e) => setDomain(e.target.value.trim())}
                            placeholder="panel.example.com"
                            className={inputClass + ' flex-1'}
                        />
                        <Button variant="primary" onClick={issue} disabled={busy || !domain}>
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
        if (setup?.uri) {
            QRCode.toDataURL(setup.uri, { width: 192, margin: 1 }).then(setQr).catch(() => setQr(''));
        } else {
            setQr('');
        }
    }, [setup]);
    const [code, setCode] = useState('');
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
            const r = await fetch('/api/v1/auth/2fa/setup', { method: 'POST' });
            if (!r.ok) throw new Error();
            setSetup(await r.json());
        } catch {
            showToast('error', t('common.error'));
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
                body: JSON.stringify({ code: code.trim() }),
            });
            if (!r.ok) throw new Error((await r.text()).trim());
            showToast('success', t('settings.2fa.enabled'));
            setSetup(null);
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
                <h3 className="text-base font-semibold text-fg">{t('settings.2fa.title')}</h3>
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
                        <input type="password" value={disablePw} onChange={(e) => setDisablePw(e.target.value)} placeholder={t('login.password')} className={inputClass} />
                        <input value={disableCode} onChange={(e) => setDisableCode(e.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" className={`${inputClass} font-mono tracking-widest`} />
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
                        <Button variant="primary" icon={ShieldCheck} disabled={busy || code.length < 6} onClick={enable}>
                            {t('settings.2fa.verify')}
                        </Button>
                        <Button variant="secondary" onClick={() => { setSetup(null); setCode(''); }}>
                            {t('common.back')}
                        </Button>
                    </div>
                </div>
            ) : (
                <Button variant="primary" icon={ShieldCheck} disabled={busy} onClick={startSetup}>
                    {t('settings.2fa.setup')}
                </Button>
            )}
        </section>
    );
}
