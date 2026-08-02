import { useEffect, useState } from 'react';
import {
    AlertTriangle,
    ArrowRight,
    CheckCircle,
    Clock3,
    ShieldAlert,
} from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { sslTier, sslTierLabel, type SSLTier } from '../lib/sslTier';
import { Button } from './ui';
import type { SSLRuntimeSummary } from './DomainSSLSettings';

interface OverviewCertificate {
    issuer: string;
    expires_at: string;
    days_until_expiry: number;
    activated: boolean;
    usable: boolean;
    trust_status: 'trusted' | 'untrusted' | 'unknown' | 'invalid';
    activation_pending: boolean;
    dependents_pending: boolean;
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
    none: { icon: AlertTriangle, color: 'text-warning', surface: 'border-warning/30 bg-warning/5' },
    pending: { icon: Clock3, color: 'text-warning', surface: 'border-warning/30 bg-warning/5' },
    invalid: { icon: ShieldAlert, color: 'text-danger', surface: 'border-danger/30 bg-danger/5' },
    untrusted: { icon: ShieldAlert, color: 'text-danger', surface: 'border-danger/30 bg-danger/5' },
    trustUnknown: { icon: ShieldAlert, color: 'text-warning', surface: 'border-warning/30 bg-warning/5' },
    expired: { icon: ShieldAlert, color: 'text-danger', surface: 'border-danger/30 bg-danger/5' },
    inactive: { icon: ShieldAlert, color: 'text-warning', surface: 'border-warning/30 bg-warning/5' },
    incomplete: { icon: ShieldAlert, color: 'text-warning', surface: 'border-warning/30 bg-warning/5' },
    expiring: { icon: Clock3, color: 'text-warning', surface: 'border-warning/30 bg-warning/5' },
    dependentsPending: { icon: Clock3, color: 'text-warning', surface: 'border-warning/30 bg-warning/5' },
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
    const { t } = useI18n();
    const [data, setData] = useState<OverviewSSLData | null>(null);
    const [loading, setLoading] = useState(true);
    const [failed, setFailed] = useState(false);

    useEffect(() => {
        const controller = new AbortController();
        setLoading(true);
        setFailed(false);

        fetch(`/api/v1/domains/${domainId}/ssl`, {
            cache: 'no-store',
            signal: controller.signal,
        })
            .then((response) => {
                if (!response.ok) throw new Error();
                return response.json();
            })
            .then((result: OverviewSSLData) => {
                setData(result);
                onCertificateChange?.({
                    activated: result.certificate?.activated === true,
                    usable: result.certificate?.usable === true,
                });
            })
            .catch((error: unknown) => {
                if (error instanceof DOMException && error.name === 'AbortError') return;
                setData(null);
                setFailed(true);
            })
            .finally(() => {
                if (!controller.signal.aborted) setLoading(false);
            });

        return () => controller.abort();
    }, [domainId, onCertificateChange]);

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
                surface: 'border-warning/30 bg-warning/5',
                label: 'domain.overview.ssl.unavailable',
            }
          : {
                ...sslTierPresentation[certificateTier],
                label: sslTierLabel[certificateTier],
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
                    <p className="mt-1 text-xs leading-relaxed text-fg-muted">{detail}</p>
                </div>
            </div>
            <Button
                type="button"
                variant={!loading && !failed && (!hasCertificate || !cert?.usable) ? 'primary' : 'secondary'}
                icon={ArrowRight}
                onClick={onOpen}
                className="shrink-0 self-start sm:self-center"
            >
                {t('domain.overview.ssl.open')}
            </Button>
        </section>
    );
}
