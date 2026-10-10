import type { TranslationKey } from '../i18n/en'

export type SSLTier =
  | 'none'
  | 'pending'
  | 'waitingForOwner'
  | 'invalid'
  | 'untrusted'
  | 'trustUnknown'
  | 'expired'
  | 'inactive'
  | 'incomplete'
  | 'expiring'
  | 'dependentsPending'
  | 'valid'

export interface SSLTierCertificate {
  activated: boolean
  usable: boolean
  trust_status: 'trusted' | 'untrusted' | 'unknown' | 'invalid'
  activation_pending: boolean
  dependents_pending: boolean
  // D-031 step 1b: the site's configuration file the owner kept does not
  // use this certificate yet, or stopped its renewal. Absent from older answers.
  waiting_for_owner?: boolean
  // Which of the two waits (additive): a new certificate the file does not
  // use yet, or a request or renewal the file stopped.
  waiting_for_owner_reason?: string
  days_until_expiry: number
}

export const sslTierLabel: Record<SSLTier, TranslationKey> = {
  none: 'ssl.status.none',
  pending: 'ssl.status.pending',
  waitingForOwner: 'ssl.status.waitingForOwner',
  invalid: 'ssl.status.invalid',
  untrusted: 'ssl.status.untrusted',
  trustUnknown: 'ssl.status.trustUnknown',
  expired: 'ssl.status.expired',
  inactive: 'ssl.status.inactive',
  incomplete: 'ssl.status.incomplete',
  expiring: 'ssl.status.expiring',
  dependentsPending: 'ssl.status.dependentsPending',
  valid: 'ssl.status.valid',
}

// Keep certificate severity identical everywhere it is presented. The order is
// deliberate: trust and expiry failures take precedence over activation and
// warning states; an unusable certificate is more urgent than an expiry warning.
// A certificate waiting for a choice on the site's configuration file comes
// after them: an expired, invalid or untrusted certificate is a verified
// failure of the one in use and is shown as such (D-025 invariant 2). A trust
// that could not be checked is not a verified failure; the waiting state, and
// an expiry known from the date, come before it.
export function sslTier(cert?: SSLTierCertificate | null): SSLTier {
  if (!cert) return 'none'
  if (cert.activation_pending) return 'pending'
  if (cert.trust_status === 'invalid') return 'invalid'
  if (cert.trust_status === 'untrusted') return 'untrusted'
  if (cert.days_until_expiry < 0) return 'expired'
  if (cert.waiting_for_owner) return 'waitingForOwner'
  if (cert.trust_status === 'unknown') return 'trustUnknown'
  if (!cert.activated) return 'inactive'
  if (!cert.usable) return 'incomplete'
  if (cert.days_until_expiry < 30) return 'expiring'
  if (cert.dependents_pending) return 'dependentsPending'
  return 'valid'
}

// The waiting state's label names which wait it is when the answer says so;
// an older answer keeps the general label.
export function sslTierLabelFor(tier: SSLTier, cert?: SSLTierCertificate | null): TranslationKey {
  if (tier === 'waitingForOwner') {
    if (cert?.waiting_for_owner_reason === 'certificate') return 'ssl.status.waitingForOwner.certificate'
    if (cert?.waiting_for_owner_reason === 'certificate_validation') return 'ssl.status.waitingForOwner.certificate_validation'
  }
  return sslTierLabel[tier]
}
