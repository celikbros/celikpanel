import { BadgeCheck } from 'lucide-react';
import { Link } from '../router';
import { useI18n } from '../i18n';
import { useRemote } from '../lib/remote';

const STATES = ['active', 'missing', 'invalid', 'expired', 'verification_unavailable', 'status_unavailable'] as const;
type LicenseState = (typeof STATES)[number];

interface LicenseStatus {
    state: LicenseState;
    can_provision: boolean;
}

const LICENSE_URL = '/api/v1/panel/license';
const NO_STORE: RequestInit = { cache: 'no-store' };

// The answer is the contract or it is unknown. In particular an answer without
// `can_provision`, or with a state this build does not know, is not "a license
// is required".
// Yanıt ya sözleşmedir ya da bilinmeyendir. `can_provision` taşımayan ya da bu
// sürümün tanımadığı bir durum bildiren yanıt "lisans gerekli" değildir.
function decodeLicenseStatus(raw: unknown): LicenseStatus {
    if (!raw || typeof raw !== 'object') throw new Error('shape');
    const { state, can_provision } = raw as { state?: unknown; can_provision?: unknown };
    if (!STATES.includes(state as LicenseState) || typeof can_provision !== 'boolean') throw new Error('field');
    if (can_provision && state !== 'active') throw new Error('contradiction');
    return { state: state as LicenseState, can_provision };
}

/**
 * Mounted only in the administrator dashboard. No tenant license request.
 *
 * It speaks only about an answer the server gave. While the license is being
 * read, and when that read failed, it draws nothing: the access gate around
 * every page reads access on its own and holds the page when access is not
 * known. And "the license could not be verified just now" is said as that, not
 * as "an active license is required" (before 9 Oct 2026 the two looked alike).
 *
 * Yalnız sunucunun verdiği yanıt hakkında konuşur. Lisans okunurken ve okuma
 * başarısız olduğunda hiçbir şey çizmez. "Lisans şu an doğrulanamadı" da öyle
 * söylenir; "aktif lisans gerekiyor" diye değil.
 */
export function LicenseNotice() {
    const { t } = useI18n();
    const license = useRemote(LICENSE_URL, decodeLicenseStatus, { init: NO_STORE });
    if (license.remote.state !== 'known' || license.remote.value.can_provision) return null;

    const { state } = license.remote.value;
    const unverified = state === 'verification_unavailable' || state === 'status_unavailable';
    const title = unverified
        ? t('recovery.licenseTitle')
        : state === 'active'
            ? t('license.title')
            : t(`license.state.${state}`);
    return (
        <section
            className="mb-5 flex flex-wrap items-center justify-between gap-4 rounded-lg border border-border bg-surface p-4"
            aria-label={t('license.title')}
        >
            <div className="flex min-w-0 items-start gap-3">
                <BadgeCheck className="mt-1 h-5 w-5 shrink-0 text-fg-muted" aria-hidden="true" />
                <div className="min-w-0">
                    <h2 className="font-semibold text-fg">{title}</h2>
                    <p className="max-w-[75ch] text-sm text-fg-muted">
                        {t(unverified ? 'license.noticeUnverified' : 'license.restricted')}
                    </p>
                </div>
            </div>
            <Link to="/settings?section=license" className="shrink-0 text-sm font-medium text-primary underline underline-offset-4">
                {t(unverified || state === 'expired' ? 'license.noticeOpen' : 'license.activate')}
            </Link>
        </section>
    );
}
