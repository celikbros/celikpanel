import { decodeListIn } from './remote';

// The subscriptions the signed-in account may see: GET /api/v1/subscriptions.
// One address, one decoder, for every screen that reads it. An answer without
// the list is not "no subscriptions"; it is not the contract.
//
// Oturum açan hesabın görebildiği abonelikler. Bu adresi okuyan her ekran için
// tek adres, tek çözücü. Listeyi taşımayan yanıt "abonelik yok" değildir.
export interface SubscriptionUsage {
    disk_used_bytes: number;
    disk_limit_bytes: number;
    domains: number;
    domains_limit: number;
    databases: number;
    databases_limit: number;
    mail_accounts: number;
    mail_limit: number;
}

export interface SubscriptionRow {
    id: number;
    name: string;
    owner: string;
    usage?: SubscriptionUsage;
}

export const SUBSCRIPTIONS_URL = '/api/v1/subscriptions';

export function decodeSubscriptions(raw: unknown): SubscriptionRow[] {
    return decodeListIn<SubscriptionRow>(raw, 'subscriptions');
}
