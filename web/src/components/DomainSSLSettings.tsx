import { useState, useEffect, useRef } from 'react';
import { Shield, CheckCircle, AlertTriangle, XCircle, Lock, Upload, Unlink, RefreshCw } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, Checking, CouldNotCheck, Field, FormActions, FormSection, inputClass } from './ui';
import type { TranslationKey } from '../i18n/en';
import { apiErrorText, readApiError } from '../lib/apiError';
import { decodeListIn, lastKnown, useRemote } from '../lib/remote';
import { sslTier, sslTierLabel } from '../lib/sslTier';

interface DomainSSLSettingsProps {
    domainId: number;
    domainName: string;
    mailAvailable: boolean | null;
    readOnly?: boolean;
    onCertificateChange?: (status: SSLRuntimeSummary) => void;
}

export interface SSLRuntimeSummary {
    activated: boolean;
    usable: boolean;
}

interface SSLCertificate {
    id: number;
    type: string;
    provider_id?: string | null;
    issuer: string;
    subject: string;
    issued_at: string;
    expires_at: string;
    days_until_expiry: number;
    auto_renew: boolean;
    renewal_status: string;
    status: string;
    dns_names?: string[] | null;
    activated: boolean;
    usable: boolean;
    trust_status: 'trusted' | 'untrusted' | 'unknown' | 'invalid';
    trust_error?: string;
    activation_pending: boolean;
    dependents_pending: boolean;
}

interface SSLSettings {
    force_https: boolean;
    hsts_enabled: boolean;
    hsts_max_age: number;
    hsts_retire_after?: string | null;
}

interface SSLData {
    domain_id: number;
    domain_name: string;
    has_certificate: boolean;
    certificate?: SSLCertificate;
    settings: SSLSettings;
    managed_names?: string[] | null;
}

interface SSLProvider {
    id: string;
    name: string;
    note: string;
    needs_eab: boolean;
}

const HSTS_PRESETS = [
    { seconds: 300, label: 'ssl.hstsMaxAge.fiveMinutes' },
    { seconds: 86400, label: 'ssl.hstsMaxAge.oneDay' },
    { seconds: 604800, label: 'ssl.hstsMaxAge.oneWeek' },
    { seconds: 2592000, label: 'ssl.hstsMaxAge.oneMonth' },
    { seconds: 15552000, label: 'ssl.hstsMaxAge.sixMonths' },
    { seconds: 31536000, label: 'ssl.hstsMaxAge.oneYear' },
] as const satisfies ReadonlyArray<{ seconds: number; label: TranslationKey }>;

const INITIAL_HSTS_MAX_AGE = 300;

// What this screen reads. Each answer is the contract or it is unknown: a
// missing `has_certificate` is not "no certificate", a missing `secure_mail`
// is not "mail is not secured", and an unreadable authority list is not "only
// Let's Encrypt".
// Bu ekranın okudukları. Her yanıt ya sözleşmedir ya da bilinmeyendir: eksik
// `has_certificate` "sertifika yok" değildir.
export function decodeSSLData(raw: unknown): SSLData {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Record<string, unknown>;
    const settings = body.settings as Record<string, unknown> | null | undefined;
    if (
        typeof body.has_certificate !== 'boolean' ||
        !settings || typeof settings !== 'object' ||
        typeof settings.force_https !== 'boolean' ||
        typeof settings.hsts_enabled !== 'boolean' ||
        typeof settings.hsts_max_age !== 'number'
    ) {
        throw new Error('field');
    }
    if (body.has_certificate && (!body.certificate || typeof body.certificate !== 'object')) throw new Error('certificate');
    return raw as SSLData;
}

function decodeSecureMail(raw: unknown): boolean {
    if (!raw || typeof raw !== 'object' || typeof (raw as { secure_mail?: unknown }).secure_mail !== 'boolean') {
        throw new Error('shape');
    }
    return (raw as { secure_mail: boolean }).secure_mail;
}

const SSL_PROVIDERS_URL = '/api/v1/ssl/providers';
const decodeProviders = (raw: unknown) => decodeListIn<SSLProvider>(raw, 'providers');
const NO_PROVIDERS: SSLProvider[] = [];

export function DomainSSLSettings({
    domainId,
    domainName,
    mailAvailable,
    readOnly = false,
    onCertificateChange,
}: DomainSSLSettingsProps) {
    const { t, locale } = useI18n();
    // Another domain is another address: its answer never appears under this
    // one, and an answer for the domain just left is dropped.
    // Başka alan adı başka adrestir: yanıtı bunun altında görünmez.
    const ssl = useRemote(`/api/v1/domains/${domainId}/ssl`, decodeSSLData);
    const mailState = useRemote(mailAvailable === true ? `/api/v1/domains/${domainId}/ssl/mail` : null, decodeSecureMail);
    const providersRead = useRemote(SSL_PROVIDERS_URL, decodeProviders);
    const [clockMs, setClockMs] = useState(() => Date.now());
    const [issuing, setIssuing] = useState(false);
    const [email, setEmail] = useState('');
    const [autoRenew, setAutoRenew] = useState(true);
    const [includeMail, setIncludeMail] = useState(false);
    const [certSource, setCertSource] = useState<'letsencrypt' | 'custom'>('letsencrypt');
    // The CA the cert is issued from (operator, 23 Jul: "a few kinds of SSL").
    // The list comes from the server registry — the UI never hardcodes a CA.
    // Sertifikanın alındığı CA (operatör, 23 Tem: "birkaç çeşit SSL"). Liste
    // sunucu kayıt defterinden gelir — UI asla bir CA'yı sabitlemez.
    const providers = providersRead.remote.state === 'known' ? providersRead.remote.value : NO_PROVIDERS;
    const [provider, setProvider] = useState('letsencrypt');
    const [eabKid, setEabKid] = useState('');
    const [eabHmac, setEabHmac] = useState('');
    const [certFile, setCertFile] = useState<File | null>(null);
    const [keyFile, setKeyFile] = useState<File | null>(null);
    const [chainFile, setChainFile] = useState<File | null>(null);
    const [uploading, setUploading] = useState(false);
    const [showReissue, setShowReissue] = useState(false);
    const [retrying, setRetrying] = useState(false);
    const [removing, setRemoving] = useState(false);
    const [settingsBusy, setSettingsBusy] = useState(false);
    const [secureMailBusy, setSecureMailBusy] = useState(false);
    const [renewalBusy, setRenewalBusy] = useState(false);
    const currentDomainIdRef = useRef(domainId);
    const settingsMutationBusyRef = useRef(false);
    const secureMailMutationBusyRef = useRef(false);
    const renewalMutationBusyRef = useRef(false);

    // Event handlers from the previous render can finish after navigation. Keep
    // the current identity synchronously available so they cannot update the
    // newly selected domain before its effect has run.
    currentDomainIdRef.current = domainId;

    // What the server last said about this domain's certificate, if anything,
    // and whether that is still the latest read.
    // Sunucunun bu alan adının sertifikası hakkında en son söylediği ve bunun
    // hâlâ son okuma olup olmadığı.
    const observed = lastKnown(ssl.remote);
    const data = observed?.value ?? null;
    const observedAt = observed?.observedAt ?? 0;
    // Whether mail uses the certificate: `false` only where the server has no
    // mail or said so, `null` whenever it is not known.
    // Postanın sertifikayı kullanıp kullanmadığı: yalnız sunucu söylediğinde
    // `false`, bilinmediğinde `null`.
    const secureMail: boolean | null =
        mailAvailable === false ? false : mailState.remote.state === 'known' ? mailState.remote.value : null;
    // Nothing that changes the certificate or its settings is offered on an
    // earlier answer, or while the answer is being read again.
    // Sertifikayı ya da ayarlarını değiştiren hiçbir şey, önceki bir yanıt
    // üzerinde ya da yanıt yeniden okunurken sunulmaz.
    const locked = ssl.remote.state !== 'known' || ssl.reading;

    useEffect(() => {
        setShowReissue(false);
        setIncludeMail(false);
        setCertSource('letsencrypt');
        setSettingsBusy(settingsMutationBusyRef.current);
        setSecureMailBusy(secureMailMutationBusyRef.current);
        setRenewalBusy(renewalMutationBusyRef.current);
    }, [domainId]);

    // The issue form starts from what the server said, each time it says it.
    // Verme formu, sunucunun her söyleyişinde onun söylediğinden başlar.
    useEffect(() => {
        if (ssl.remote.state !== 'known') return;
        const next = ssl.remote.value;
        onCertificateChange?.({
            activated: next.certificate?.activated === true,
            usable: next.certificate?.usable === true,
        });
        if (next.certificate?.type === 'letsencrypt') {
            setAutoRenew(next.certificate.auto_renew);
        } else if (!next.has_certificate) {
            setAutoRenew(true);
        }
        const loadedMailName = `mail.${normaliseDNSName(next.domain_name || domainName)}`;
        const loadedDNSNames = Array.isArray(next.certificate?.dns_names)
            ? uniqueDNSNames(next.certificate.dns_names)
            : [];
        setIncludeMail(
            mailAvailable === true &&
                next.has_certificate &&
                loadedDNSNames.some((name) => normaliseDNSName(name) === loadedMailName),
        );
        // Keyed on the answer itself: a new read, or another domain's.
    }, [observedAt, ssl.remote.state, domainId, mailAvailable]);

    useEffect(() => {
        const now = Date.now();
        setClockMs(now);
        const retireAfter = parseActiveHSTSRetirement(data?.settings.hsts_retire_after, now);
        if (!retireAfter) return;

        let timer: number | undefined;
        const tick = () => {
            const now = Date.now();
            setClockMs(now);
            const remainingMs = retireAfter.getTime() - now;
            if (remainingMs <= 0) return;
            timer = window.setTimeout(tick, remainingMs <= 60_000 ? 1_000 : 60_000);
        };
        tick();
        return () => {
            if (timer !== undefined) window.clearTimeout(timer);
        };
    }, [data?.settings.hsts_retire_after]);

    useEffect(() => {
        const certificate = data?.certificate;
        if (!certificate || certificate.type !== 'letsencrypt') return;
        const certificateProvider = resolveCertificateProvider(certificate, providers);
        if (!certificateProvider) return;
        setProvider(certificateProvider);
        setEabKid('');
        setEabHmac('');
    }, [data?.certificate?.id, data?.certificate?.issuer, data?.certificate?.provider_id, data?.certificate?.type, providers]);

    // Reads the certificate and the mail state again. It only reads. The
    // handles belong to the domain they were made for, so a change that
    // finishes after the person moved to another domain re-reads nothing there.
    // Sertifikayı ve posta durumunu yeniden okur; yalnız okur.
    const retrySSL = ssl.retry;
    const retryMailState = mailState.retry;
    const loadSSLData = async () => {
        await Promise.all([retrySSL(), retryMailState()]);
    };

    // The answer to a change did not arrive: whether it was made is not known.
    // Nothing is sent again; the state is read so the person can look first.
    // Değişikliğin yanıtı gelmedi: yapılıp yapılmadığı bilinmiyor. Hiçbir şey
    // yeniden gönderilmez; kişi önce bakabilsin diye durum okunur.
    const resultUnknown = (targetDomainId: number) => {
        if (currentDomainIdRef.current === targetDomainId) showToast('error', t('common.resultUnknown'));
        return loadSSLData();
    };

    const handleIssue = async () => {
        if (readOnly || locked || providersRead.remote.state !== 'known') return;
        if (!email) return showToast('error', t('ssl.emailRequired'));
        const isReissue = data?.has_certificate === true;
        if (isReissue && mailAvailable !== false && secureMail === null) {
            showToast('warning', t('ssl.mailStateUnknownReplacement'));
            return;
        }
        const confirmationKey: TranslationKey =
            isReissue && secureMail === true && !includeMail
                ? 'ssl.reissueConfirmMail'
                : isReissue && includeMail
                  ? 'ssl.reissueConfirmWithMail'
                  : includeMail
                    ? 'ssl.issueConfirmWithMail'
                    : isReissue
                      ? 'ssl.reissueConfirm'
                      : 'ssl.issueConfirm';
        const authority = providers.find((candidate) => candidate.id === provider)?.name ?? provider;
        if (!confirm(t(confirmationKey, { name: domainName, mailName: `mail.${normaliseDNSName(domainName)}`, authority }))) return;
        const targetDomainId = domainId;
        setIssuing(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl/letsencrypt`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    email,
                    auto_renew: autoRenew,
                    include_mail: mailAvailable === true && includeMail,
                    provider,
                    eab_key_id: eabKid,
                    eab_hmac_key: eabHmac,
                    reissue: isReissue,
                }),
            });
            if (!res.ok) {
                const apiError = await readApiError(res);
                if (
                    apiError.code === 'ssl_activation_pending' ||
                    apiError.code === 'ssl_dependents_pending'
                ) {
                    showToast(
                        'warning',
                        t(
                            apiError.code === 'ssl_activation_pending'
                                ? 'err.ssl_activation_pending'
                                : 'ssl.dependentsPending',
                        ),
                    );
                    setShowReissue(false);
                    await loadSSLData();
                    return;
                }
                showToast('error', apiErrorText(apiError, t, isReissue ? 'ssl.reissueFailed' : 'ssl.issueFailed'));
                return;
            }
            showToast('success', t(isReissue ? 'ssl.reissued' : 'ssl.issued'));
            setShowReissue(false);
            // The screen keeps what it showed, with its controls off, until
            // the server has said what the certificate is now.
            await loadSSLData();
        } catch {
            await resultUnknown(targetDomainId);
        } finally {
            setEabKid('');
            setEabHmac('');
            setIssuing(false);
        }
    };

    const handleUpload = async (e: React.FormEvent) => {
        e.preventDefault();
        if (readOnly || locked) return;
        if (!certFile || !keyFile) return showToast('error', t('ssl.certKeyRequired'));
        const isReplacement = data?.has_certificate === true;
        if (isReplacement && mailAvailable !== false && secureMail === null) {
            showToast('warning', t('ssl.mailStateUnknownReplacement'));
            return;
        }
        if (!confirm(t(isReplacement ? 'ssl.replaceCertificateConfirm' : 'ssl.uploadConfirm', { name: domainName }))) return;
        const targetDomainId = domainId;
        setUploading(true);
        try {
            const fd = new FormData();
            fd.append('certificate', certFile);
            fd.append('private_key', keyFile);
            if (chainFile) fd.append('chain', chainFile);
            const res = await fetch(`/api/v1/domains/${domainId}/ssl/upload`, { method: 'POST', body: fd });
            if (!res.ok) {
                const apiError = await readApiError(res);
                if (
                    apiError.code === 'ssl_activation_pending' ||
                    apiError.code === 'ssl_dependents_pending'
                ) {
                    showToast(
                        'warning',
                        t(
                            apiError.code === 'ssl_activation_pending'
                                ? 'err.ssl_activation_pending'
                                : 'ssl.dependentsPending',
                        ),
                    );
                    setCertFile(null);
                    setKeyFile(null);
                    setChainFile(null);
                    setShowReissue(false);
                    await loadSSLData();
                    return;
                }
                showToast('error', apiErrorText(apiError, t, 'ssl.uploadFailed'));
                return;
            }
            showToast('success', t('ssl.uploaded'));
            setCertFile(null);
            setKeyFile(null);
            setChainFile(null);
            setShowReissue(false);
            await loadSSLData();
        } catch {
            await resultUnknown(targetDomainId);
        } finally {
            setUploading(false);
        }
    };

    const handleUpdateSettings = async (updates: Partial<SSLSettings>) => {
        if (readOnly || locked || !data || settingsMutationBusyRef.current) return;
        const targetDomainId = domainId;
        const nextSettings = { ...data.settings, ...updates };
        const settingsRequest = {
            force_https: nextSettings.force_https,
            hsts_enabled: nextSettings.hsts_enabled,
            hsts_max_age: nextSettings.hsts_max_age,
        };
        settingsMutationBusyRef.current = true;
        setSettingsBusy(true);
        try {
            const res = await fetch(`/api/v1/domains/${targetDomainId}/ssl/settings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(settingsRequest),
            });
            if (!res.ok) {
                const apiError = await readApiError(res);
                if (currentDomainIdRef.current === targetDomainId) {
                    showToast('error', apiErrorText(apiError, t, 'ssl.settingsFailed'));
                }
                return;
            }
            if (currentDomainIdRef.current === targetDomainId) {
                showToast('success', t('ssl.settingsSaved'));
            }
            await loadSSLData();
        } catch {
            await resultUnknown(targetDomainId);
        } finally {
            settingsMutationBusyRef.current = false;
            setSettingsBusy(false);
        }
    };

    const handleSecureMailChange = async (nextSecureMail: boolean) => {
        if (readOnly || locked || secureMailMutationBusyRef.current || secureMail === null || mailAvailable !== true) return;
        const targetDomainId = domainId;
        secureMailMutationBusyRef.current = true;
        setSecureMailBusy(true);
        try {
            const res = await fetch(`/api/v1/domains/${targetDomainId}/ssl/mail`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ secure_mail: nextSecureMail }),
            });
            if (!res.ok) {
                const apiError = await readApiError(res);
                if (currentDomainIdRef.current === targetDomainId) {
                    showToast('error', apiErrorText(apiError, t));
                }
                return;
            }
            if (currentDomainIdRef.current === targetDomainId) {
                showToast('success', nextSecureMail ? t('ssl.mailSecured') : t('ssl.mailUnsecured'));
            }
            await loadSSLData();
        } catch {
            await resultUnknown(targetDomainId);
        } finally {
            secureMailMutationBusyRef.current = false;
            setSecureMailBusy(false);
        }
    };

    const handleAutoRenewChange = async (nextAutoRenew: boolean) => {
        if (readOnly || locked || renewalMutationBusyRef.current) return;
        const targetDomainId = domainId;
        renewalMutationBusyRef.current = true;
        setRenewalBusy(true);
        try {
            const res = await fetch(`/api/v1/domains/${targetDomainId}/ssl/renewal`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ auto_renew: nextAutoRenew }),
            });
            if (!res.ok) {
                const apiError = await readApiError(res);
                if (currentDomainIdRef.current === targetDomainId) {
                    showToast('error', apiErrorText(apiError, t, 'ssl.autoRenewFailed'));
                }
                return;
            }
            if (currentDomainIdRef.current === targetDomainId) {
                showToast('success', t('ssl.autoRenewSaved'));
            }
            await loadSSLData();
        } catch {
            await resultUnknown(targetDomainId);
        } finally {
            renewalMutationBusyRef.current = false;
            setRenewalBusy(false);
        }
    };

    const handleRetryActivation = async () => {
        if (readOnly || locked) return;
        const targetDomainId = domainId;
        setRetrying(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl/retry`, { method: 'POST' });
            if (!res.ok) {
                const apiError = await readApiError(res);
                showToast('error', apiErrorText(apiError, t, 'ssl.retryFailed'));
                return;
            }
            showToast('success', t('ssl.retrySucceeded'));
            await loadSSLData();
        } catch {
            await resultUnknown(targetDomainId);
        } finally {
            setRetrying(false);
        }
    };

    const handleDelete = async () => {
        if (readOnly || locked) return;
        if (data?.settings.hsts_enabled) {
            showToast('warning', t('ssl.removeBlockedByHsts'));
            return;
        }
        const retirementUntil = parseActiveHSTSRetirement(data?.settings.hsts_retire_after, Date.now());
        if (retirementUntil) {
            showToast(
                'warning',
                t('ssl.removeBlockedByHstsRetirement', {
                    date: formatHSTSRetirementDate(retirementUntil, locale),
                    remaining: formatHSTSRemaining(retirementUntil, Date.now(), locale),
                }),
            );
            return;
        }
        if (!confirm(t('ssl.confirmRemove', { name: domainName }))) return;
        const targetDomainId = domainId;
        setRemoving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl`, { method: 'DELETE' });
            if (!res.ok) {
                const apiError = await readApiError(res);
                showToast('error', apiErrorText(apiError, t, 'common.error'));
                return;
            }
            showToast('success', t('ssl.removed'));
            await loadSSLData();
        } catch {
            await resultUnknown(targetDomainId);
        } finally {
            setRemoving(false);
        }
    };

    if (ssl.remote.state === 'loading') return <Checking label={t('ssl.checking')} className="py-3" />;
    if (!data) {
        return <CouldNotCheck text={t('ssl.unknown')} onRetry={() => void loadSSLData()} busy={ssl.reading} />;
    }
    const stale = ssl.remote.state === 'unknown';

    // The backend may retain historical certificate details after the active
    // assignment is removed. has_certificate is authoritative; stale details
    // must never make the heading or controls claim that SSL is still active.
    const cert = data.has_certificate ? data.certificate : undefined;
    const managedNamesFromServer = Array.isArray(data.managed_names) ? data.managed_names : null;
    const managedNames = uniqueDNSNames(managedNamesFromServer?.length ? managedNamesFromServer : [domainName]);
    const certificateDNSNames = Array.isArray(cert?.dns_names) ? uniqueDNSNames(cert.dns_names) : null;
    const mailName = `mail.${normaliseDNSName(domainName)}`;
    const mailCovered = certificateDNSNames !== null && certificateCoversName(certificateDNSNames, mailName);
    const certificateReady = cert?.usable === true;
    const mailStateUnknown = mailAvailable !== false && secureMail === null;
    const replacementBlockedByMailState = data.has_certificate && mailStateUnknown;
    const plannedCertificateNames = uniqueDNSNames([
        ...managedNames,
        ...(mailAvailable === true && includeMail ? [mailName] : []),
    ]);
    const renewalState = cert ? renewalStatusMeta(cert.renewal_status) : null;
    const hstsRetirementUntil = parseActiveHSTSRetirement(data.settings.hsts_retire_after, clockMs);
    const hstsRetirementDate = hstsRetirementUntil
        ? formatHSTSRetirementDate(hstsRetirementUntil, locale)
        : '';
    const hstsRetirementRemaining = hstsRetirementUntil
        ? formatHSTSRemaining(hstsRetirementUntil, clockMs, locale)
        : '';
    // The status order is shared with the overview card so one certificate can
    // never be shown in two different states on the same domain page.
    const status = sslTier(cert);
    const tier = {
        none: { icon: XCircle, color: 'text-fg-subtle' },
        pending: { icon: AlertTriangle, color: 'text-warning' },
        invalid: { icon: XCircle, color: 'text-danger' },
        untrusted: { icon: Shield, color: 'text-danger' },
        trustUnknown: { icon: AlertTriangle, color: 'text-warning' },
        expired: { icon: XCircle, color: 'text-danger' },
        inactive: { icon: AlertTriangle, color: 'text-warning' },
        incomplete: { icon: AlertTriangle, color: 'text-warning' },
        expiring: { icon: AlertTriangle, color: 'text-warning' },
        dependentsPending: { icon: AlertTriangle, color: 'text-warning' },
        valid: { icon: CheckCircle, color: 'text-success' },
    }[status];
    const TierIcon = tier.icon;

    return (
        <div>
            {/* An earlier answer is said to be one, with when it was read; a
                re-read after this screen's own change says that it is reading.
                In both cases every control below that changes something is off.
                Önceki yanıtın önceki olduğu ve ne zaman okunduğu söylenir; bu
                ekranın kendi değişikliğinden sonraki okuma da okuduğunu söyler.
                İki durumda da aşağıdaki değiştiren her denetim kapalıdır. */}
            {stale ? (
                <CouldNotCheck
                    className="mb-4"
                    text={t('common.staleNotice', {
                        time: new Date(observedAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' }),
                    })}
                    onRetry={() => void loadSSLData()}
                    busy={ssl.reading}
                />
            ) : ssl.reading ? (
                <Checking label={t('ssl.rereading')} className="mb-4" />
            ) : null}
            {/* Certificate status */}
            <FormSection title="SSL/TLS">
                <div className="flex items-start gap-3">
                    <TierIcon className={`mt-0.5 h-6 w-6 shrink-0 ${tier.color}`} />
                    <div className="min-w-0 flex-1">
                    <p className="font-semibold text-fg">{t(sslTierLabel[status])}</p>
                        {data.has_certificate && cert ? (
                            <>
                                <dl className="mt-2 grid grid-cols-1 gap-x-6 gap-y-1.5 text-sm sm:grid-cols-2">
                                    <Detail
                                        label={t('ssl.type')}
                                        value={t(cert.type === 'letsencrypt' ? 'ssl.type.acme' : 'ssl.type.custom')}
                                    />
                                    <Detail label={t('ssl.issuer')} value={cert.issuer} />
                                    <Detail
                                        label={t('ssl.trust')}
                                        value={
                                            <span className={cert.trust_status === 'trusted' ? 'text-success' : 'text-warning'}>
                                                {t(`ssl.trust.${cert.trust_status}` as TranslationKey)}
                                            </span>
                                        }
                                    />
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
                                    {renewalState && (
                                        <Detail
                                            label={t('ssl.renewalStatus')}
                                            value={
                                                <span className={renewalState.color}>
                                                    {t(renewalState.label)}
                                                </span>
                                            }
                                        />
                                    )}
                                </dl>
                                {(cert.activation_pending || cert.dependents_pending) && (
                                    <div className="mt-4 flex flex-col gap-3 rounded-lg border border-warning-mark/50 bg-warning-mark/20 px-3 py-3 text-sm text-fg-muted sm:flex-row sm:items-center sm:justify-between">
                                        <span className="flex items-start gap-2">
                                            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                                            <span>
                                                {t(cert.activation_pending ? 'ssl.activationPending' : 'ssl.dependentsPending')}
                                            </span>
                                        </span>
                                        {!readOnly && (
                                            <Button
                                                type="button"
                                                variant="secondary"
                                                icon={RefreshCw}
                                                onClick={handleRetryActivation}
                                                disabled={readOnly || locked || retrying}
                                                className="shrink-0"
                                            >
                                                {t(retrying ? 'ssl.retrying' : 'ssl.retryActivation')}
                                            </Button>
                                        )}
                                    </div>
                                )}
                                {cert.trust_status !== 'trusted' && cert.trust_error && (
                                    <p className="mt-3 break-words rounded-lg border border-danger/30 bg-danger/5 px-3 py-2 text-xs text-fg-muted">
                                        {cert.trust_error}
                                    </p>
                                )}
                                <SecuredNames
                                    names={managedNames}
                                    primaryName={domainName}
                                    certificateDNSNames={certificateDNSNames}
                                    inventoryComplete={managedNamesFromServer !== null}
                                />
                                {cert.type === 'letsencrypt' && (
                                    <div className="mt-4 border-t border-border pt-4">
                                        <ControlledToggle
                                            checked={cert.auto_renew}
                                            onChange={handleAutoRenewChange}
                                            label={t('ssl.autoRenewOn')}
                                            hint={t('ssl.autoRenewHint')}
                                            disabled={readOnly || locked || renewalBusy}
                                        />
                                    </div>
                                )}
                                <div className="mt-4 flex flex-wrap gap-2">
                                    {cert.type === 'letsencrypt' ? (
                                        <Button
                                            variant="secondary"
                                            icon={RefreshCw}
                                            onClick={() => {
                                                setCertSource('letsencrypt');
                                                setShowReissue((current) => !current);
                                            }}
                                            disabled={readOnly || locked || issuing}
                                        >
                                            {showReissue ? t('common.cancel') : t('ssl.reissue')}
                                        </Button>
                                    ) : (
                                        <Button
                                            variant="secondary"
                                            icon={Upload}
                                            onClick={() => {
                                                setCertSource('custom');
                                                setShowReissue((current) => !current);
                                            }}
                                            disabled={readOnly || locked || uploading}
                                        >
                                            {showReissue ? t('common.cancel') : t('ssl.replaceCustom')}
                                        </Button>
                                    )}
                                    <Button
                                        variant="danger"
                                        icon={Unlink}
                                        onClick={handleDelete}
                                        disabled={readOnly || locked || issuing || removing || data.settings.hsts_enabled || hstsRetirementUntil !== null}
                                    >
                                        {t('ssl.remove')}
                                    </Button>
                                </div>
                                {data.settings.hsts_enabled ? (
                                    <p className="mt-3 flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 px-3 py-2 text-xs text-fg-muted">
                                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                                        <span>{t('ssl.removeBlockedByHsts')}</span>
                                    </p>
                                ) : hstsRetirementUntil ? (
                                    <p className="mt-3 flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 px-3 py-2 text-xs text-fg-muted">
                                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                                        <span>
                                            {t('ssl.hstsRetirementActive', {
                                                date: hstsRetirementDate,
                                                remaining: hstsRetirementRemaining,
                                            })}
                                        </span>
                                    </p>
                                ) : null}
                            </>
                        ) : (
                            <p className="mt-1 text-sm text-fg-muted">{t('ssl.noCert')}</p>
                        )}
                    </div>
                </div>
            </FormSection>

            {/* Issuance and replacement are always explicit. Opening the page
                never renews or replaces a certificate on its own. */}
            {!readOnly && (!data.has_certificate || showReissue) && (
                <FormSection
                    title={
                        data.has_certificate
                            ? t(certSource === 'custom' ? 'ssl.replaceCertificateTitle' : 'ssl.reissueTitle')
                            : t('domain.sub.ssl')
                    }
                >
                    <div className="mb-4 flex gap-1 border-b border-border">
                        <TabBtn active={certSource === 'letsencrypt'} onClick={() => setCertSource('letsencrypt')} icon={Shield} label={t('ssl.tab.letsencrypt')} />
                        <TabBtn active={certSource === 'custom'} onClick={() => setCertSource('custom')} icon={Upload} label={t('ssl.tab.custom')} />
                    </div>

                    {replacementBlockedByMailState && (
                        <p className="mb-4 flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 px-3 py-2 text-xs text-fg-muted">
                            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                            <span>{t('ssl.mailStateUnknownReplacement')}</span>
                        </p>
                    )}

                    {certSource === 'letsencrypt' ? (
                        <div className="space-y-3">
                            <p className="text-sm text-fg-muted">
                                {t(data.has_certificate ? 'ssl.reissueDesc' : 'ssl.letsencryptDesc')}
                            </p>
                            <PlannedCertificateNames
                                names={plannedCertificateNames}
                                inventoryComplete={managedNamesFromServer !== null}
                            />
                            {/* The authority is one the server named. Until
                                the list is known the request is not offered;
                                there is no assumed authority.
                                Yetkili, sunucunun adlandırdığı biridir. Liste
                                bilinene dek istek sunulmaz. */}
                            {providersRead.remote.state === 'loading' && <Checking label={t('ssl.checkingProviders')} />}
                            {providersRead.remote.state === 'unknown' && (
                                <CouldNotCheck
                                    text={t('ssl.providersUnknown')}
                                    onRetry={() => void providersRead.retry()}
                                    busy={providersRead.reading}
                                />
                            )}
                            {providers.length > 1 && (
                                <Field label={t('ssl.provider')} hint={providers.find((p) => p.id === provider)?.note}>
                                    <select
                                        value={provider}
                                        onChange={(e) => {
                                            setProvider(e.target.value);
                                            setEabKid('');
                                            setEabHmac('');
                                        }}
                                        className={inputClass}
                                    >
                                        {providers.map((p) => (
                                            <option key={p.id} value={p.id}>{p.name}</option>
                                        ))}
                                    </select>
                                </Field>
                            )}
                            {/* EAB fields appear only for a CA that requires them
                                (ZeroSSL, Google) — the panel refuses issuance
                                without them, so asking here is honest, not
                                optional decoration.
                                EAB alanları yalnız gerektiren CA'da görünür
                                (ZeroSSL, Google) — panel bunlarsız vermeyi
                                reddeder; burada sormak dürüsttür, isteğe bağlı
                                süs değil. */}
                            {providers.find((p) => p.id === provider)?.needs_eab && (
                                <>
                                    <Field label={t('ssl.eabKid')} hint={t('ssl.eabHint')}>
                                        <input value={eabKid} onChange={(e) => setEabKid(e.target.value)} className={inputClass} autoComplete="off" />
                                    </Field>
                                    <Field label={t('ssl.eabHmac')}>
                                        <input
                                            type="password"
                                            value={eabHmac}
                                            onChange={(e) => setEabHmac(e.target.value)}
                                            className={inputClass}
                                            autoComplete="new-password"
                                        />
                                    </Field>
                                </>
                            )}
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
                            {mailAvailable === true ? (
                                <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-border bg-surface-subtle px-3 py-3">
                                    <input
                                        type="checkbox"
                                        checked={includeMail}
                                        onChange={(e) => setIncludeMail(e.target.checked)}
                                        className="mt-0.5 h-4 w-4 shrink-0 accent-primary"
                                    />
                                    <span>
                                        <span className="block text-sm font-medium text-fg">
                                            {t('ssl.includeMail', { name: mailName })}
                                        </span>
                                        <span className="mt-1 block text-xs text-fg-muted">
                                            {t('ssl.includeMailHint', { name: mailName })}
                                        </span>
                                    </span>
                                </label>
                            ) : (
                                <p className="flex items-start gap-2 rounded-lg border border-border bg-surface-subtle px-3 py-2 text-xs text-fg-muted">
                                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-fg-subtle" />
                                    <span>{t(mailAvailable === false ? 'ssl.mailUnavailable' : 'ssl.mailCapabilityUnknown')}</span>
                                </p>
                            )}
                            <Button
                                variant="primary"
                                icon={Lock}
                                onClick={handleIssue}
                                disabled={locked || providersRead.remote.state !== 'known' || issuing || !email || replacementBlockedByMailState}
                            >
                                {issuing
                                    ? t(data.has_certificate ? 'ssl.reissuing' : 'ssl.issuing')
                                    : t(data.has_certificate ? 'ssl.reissue' : 'ssl.issue')}
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
                                <Button
                                    type="submit"
                                    variant="primary"
                                    icon={Upload}
                                    disabled={locked || uploading || !certFile || !keyFile || replacementBlockedByMailState}
                                >
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
                    onChange={(v) => {
                        if (!v && data.settings.hsts_enabled) {
                            showToast('warning', t('ssl.forceHttpsRequiredByHsts'));
                            return;
                        }
                        handleUpdateSettings({ force_https: v });
                    }}
                    label={t('ssl.forceHttps')}
                    hint={
                        !data.has_certificate
                            ? t('ssl.httpsCertificateRequired')
                            : !certificateReady && !data.settings.force_https
                              ? t('ssl.usableCertificateRequired')
                            : data.settings.hsts_enabled
                              ? t('ssl.forceHttpsRequiredByHsts')
                              : t('ssl.forceHttpsHint')
                    }
                    disabled={
                        readOnly ||
                        locked ||
                        settingsBusy ||
                        (!certificateReady && !data.settings.force_https) ||
                        (data.settings.hsts_enabled && data.settings.force_https)
                    }
                />
                <ControlledToggle
                    checked={data.settings.hsts_enabled}
                    onChange={(v) =>
                        handleUpdateSettings({
                            hsts_enabled: v,
                            force_https: v ? true : data.settings.force_https,
                            hsts_max_age: v ? INITIAL_HSTS_MAX_AGE : data.settings.hsts_max_age,
                        })
                    }
                    label={t('ssl.hsts')}
                    hint={
                        !data.has_certificate
                            ? t('ssl.httpsCertificateRequired')
                            : !certificateReady && !data.settings.hsts_enabled
                              ? t('ssl.usableCertificateRequired')
                              : hstsRetirementUntil && !data.settings.hsts_enabled
                                ? t('ssl.hstsRetirementActive', {
                                      date: hstsRetirementDate,
                                      remaining: hstsRetirementRemaining,
                                  })
                              : t('ssl.hstsHint')
                    }
                    disabled={readOnly || locked || settingsBusy || (!certificateReady && !data.settings.hsts_enabled)}
                />
                {data.has_certificate && data.settings.hsts_enabled && (
                    <div className="ml-7 space-y-2">
                        <Field label={t('ssl.hstsMaxAge')} hint={t('ssl.hstsMaxAgeHint')}>
                            <select
                                value={data.settings.hsts_max_age}
                                onChange={(event) => handleUpdateSettings({ hsts_max_age: Number(event.target.value) })}
                                className={inputClass}
                                disabled={readOnly || locked || settingsBusy}
                            >
                                {!HSTS_PRESETS.some((preset) => preset.seconds === data.settings.hsts_max_age) && (
                                    <option value={data.settings.hsts_max_age}>
                                        {t('ssl.hstsMaxAge.current', { seconds: data.settings.hsts_max_age })}
                                    </option>
                                )}
                                {HSTS_PRESETS.map((preset) => (
                                    <option key={preset.seconds} value={preset.seconds}>
                                        {t(preset.label)}
                                    </option>
                                ))}
                            </select>
                        </Field>
                        <p className="flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 px-3 py-2 text-xs text-fg-muted">
                            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                            <span>{t('ssl.hstsHttpsOnly')}</span>
                        </p>
                    </div>
                )}
                {data.has_certificate && mailAvailable === true && (
                    <ControlledToggle
                        checked={secureMail === true}
                        onChange={handleSecureMailChange}
                        label={t('ssl.secureMail')}
                        hint={
                            secureMail === null
                                ? t('ssl.mailStateUnknown')
                                : !certificateReady
                                ? t('ssl.usableCertificateRequired')
                                : certificateDNSNames === null
                                ? t('ssl.secureMailCoverageUnknown', { name: mailName })
                                : mailCovered
                                  ? t('ssl.secureMailHint', { name: mailName })
                                  : t('ssl.secureMailNotCovered', { name: mailName })
                        }
                        disabled={
                            readOnly ||
                            locked ||
                            secureMailBusy ||
                            secureMail === null ||
                            (secureMail !== true && (!certificateReady || !mailCovered))
                        }
                    />
                )}
                {data.has_certificate && mailAvailable !== true && (
                    <p className="flex items-start gap-2 rounded-lg border border-border bg-surface-subtle px-3 py-2 text-xs text-fg-muted">
                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-fg-subtle" />
                        <span>{t(mailAvailable === false ? 'ssl.mailUnavailable' : 'ssl.mailCapabilityUnknown')}</span>
                    </p>
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

function ControlledToggle({
    checked,
    onChange,
    label,
    hint,
    disabled = false,
}: {
    checked: boolean;
    onChange: (v: boolean) => void;
    label: string;
    hint?: string;
    disabled?: boolean;
}) {
    return (
        <label className={`flex items-start gap-3 ${disabled ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'}`}>
            <input
                type="checkbox"
                checked={checked}
                disabled={disabled}
                onChange={(e) => onChange(e.target.checked)}
                className="mt-0.5 h-4 w-4 accent-primary"
            />
            <span>
                <span className="block text-sm text-fg">{label}</span>
                {hint && <span className="block text-xs text-fg-subtle">{hint}</span>}
            </span>
        </label>
    );
}

function PlannedCertificateNames({
    names,
    inventoryComplete,
}: {
    names: string[];
    inventoryComplete: boolean;
}) {
    const { t } = useI18n();

    return (
        <div className="rounded-lg border border-border bg-surface-subtle px-3 py-3">
            <p className="text-sm font-semibold text-fg">{t('ssl.willCover')}</p>
            <p className="mt-0.5 text-xs text-fg-muted">{t('ssl.willCoverHint')}</p>
            {!inventoryComplete && <p className="mt-1 text-xs text-warning">{t('ssl.managedNamesUnknown')}</p>}
            <div className="mt-2 flex flex-wrap gap-1.5">
                {names.map((name) => (
                    <span
                        key={normaliseDNSName(name)}
                        className="rounded-md border border-border bg-surface px-2 py-1 font-mono text-xs text-fg-muted"
                    >
                        {name}
                    </span>
                ))}
            </div>
        </div>
    );
}

function SecuredNames({
    names,
    primaryName,
    certificateDNSNames,
    inventoryComplete,
}: {
    names: string[];
    primaryName: string;
    certificateDNSNames: string[] | null;
    inventoryComplete: boolean;
}) {
    const { t } = useI18n();
    const normalisedPrimary = normaliseDNSName(primaryName);

    return (
        <div className="mt-4 border-t border-border pt-4">
            <p className="text-sm font-semibold text-fg">{t('ssl.securedNames')}</p>
            <p className="mt-0.5 text-xs text-fg-muted">
                {certificateDNSNames === null ? t('ssl.securedNamesUnknown') : t('ssl.securedNamesHint')}
            </p>
            {!inventoryComplete && <p className="mt-1 text-xs text-warning">{t('ssl.managedNamesUnknown')}</p>}
            <ul className="mt-3 divide-y divide-border rounded-lg border border-border">
                {names.map((name) => {
                    const covered =
                        certificateDNSNames === null ? null : certificateCoversName(certificateDNSNames, name);
                    const normalisedName = normaliseDNSName(name);
                    const component =
                        normalisedName === normalisedPrimary
                            ? t('ssl.component.domain')
                            : normalisedName === `www.${normalisedPrimary}`
                              ? t('ssl.component.www')
                              : t('ssl.component.alias');

                    return (
                        <li key={normalisedName} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
                            <span className="min-w-0">
                                <span className="block font-medium text-fg">{name}</span>
                                <span className="block text-xs text-fg-subtle">{component}</span>
                            </span>
                            <span
                                className={
                                    covered === null
                                        ? 'inline-flex items-center gap-1.5 text-fg-subtle'
                                        : covered
                                          ? 'inline-flex items-center gap-1.5 text-success'
                                          : 'inline-flex items-center gap-1.5 text-danger'
                                }
                            >
                                {covered === null ? (
                                    <AlertTriangle className="h-4 w-4" />
                                ) : covered ? (
                                    <CheckCircle className="h-4 w-4" />
                                ) : (
                                    <XCircle className="h-4 w-4" />
                                )}
                                {covered === null
                                    ? t('ssl.coverage.unknown')
                                    : covered
                                      ? t('ssl.coverage.secured')
                                      : t('ssl.coverage.uncovered')}
                            </span>
                        </li>
                    );
                })}
            </ul>
            {certificateDNSNames !== null && certificateDNSNames.length > 0 && (
                <div className="mt-3">
                    <p className="text-xs font-medium text-fg-muted">{t('ssl.certificateNames')}</p>
                    <div className="mt-1.5 flex flex-wrap gap-1.5">
                        {certificateDNSNames.map((name) => (
                            <span
                                key={normaliseDNSName(name)}
                                className="rounded-md border border-border bg-surface-2 px-2 py-1 font-mono text-xs text-fg-muted"
                            >
                                {name}
                            </span>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
}

function renewalStatusMeta(status: string): { label: TranslationKey; color: string } | null {
    switch (status) {
        case 'current':
            return { label: 'ssl.renewal.current', color: 'text-success' };
        case 'renewed':
            return { label: 'ssl.renewal.renewed', color: 'text-success' };
        case 'expiring':
            return { label: 'ssl.renewal.expiring', color: 'text-warning' };
        case 'failed':
            return { label: 'ssl.renewal.failed', color: 'text-danger' };
        case 'activation_pending':
            return { label: 'ssl.renewal.activationPending', color: 'text-warning' };
        case 'dependents_pending':
            return { label: 'ssl.renewal.dependentsPending', color: 'text-warning' };
        default:
            return null;
    }
}

function normaliseDNSName(name: string): string {
    return name.trim().toLowerCase().replace(/\.$/, '');
}

function parseActiveHSTSRetirement(raw: string | null | undefined, nowMs: number): Date | null {
    if (!raw) return null;
    const retireAfter = new Date(raw);
    if (!Number.isFinite(retireAfter.getTime()) || retireAfter.getTime() <= nowMs) return null;
    return retireAfter;
}

function formatHSTSRetirementDate(retireAfter: Date, locale: 'tr' | 'en'): string {
    return retireAfter.toLocaleString(locale === 'tr' ? 'tr-TR' : 'en-US', {
        dateStyle: 'medium',
        timeStyle: 'medium',
    });
}

function formatHSTSRemaining(retireAfter: Date, nowMs: number, locale: 'tr' | 'en'): string {
    const remainingSeconds = Math.max(1, Math.ceil((retireAfter.getTime() - nowMs) / 1_000));
    const formatter = new Intl.RelativeTimeFormat(locale === 'tr' ? 'tr-TR' : 'en-US', { numeric: 'always' });

    if (remainingSeconds >= 86_400) return formatter.format(Math.ceil(remainingSeconds / 86_400), 'day');
    if (remainingSeconds >= 3_600) return formatter.format(Math.ceil(remainingSeconds / 3_600), 'hour');
    if (remainingSeconds >= 60) return formatter.format(Math.ceil(remainingSeconds / 60), 'minute');
    return formatter.format(remainingSeconds, 'second');
}

function uniqueDNSNames(names: string[]): string[] {
    const unique = new Map<string, string>();
    for (const name of names) {
        const normalised = normaliseDNSName(name);
        if (normalised && !unique.has(normalised)) unique.set(normalised, name.trim().replace(/\.$/, ''));
    }
    return [...unique.values()];
}

function resolveCertificateProvider(certificate: SSLCertificate, providers: SSLProvider[]): string | null {
    const explicitProvider = certificate.provider_id?.trim();
    if (explicitProvider) return explicitProvider;

    const issuer = certificate.issuer.trim().toLowerCase();
    if (!issuer) return null;
    const directMatch = providers.find((candidate) => {
        const id = candidate.id.trim().toLowerCase();
        const name = candidate.name.trim().toLowerCase();
        return (id && issuer.includes(id)) || (name && (issuer.includes(name) || name.includes(issuer)));
    });
    if (directMatch) return directMatch.id;

    const knownIssuers = [
        { id: 'letsencrypt', fragments: ["let's encrypt", 'isrg'] },
        { id: 'zerossl', fragments: ['zerossl'] },
        { id: 'google', fragments: ['google trust services', 'gts'] },
    ];
    const knownIssuer = knownIssuers.find((candidate) =>
        candidate.fragments.some((fragment) => issuer.includes(fragment)),
    );
    if (!knownIssuer) return null;
    return providers.find((candidate) => candidate.id.toLowerCase() === knownIssuer.id)?.id ?? knownIssuer.id;
}

export function certificateCoversName(dnsNames: string[], hostname: string): boolean {
    const normalisedHostname = normaliseDNSName(hostname);
    return dnsNames.some((dnsName) => {
        const pattern = normaliseDNSName(dnsName);
        if (pattern === normalisedHostname) return true;
        if (!pattern.startsWith('*.')) return false;

        const suffix = pattern.slice(2);
        if (!normalisedHostname.endsWith(`.${suffix}`)) return false;
        return normalisedHostname.split('.').length === suffix.split('.').length + 1;
    });
}
