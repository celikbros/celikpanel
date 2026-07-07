import { useEffect, useState } from 'react';
import QRCode from 'qrcode';
import { ShieldCheck, ShieldOff, Copy, Check } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { PageHeader, Button, inputClass } from './ui';

// Account settings. Today it hosts two-factor authentication; password change
// and other personal settings can join here later.
// Hesap ayarları. Bugün iki faktörlü doğrulamayı barındırır; parola değişimi
// ve diğer kişisel ayarlar ileride buraya katılabilir.
export function Settings() {
    const { t } = useI18n();
    return (
        <div className="p-6 md:p-8">
            <PageHeader title={t('nav.settings')} subtitle={t('settings.subtitle')} breadcrumb={[t('common.home'), t('nav.settings')]} />
            <div className="max-w-2xl">
                <TwoFactorPanel />
            </div>
        </div>
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
                <h3 className="font-semibold text-fg">{t('settings.2fa.title')}</h3>
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
