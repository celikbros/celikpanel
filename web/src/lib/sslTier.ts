import type { TranslationKey } from '../i18n/en'

export type SSLTier =
  | 'none'
  | 'pending'
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
  days_until_expiry: number
}

export const sslTierLabel: Record<SSLTier, TranslationKey> = {
  none: 'ssl.status.none',
  pending: 'ssl.status.pending',
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
export function sslTier(cert?: SSLTierCertificate | null): SSLTier {
  if (!cert) return 'none'
  if (cert.activation_pending) return 'pending'
  if (cert.trust_status === 'invalid') return 'invalid'
  if (cert.trust_status === 'untrusted') return 'untrusted'
  if (cert.trust_status === 'unknown') return 'trustUnknown'
  if (cert.days_until_expiry < 0) return 'expired'
  if (!cert.activated) return 'inactive'
  if (!cert.usable) return 'incomplete'
  if (cert.days_until_expiry < 30) return 'expiring'
  if (cert.dependents_pending) return 'dependentsPending'
  return 'valid'
}
