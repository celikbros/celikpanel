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
import { Button } from './ui';

interface OverviewCertificate {
    issuer: string;
    expires_at: string;
    days_until_expiry: number;
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

export function DomainSSLOverviewCard({
    domainId,
    onOpen,
    onCertificateChange,
}: {
    domainId: number;
    onOpen: () => void;
    onCertificateChange?: (hasCertificate: boolean) => void;
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
                onCertificateChange?.(result.has_certificate);
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
          : !data?.has_certificate || !cert
            ? {
                  icon: AlertTriangle,
                  color: 'text-warning',
                  surface: 'border-warning/30 bg-warning/5',
                  label: 'ssl.status.none',
              }
            : cert.days_until_expiry < 0
              ? {
                    icon: ShieldAlert,
                    color: 'text-danger',
                    surface: 'border-danger/30 bg-danger/5',
                    label: 'ssl.status.expired',
                }
              : cert.days_until_expiry < 30
                ? {
                      icon: Clock3,
                      color: 'text-warning',
                      surface: 'border-warning/30 bg-warning/5',
                      label: 'ssl.status.expiring',
                  }
                : {
                      icon: CheckCircle,
                      color: 'text-success',
                      surface: 'border-success/30 bg-success/5',
                      label: 'ssl.status.valid',
                  };
    const TierIcon = tier.icon;
    const hasCertificate = Boolean(data?.has_certificate && cert);

    let detail = t('domain.overview.ssl.loading');
    if (!loading) {
        if (failed) {
            detail = t('domain.overview.ssl.unavailableHint');
        } else if (!hasCertificate) {
            detail = t('domain.overview.ssl.none');
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
                variant={!loading && !failed && !hasCertificate ? 'primary' : 'secondary'}
                icon={ArrowRight}
                onClick={onOpen}
                className="shrink-0 self-start sm:self-center"
            >
                {t('domain.overview.ssl.open')}
            </Button>
        </section>
    );
}
