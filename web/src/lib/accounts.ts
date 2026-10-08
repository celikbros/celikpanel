import type { PanelUser, ServicePlan } from './api';
import { decodeListIn } from './remote';

// The accounts and the plans the signed-in account may see: GET /api/v1/users
// and GET /api/v1/plans. One address, one decoder each, for every screen that
// reads them. An answer that is not `{users: [...]}` / `{plans: [...]}` is not
// "no accounts" or "no plans"; it is unknown. `null` in place of the list is
// how the server writes a list with no rows.
//
// Oturum açan hesabın görebildiği hesaplar ve planlar. Okuyan her ekran için
// adres başına tek çözücü. `{users: [...]}` / `{plans: [...]}` olmayan yanıt
// "hesap yok" ya da "plan yok" değildir; bilinmeyendir.
export const USERS_URL = '/api/v1/users';
export const PLANS_URL = '/api/v1/plans';

export const decodeUsers = (raw: unknown) => decodeListIn<PanelUser>(raw, 'users');
export const decodePlans = (raw: unknown) => decodeListIn<ServicePlan>(raw, 'plans');
