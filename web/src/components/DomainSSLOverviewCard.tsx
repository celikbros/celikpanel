import { useEffect } from 'react';
import {
    AlertTriangle,
    ArrowRight,
    CheckCircle,
    Clock3,
    ShieldAlert,
} from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { sslTier, sslTierLabelFor, type SSLTier } from '../lib/sslTier';
import { Button } from './ui';
import { lastKnown, useRemote } from '../lib/remote';
import { decodeSSLData, type SSLRuntimeSummary } from './DomainSSLSettings';

interface OverviewCertificate {
    issuer: string;
    expires_at: string;
    days_until_expiry: number;
    activated: boolean;
    usable: boolean;
    trust_status: 'trusted' | 'untrusted' | 'unknown' | 'invalid';
    activation_pending: boolean;
    dependents_pending: boolean;
    waiting_for_owner?: boolean;
    waiting_for_owner_reason?: string;
}

interface OverviewSSLData {
    has_certificate: boolean;
    certificate?: OverviewCertificate;
}

interface Tier {
    icon: typeof CheckCircle;
    color: string;
    surface: string;
    label: TranslationKey;
}

const sslTierPresentation: Record<SSLTier, Omit<Tier, 'label'>> = {
    none: { icon: AlertTriangle, color: 'text-warning', surface: 'border-border-strong bg-surface-2' },
    pending: { icon: Clock3, color: 'text-warning', surface: 'border-border-strong bg-surface-2' },
    waitingForOwner: { icon: Clock3, color: 'text-warning', surface: 'border-border-strong bg-surface-2' },
    invalid: { icon: ShieldAlert, color: 'text-danger', surface: 'border-danger/30 bg-danger/5' },
    untrusted: { icon: ShieldAlert, color: 'text-danger', surface: 'border-danger/30 bg-danger/5' },
    trustUnknown: { icon: ShieldAlert, color: 'text-warning', surface: 'border-border-strong bg-surface-2' },
    expired: { icon: ShieldAlert, color: 'text-danger', surface: 'border-danger/30 bg-danger/5' },
    inactive: { icon: ShieldAlert, color: 'text-warning', surface: 'border-border-strong bg-surface-2' },
    incomplete: { icon: ShieldAlert, color: 'text-warning', surface: 'border-border-strong bg-surface-2' },
    expiring: { icon: Clock3, color: 'text-warning', surface: 'border-border-strong bg-surface-2' },
    dependentsPending: { icon: Clock3, color: 'text-warning', surface: 'border-border-strong bg-surface-2' },
    valid: { icon: CheckCircle, color: 'text-success', surface: 'border-success/30 bg-success/5' },
};

export function DomainSSLOverviewCard({
    domainId,
    onOpen,
    onCertificateChange,
}: {
    domainId: number;
    onOpen: () => void;
    onCertificateChange?: (status: SSLRuntimeSummary) => void;
}) {
    const { t, locale } = useI18n();
    // The same address and the same decoder as the SSL/TLS tab, so the card
    // and the tab can never disagree about what the answer means. The card
    // keeps one shape in its three states: checking, could not check (with the
    // read again), and the certificate the server described. After a read that
    // failed, an earlier answer stays, marked with the time it was read.
    // SSL/TLS sekmesiyle aynı adres ve aynı çözücü. Kart üç durumunda da tek
    // biçimdedir: kontrol ediliyor, kontrol edilemedi (yeniden okumayla) ve
    // sunucunun tarif ettiği sertifika. Başarısız okumadan sonra önceki yanıt,
    // okunduğu saatle işaretlenerek kalır.
    const ssl = useRemote(`/api/v1/domains/${domainId}/ssl`, decodeSSLData);
    const shown = lastKnown(ssl.remote);
    const data: OverviewSSLData | null = shown ? shown.value : null;
    const loading = ssl.remote.state === 'loading';
    const failed = ssl.remote.state === 'unknown' && !shown;
    const stale = ssl.remote.state === 'unknown' && shown !== undefined;

    const known = ssl.remote.state === 'known' ? ssl.remote.value : null;
    useEffect(() => {
        if (!known) return;
        onCertificateChange?.({
            activated: known.certificate?.activated === true,
            usable: known.certificate?.usable === true,
        });
    }, [known, onCertificateChange]);

    const cert = data?.certificate;
    const certificateTier = sslTier(data?.has_certificate ? cert : undefined);
    const tier: Tier = loading
        ? {
              icon: Clock3,
              color: 'text-fg-muted',
              surface: 'border-border bg-surface-2',
              label: 'domain.overview.ssl.checking',
          }
        : failed
          ? {
                icon: ShieldAlert,
                color: 'text-warning',
                surface: 'border-border-strong bg-surface-2',
                label: 'domain.overview.ssl.unavailable',
            }
          : {
                ...sslTierPresentation[certificateTier],
                label: sslTierLabelFor(certificateTier, cert),
            };
    const TierIcon = tier.icon;
    const hasCertificate = Boolean(data?.has_certificate && cert);

    let detail = t('domain.overview.ssl.loading');
    if (!loading) {
        if (failed) {
            detail = t('domain.overview.ssl.unavailableHint');
        } else if (!hasCertificate) {
            detail = t('domain.overview.ssl.none');
        } else if (cert?.activation_pending) {
            detail = t('domain.overview.ssl.activationPending');
        } else if (cert?.trust_status === 'unknown') {
            detail = t('domain.overview.ssl.trustUnknown');
        } else if (cert?.dependents_pending) {
            detail = t('domain.overview.ssl.dependentsPending');
        } else if (cert && !cert.usable) {
            detail = t('domain.overview.ssl.notUsable');
        } else if (cert) {
            detail = t('domain.overview.ssl.certificate', {
                issuer: cert.issuer,
                date: new Date(cert.expires_at).toLocaleDateString(),
                days: t('ssl.days', { n: cert.days_until_expiry }),
            });
        }
    }

    return (
        <section
            aria-labelledby="domain-overview-ssl-title"
            className={`mb-5 flex flex-col gap-4 rounded-xl border p-4 sm:flex-row sm:items-center ${tier.surface}`}
        >
            <div className="flex min-w-0 flex-1 items-start gap-3">
                <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-surface">
                    <TierIcon className={`h-5 w-5 ${tier.color}`} aria-hidden="true" />
                </span>
                <div className="min-w-0">
                    <h3 id="domain-overview-ssl-title" className="text-sm font-semibold text-fg">
                        {t('domain.overview.ssl.title')}
                    </h3>
                    <p className={`mt-0.5 text-sm font-medium ${tier.color}`} aria-live="polite">
                        {t(tier.label)}
                    </p>
                    {/* Two lines of room on a phone, where the usual answers
                        take two: the card under this one does not move when
                        the answer arrives.
                        Telefonda iki satırlık yer; yanıt gelince alttaki kart
                        oynamaz. */}
                    <p className="mt-1 min-h-[2.5rem] text-xs leading-relaxed text-fg-muted sm:min-h-0">{detail}</p>
                    {stale && shown && (
                        <p className="mt-1 text-xs leading-relaxed text-fg">
                            {t('domain.overview.ssl.stale', {
                                time: new Date(shown.observedAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' }),
                            })}
                        </p>
                    )}
                </div>
            </div>
            <div className="flex shrink-0 flex-wrap items-center gap-2 self-start sm:self-center">
                {ssl.remote.state === 'unknown' && (
                    <Button type="button" loading={ssl.reading} onClick={() => void ssl.retry()}>
                        {t('common.retry')}
                    </Button>
                )}
                <Button
                    type="button"
                    variant={ssl.remote.state === 'known' && (!hasCertificate || !cert?.usable) ? 'primary' : 'secondary'}
                    icon={ArrowRight}
                    onClick={onOpen}
                >
                    {t('domain.overview.ssl.open')}
                </Button>
            </div>
        </section>
    );
}
