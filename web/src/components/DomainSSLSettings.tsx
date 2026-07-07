import { useState, useEffect } from 'react';
import { Shield, CheckCircle, AlertTriangle, XCircle, Lock, Upload, Trash2 } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { FormSection, Field, FormActions, Button, inputClass } from './ui';
import type { TranslationKey } from '../i18n/en';

interface DomainSSLSettingsProps {
    domainId: number;
    domainName: string;
}

interface SSLCertificate {
    id: number;
    type: string;
    issuer: string;
    subject: string;
    issued_at: string;
    expires_at: string;
    days_until_expiry: number;
    auto_renew: boolean;
    renewal_status: string;
    status: string;
}

interface SSLSettings {
    force_https: boolean;
    hsts_enabled: boolean;
    hsts_max_age: number;
}

interface SSLData {
    domain_id: number;
    domain_name: string;
    has_certificate: boolean;
    certificate?: SSLCertificate;
    settings: SSLSettings;
}

export function DomainSSLSettings({ domainId, domainName }: DomainSSLSettingsProps) {
    const { t } = useI18n();
    const [data, setData] = useState<SSLData | null>(null);
    const [loading, setLoading] = useState(true);
    const [issuing, setIssuing] = useState(false);
    const [email, setEmail] = useState('');
    const [autoRenew, setAutoRenew] = useState(true);
    const [certSource, setCertSource] = useState<'letsencrypt' | 'custom'>('letsencrypt');
    const [certFile, setCertFile] = useState<File | null>(null);
    const [keyFile, setKeyFile] = useState<File | null>(null);
    const [chainFile, setChainFile] = useState<File | null>(null);
    const [uploading, setUploading] = useState(false);
    const [secureMail, setSecureMail] = useState(false);

    useEffect(() => {
        loadSSLData();
    }, [domainId]);

    const loadSSLData = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl`);
            if (!res.ok) throw new Error();
            setData(await res.json());
            const mailRes = await fetch(`/api/v1/domains/${domainId}/ssl/mail`);
            if (mailRes.ok) setSecureMail((await mailRes.json()).secure_mail === true);
        } catch {
            showToast('error', t('ssl.loadFailed'));
        } finally {
            setLoading(false);
        }
    };

    const handleIssue = async () => {
        if (!email) return showToast('error', t('ssl.emailRequired'));
        if (!confirm(t('ssl.issueConfirm', { name: domainName }))) return;
        setIssuing(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl/letsencrypt`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ email, auto_renew: autoRenew }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('ssl.issued'));
            loadSSLData();
        } catch {
            showToast('error', t('ssl.issueFailed'));
        } finally {
            setIssuing(false);
        }
    };

    const handleUpload = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!certFile || !keyFile) return showToast('error', t('ssl.certKeyRequired'));
        if (!confirm(t('ssl.uploadConfirm', { name: domainName }))) return;
        setUploading(true);
        try {
            const fd = new FormData();
            fd.append('certificate', certFile);
            fd.append('private_key', keyFile);
            if (chainFile) fd.append('chain', chainFile);
            const res = await fetch(`/api/v1/domains/${domainId}/ssl/upload`, { method: 'POST', body: fd });
            if (!res.ok) throw new Error();
            showToast('success', t('ssl.uploaded'));
            setCertFile(null);
            setKeyFile(null);
            setChainFile(null);
            loadSSLData();
        } catch {
            showToast('error', t('ssl.uploadFailed'));
        } finally {
            setUploading(false);
        }
    };

    const handleUpdateSettings = async (updates: Partial<SSLSettings>) => {
        if (!data) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl/settings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ...data.settings, ...updates }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('ssl.settingsSaved'));
            loadSSLData();
        } catch {
            showToast('error', t('ssl.settingsFailed'));
        }
    };

    const handleDelete = async () => {
        if (!confirm(t('ssl.confirmRemove', { name: domainName }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('ssl.removed'));
            loadSSLData();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center py-16">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }
    if (!data) return <p className="text-danger">{t('ssl.loadFailed')}</p>;

    const cert = data.certificate;
    // Status tier drives icon + color, echoing Plesk's date pills but tokened.
    // Durum kademesi ikon+rengi belirler; Plesk'in tarih rozetlerini token'lı yansıtır.
    const tier = !cert
        ? { icon: XCircle, color: 'text-fg-subtle', label: 'ssl.status.none' as TranslationKey }
        : cert.days_until_expiry < 0
          ? { icon: XCircle, color: 'text-danger', label: 'ssl.status.expired' as TranslationKey }
          : cert.days_until_expiry < 30
            ? { icon: AlertTriangle, color: 'text-warning', label: 'ssl.status.expiring' as TranslationKey }
            : { icon: CheckCircle, color: 'text-success', label: 'ssl.status.valid' as TranslationKey };
    const TierIcon = tier.icon;

    return (
        <div>
            {/* Certificate status */}
            <FormSection title="SSL/TLS">
                <div className="flex items-start gap-3">
                    <TierIcon className={`mt-0.5 h-6 w-6 shrink-0 ${tier.color}`} />
                    <div className="min-w-0 flex-1">
                        <p className="font-semibold text-fg">{t(tier.label)}</p>
                        {data.has_certificate && cert ? (
                            <>
                                <dl className="mt-2 grid grid-cols-1 gap-x-6 gap-y-1.5 text-sm sm:grid-cols-2">
                                    <Detail label={t('ssl.type')} value={cert.type === 'letsencrypt' ? "Let's Encrypt" : 'Custom'} />
                                    <Detail label={t('ssl.issuer')} value={cert.issuer} />
                                    <Detail
                                        label={t('ssl.expires')}
                                        value={
                                            <span>
                                                {new Date(cert.expires_at).toLocaleDateString()}{' '}
                                                <span className={cert.days_until_expiry < 30 ? 'text-warning' : 'text-fg-subtle'}>
                                                    ({t('ssl.days', { n: cert.days_until_expiry })})
                                                </span>
                                            </span>
                                        }
                                    />
                                    {cert.type === 'letsencrypt' && (
                                        <Detail
                                            label={t('ssl.autoRenew')}
                                            value={
                                                <span className={cert.auto_renew ? 'text-success' : 'text-fg-subtle'}>
                                                    {cert.auto_renew ? t('domain.info.on') : t('domain.info.off')}
                                                </span>
                                            }
                                        />
                                    )}
                                    {cert.renewal_status !== '' && (
                                        <Detail
                                            label={t('ssl.renewalStatus')}
                                            value={
                                                <span className={cert.renewal_status === 'renewed' ? 'text-success' : 'text-warning'}>
                                                    {cert.renewal_status === 'renewed'
                                                        ? t('ssl.renewed')
                                                        : cert.renewal_status === 'expiring'
                                                            ? t('ssl.expiringSoon')
                                                            : cert.renewal_status}
                                                </span>
                                            }
                                        />
                                    )}
                                </dl>
                                <div className="mt-3">
                                    <Button variant="danger" icon={Trash2} onClick={handleDelete}>
                                        {t('ssl.remove')}
                                    </Button>
                                </div>
                            </>
                        ) : (
                            <p className="mt-1 text-sm text-fg-muted">{t('ssl.noCert')}</p>
                        )}
                    </div>
                </div>
            </FormSection>

            {/* Issue / upload (only when no cert) */}
            {!data.has_certificate && (
                <FormSection title={t('domain.sub.ssl')}>
                    <div className="mb-4 flex gap-1 border-b border-border">
                        <TabBtn active={certSource === 'letsencrypt'} onClick={() => setCertSource('letsencrypt')} icon={Shield} label={t('ssl.tab.letsencrypt')} />
                        <TabBtn active={certSource === 'custom'} onClick={() => setCertSource('custom')} icon={Upload} label={t('ssl.tab.custom')} />
                    </div>

                    {certSource === 'letsencrypt' ? (
                        <div className="space-y-3">
                            <p className="text-sm text-fg-muted">{t('ssl.letsencryptDesc')}</p>
                            <Field label={t('ssl.email')} hint={t('ssl.emailHint')}>
                                <input
                                    type="email"
                                    value={email}
                                    onChange={(e) => setEmail(e.target.value)}
                                    placeholder="admin@example.com"
                                    className={inputClass}
                                />
                            </Field>
                            <label className="flex cursor-pointer items-center gap-2">
                                <input type="checkbox" checked={autoRenew} onChange={(e) => setAutoRenew(e.target.checked)} className="h-4 w-4 accent-primary" />
                                <span className="text-sm text-fg">{t('ssl.autoRenewOn')}</span>
                            </label>
                            <Button variant="primary" icon={Lock} onClick={handleIssue} disabled={issuing || !email}>
                                {issuing ? t('ssl.issuing') : t('ssl.issue')}
                            </Button>
                        </div>
                    ) : (
                        <form onSubmit={handleUpload} className="space-y-3">
                            <p className="text-sm text-fg-muted">{t('ssl.customDesc')}</p>
                            <Field label={`${t('ssl.cert')} *`}>
                                <input type="file" accept=".pem,.crt,.cer" onChange={(e) => setCertFile(e.target.files?.[0] || null)} className={fileClass} />
                            </Field>
                            <Field label={`${t('ssl.key')} *`}>
                                <input type="file" accept=".pem,.key" onChange={(e) => setKeyFile(e.target.files?.[0] || null)} className={fileClass} />
                            </Field>
                            <Field label={t('ssl.chain')} hint={t('ssl.chainHint')}>
                                <input type="file" accept=".pem,.crt,.cer" onChange={(e) => setChainFile(e.target.files?.[0] || null)} className={fileClass} />
                            </Field>
                            <FormActions>
                                <Button type="submit" variant="primary" icon={Upload} disabled={uploading || !certFile || !keyFile}>
                                    {uploading ? t('ssl.uploading') : t('ssl.upload')}
                                </Button>
                            </FormActions>
                        </form>
                    )}
                </FormSection>
            )}

            {/* HTTPS settings */}
            <FormSection title={t('ssl.httpsSettings')}>
                <ControlledToggle
                    checked={data.settings.force_https}
                    onChange={(v) => handleUpdateSettings({ force_https: v })}
                    label={t('ssl.forceHttps')}
                    hint={t('ssl.forceHttpsHint')}
                />
                <ControlledToggle
                    checked={data.settings.hsts_enabled}
                    onChange={(v) => handleUpdateSettings({ hsts_enabled: v })}
                    label={t('ssl.hsts')}
                    hint={t('ssl.hstsHint')}
                />
                {data.has_certificate && (
                    <ControlledToggle
                        checked={secureMail}
                        onChange={async (v) => {
                            try {
                                const r = await fetch(`/api/v1/domains/${domainId}/ssl/mail`, {
                                    method: 'PUT',
                                    headers: { 'Content-Type': 'application/json' },
                                    body: JSON.stringify({ secure_mail: v }),
                                });
                                if (!r.ok) throw new Error();
                                setSecureMail(v);
                                showToast('success', v ? t('ssl.mailSecured') : t('ssl.mailUnsecured'));
                            } catch {
                                showToast('error', t('common.error'));
                            }
                        }}
                        label={t('ssl.secureMail')}
                        hint={t('ssl.secureMailHint')}
                    />
                )}
            </FormSection>
        </div>
    );
}

const fileClass =
    'w-full rounded-lg border border-border bg-surface px-3 py-2 text-sm text-fg file:mr-3 file:rounded file:border-0 file:bg-surface-2 file:px-3 file:py-1 file:text-fg hover:file:bg-surface-3';

function Detail({ label, value }: { label: string; value: React.ReactNode }) {
    return (
        <div className="flex gap-2">
            <dt className="text-fg-subtle">{label}:</dt>
            <dd className="font-medium text-fg">{value}</dd>
        </div>
    );
}

function TabBtn({ active, onClick, icon: Icon, label }: { active: boolean; onClick: () => void; icon: typeof Shield; label: string }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" />
            {label}
        </button>
    );
}

function ControlledToggle({ checked, onChange, label, hint }: { checked: boolean; onChange: (v: boolean) => void; label: string; hint?: string }) {
    return (
        <label className="flex cursor-pointer items-start gap-3">
            <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} className="mt-0.5 h-4 w-4 accent-primary" />
            <span>
                <span className="block text-sm text-fg">{label}</span>
                {hint && <span className="block text-xs text-fg-subtle">{hint}</span>}
            </span>
        </label>
    );
}
