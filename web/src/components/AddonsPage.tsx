import { useCallback, useEffect, useRef, useState } from 'react';
import { Loader2, Package, RefreshCw } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { apiErrorText, readApiError, type ApiError } from '../lib/apiError';
import { Button, Card, EmptyState, ErrorBanner, PageHeader, inputClass } from './ui';
import {
    StoreCatalog,
    type StoreEntitlementState,
    type StoreItemAction,
    type StoreItemState,
    type StoreItemView,
    type StoreSubscriptionView,
} from './StoreCatalog';

interface StorePrimaryAction {
    type?: string;
    path?: string;
    enabled?: boolean;
}

interface StoreMetadata {
    icon?: unknown;
    tags?: unknown;
}

interface StoreAPIItem {
    id?: string;
    category?: string;
    vendor?: string;
    name?: string;
    description?: string;
    state?: string;
    state_reason?: string;
    blocker_reason?: string;
    entitlement_state?: string;
    action?: string;
    action_path?: string;
    manage_path?: string;
    metadata?: StoreMetadata;
    primary_action?: StorePrimaryAction;
}

interface SubscriptionAPI {
    id?: number;
    name?: string;
    owner?: string;
}

const STORE_STATES = new Set<StoreItemState>(['available', 'included', 'coming_soon', 'unsupported']);
const STORE_ENTITLEMENT_STATES = new Set<StoreEntitlementState>([
    'included',
    'owned',
    'not_owned',
    'suspended',
    'expired',
]);
const STORE_ACTIONS = new Set<StoreItemAction>([
    'acquire',
    'remove',
    'open_components',
    'manage_components',
    'open_domain_apps',
    'none',
]);

export function AddonsPage() {
    const { t, locale } = useI18n();
    const [subscriptions, setSubscriptions] = useState<StoreSubscriptionView[]>([]);
    const [selected, setSelected] = useState<number | null>(null);
    const [items, setItems] = useState<StoreItemView[]>([]);
    const [loadedSubscriptionID, setLoadedSubscriptionID] = useState<number | null>(null);
    const [busyItemIDs, setBusyItemIDs] = useState<Set<string>>(new Set());
    const [loadingSubscriptions, setLoadingSubscriptions] = useState(true);
    const [loadingStore, setLoadingStore] = useState(false);
    const [subscriptionsError, setSubscriptionsError] = useState<ApiError | null>(null);
    const [storeError, setStoreError] = useState<ApiError | null>(null);
    const storeRequestSequence = useRef(0);
    const selectedRef = useRef<number | null>(selected);
    selectedRef.current = selected;

    const loadSubscriptions = useCallback(async () => {
        setLoadingSubscriptions(true);
        setSubscriptionsError(null);
        try {
            const response = await fetch('/api/v1/subscriptions', { cache: 'no-store' });
            if (!response.ok) throw await readApiError(response);
            const payload = (await response.json()) as { subscriptions?: SubscriptionAPI[] };
            const list = normalizeSubscriptions(payload.subscriptions);
            setSubscriptions(list);
            setSelected((current) => list.some((subscription) => subscription.id === current)
                ? current
                : list[0]?.id ?? null);
        } catch (cause) {
            const error = normalizeError(cause, t('addons.loadFailed'));
            setSubscriptions([]);
            setSelected(null);
            setSubscriptionsError(error);
            showToast('error', apiErrorText(error, t, 'addons.loadFailed'));
        } finally {
            setLoadingSubscriptions(false);
        }
    }, [t]);

    const loadStore = useCallback(async (
        subscriptionID: number,
        signal?: AbortSignal,
    ) => {
        const requestSequence = ++storeRequestSequence.current;
        setLoadingStore(true);
        setStoreError(null);
        setItems([]);
        setLoadedSubscriptionID(null);
        try {
            const query = new URLSearchParams({
                subscription_id: String(subscriptionID),
                locale: locale === 'tr' ? 'tr' : 'en',
            });
            const response = await fetch(`/api/v1/store?${query.toString()}`, { cache: 'no-store', signal });
            if (!response.ok) throw await readApiError(response);
            const payload = (await response.json()) as { items?: StoreAPIItem[] };
            if (signal?.aborted || requestSequence !== storeRequestSequence.current) return;
            setItems(normalizeStoreItems(payload.items, t('addons.uncategorized')));
            setLoadedSubscriptionID(subscriptionID);
        } catch (cause) {
            if (signal?.aborted || requestSequence !== storeRequestSequence.current) return;
            const error = normalizeError(cause, t('addons.loadFailed'));
            setItems([]);
            setLoadedSubscriptionID(null);
            setStoreError(error);
            showToast('error', apiErrorText(error, t, 'addons.loadFailed'));
        } finally {
            if (!signal?.aborted && requestSequence === storeRequestSequence.current) {
                setLoadingStore(false);
            }
        }
    }, [locale, t]);

    useEffect(() => { void loadSubscriptions(); }, [loadSubscriptions]);

    useEffect(() => {
        if (selected == null) {
            storeRequestSequence.current++;
            setItems([]);
            setLoadedSubscriptionID(null);
            setLoadingStore(false);
            return;
        }
        const controller = new AbortController();
        void loadStore(selected, controller.signal);
        return () => controller.abort();
    }, [loadStore, selected]);

    const runStoreAction = async (item: StoreItemView) => {
        const targetSubscriptionID = loadedSubscriptionID;
        if (targetSubscriptionID == null || selected !== targetSubscriptionID || busyItemIDs.has(item.id)) return;
        if (item.action !== 'acquire' && item.action !== 'remove') return;
        if (item.action === 'remove' && !window.confirm(t('addons.confirmRemove', { name: item.name }))) return;

        setBusyItemIDs((current) => new Set(current).add(item.id));
        setStoreError(null);
        try {
            const basePath = `/api/v1/subscriptions/${targetSubscriptionID}/entitlements`;
            const response = item.action === 'acquire'
                ? await fetch(basePath, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ product_id: item.id }),
                })
                : await fetch(`${basePath}/${encodeURIComponent(item.id)}`, { method: 'DELETE' });
            if (!response.ok) throw await readApiError(response);
            showToast('success', t(item.action === 'acquire' ? 'addons.granted' : 'addons.revoked'));
            if (selectedRef.current === targetSubscriptionID) {
                await loadStore(targetSubscriptionID);
            }
        } catch (cause) {
            const error = normalizeError(cause, t('common.error'));
            setStoreError(error);
            showToast('error', apiErrorText(error, t));
        } finally {
            setBusyItemIDs((current) => {
                const next = new Set(current);
                next.delete(item.id);
                return next;
            });
        }
    };

    const selectedSubscription = subscriptions.find((subscription) => subscription.id === selected) ?? null;

    return (
        <div className="p-4 sm:p-6 md:p-8">
            <PageHeader
                title={t('addons.title')}
                subtitle={t('addons.subtitle')}
                breadcrumb={[t('common.home'), t('addons.title')]}
                actions={selected != null ? (
                    <Button
                        icon={RefreshCw}
                        disabled={loadingStore || busyItemIDs.size > 0}
                        onClick={() => void loadStore(selected)}
                    >
                        {t('addons.refresh')}
                    </Button>
                ) : undefined}
            />

            {loadingSubscriptions ? (
                <StoreLoading />
            ) : subscriptionsError ? (
                <div className="space-y-4">
                    <ErrorBanner error={subscriptionsError} />
                    <EmptyState
                        icon={Package}
                        title={t('addons.loadFailed')}
                        action={<Button icon={RefreshCw} onClick={() => void loadSubscriptions()}>{t('addons.retry')}</Button>}
                    />
                </div>
            ) : subscriptions.length === 0 ? (
                <EmptyState icon={Package} title={t('addons.noSubs')} hint={t('addons.noSubsHint')} />
            ) : (
                <div className="space-y-4">
                    <Card>
                        <div className="flex flex-col gap-2 p-4 sm:flex-row sm:items-center sm:justify-between sm:p-5">
                            <label htmlFor="store-subscription" className="text-sm font-semibold text-fg">
                                {t('addons.subscription')}
                            </label>
                            <select
                                id="store-subscription"
                                value={selected ?? ''}
                                disabled={busyItemIDs.size > 0}
                                onChange={(event) => setSelected(Number(event.target.value))}
                                className={`${inputClass} sm:max-w-md`}
                            >
                                {subscriptions.map((subscription) => (
                                    <option key={subscription.id} value={subscription.id}>
                                        {subscription.name} — {subscription.owner}
                                    </option>
                                ))}
                            </select>
                        </div>
                    </Card>

                    <ErrorBanner error={storeError} />

                    {storeError && selectedSubscription ? (
                        <EmptyState
                            icon={Package}
                            title={t('addons.loadFailed')}
                            action={<Button icon={RefreshCw} onClick={() => void loadStore(selectedSubscription.id)}>{t('addons.retry')}</Button>}
                        />
                    ) : loadingStore || !selectedSubscription || loadedSubscriptionID !== selectedSubscription.id ? (
                        <StoreLoading />
                    ) : (
                        <StoreCatalog
                            items={items}
                            subscription={selectedSubscription}
                            busyItemIDs={busyItemIDs}
                            onAction={(item) => void runStoreAction(item)}
                        />
                    )}
                </div>
            )}
        </div>
    );
}

function StoreLoading() {
    const { t } = useI18n();
    return (
        <Card>
            <div className="flex min-h-64 flex-col items-center justify-center px-6 py-14 text-center">
                <Loader2 className="h-9 w-9 animate-spin text-primary" />
                <h2 className="mt-4 text-base font-semibold text-fg">{t('addons.loadingTitle')}</h2>
                <p className="mt-1 text-sm text-fg-muted">{t('addons.loadingHint')}</p>
            </div>
        </Card>
    );
}

function normalizeSubscriptions(values: SubscriptionAPI[] | undefined): StoreSubscriptionView[] {
    if (!Array.isArray(values)) return [];
    const seen = new Set<number>();
    return values.flatMap((value) => {
        if (!Number.isSafeInteger(value.id) || Number(value.id) <= 0 || seen.has(Number(value.id))) return [];
        seen.add(Number(value.id));
        return [{
            id: Number(value.id),
            name: value.name?.trim() || `#${value.id}`,
            owner: value.owner?.trim() || '',
        }];
    });
}

function normalizeStoreItems(values: StoreAPIItem[] | undefined, uncategorized: string): StoreItemView[] {
    if (!Array.isArray(values)) return [];
    const seen = new Set<string>();
    return values.flatMap((value) => {
        const id = value.id?.trim() || '';
        if (!id || seen.has(id)) return [];
        seen.add(id);

        const state = STORE_STATES.has(value.state as StoreItemState)
            ? value.state as StoreItemState
            : 'unsupported';
        const entitlementState = STORE_ENTITLEMENT_STATES.has(value.entitlement_state as StoreEntitlementState)
            ? value.entitlement_state as StoreEntitlementState
            : 'suspended';
        const rawAction = value.primary_action?.type || 'none';
        const action = STORE_ACTIONS.has(rawAction as StoreItemAction)
            ? rawAction as StoreItemAction
            : 'none';
        const actionEnabled = value.primary_action?.enabled === true;
        const linkAction = action === 'open_components' ||
            action === 'manage_components' ||
            action === 'open_domain_apps';
        const actionPath = actionEnabled && linkAction
            ? safePanelPath(value.primary_action?.path)
            : undefined;
        const rawTags = value.metadata?.tags;
        const tags = Array.isArray(rawTags)
            ? rawTags.flatMap((tag) => typeof tag === 'string' && tag.trim() ? [tag.trim()] : [])
            : [];

        return [{
            id,
            name: value.name?.trim() || id,
            description: value.description?.trim() || '',
            category: value.category?.trim() || uncategorized,
            vendor: typeof value.vendor === 'string' ? value.vendor.trim() : '',
            icon: typeof value.metadata?.icon === 'string' ? value.metadata.icon.trim() : '',
            tags,
            entitlementState,
            state,
            stateReason: value.state_reason?.trim() || value.blocker_reason?.trim() || undefined,
            action: actionEnabled ? action : 'none',
            actionPath,
        }];
    });
}

function safePanelPath(value: string | undefined): string | undefined {
    if (!value || !value.startsWith('/') || value.startsWith('//') || /[\\\u0000-\u001f\u007f]/.test(value)) {
        return undefined;
    }

    const rawPath = value.split(/[?#]/, 1)[0];
    try {
        if (decodeURIComponent(value) !== value) return undefined;
        for (const segment of rawPath.split('/')) {
            const decoded = decodeURIComponent(segment);
            if (decoded === '.' || decoded === '..' || decoded.includes('/') || decoded.includes('\\') ||
                /[\u0000-\u001f\u007f]/.test(decoded)) {
                return undefined;
            }
        }
        const parsed = new URL(value, 'https://celikpanel.invalid');
        if (parsed.origin !== 'https://celikpanel.invalid') return undefined;
        return `${parsed.pathname}${parsed.search}${parsed.hash}`;
    } catch {
        return undefined;
    }
}

function normalizeError(cause: unknown, fallback: string): ApiError {
    if (cause && typeof cause === 'object' && 'message' in cause) return cause as ApiError;
    return { message: typeof cause === 'string' ? cause : fallback };
}
