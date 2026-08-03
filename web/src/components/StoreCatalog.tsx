import { useMemo, useState } from 'react';
import {
    Activity,
    Archive,
    ArrowRight,
    Ban,
    Briefcase,
    Check,
    Clock3,
    Cloud,
    Container,
    ExternalLink,
    GitBranch,
    Key,
    Loader2,
    Lock,
    Mail,
    MailCheck,
    Network,
    Package,
    PackageOpen,
    RefreshCw,
    Search,
    Shield,
    ShieldAlert,
    ShieldCheck,
    Sparkles,
    type LucideIcon,
} from 'lucide-react';
import { Link } from '../router';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Button, Card, inputClass } from './ui';

export type StoreItemState = 'available' | 'included' | 'coming_soon' | 'unsupported';
export type StoreEntitlementState = 'included' | 'owned' | 'not_owned' | 'suspended' | 'expired';
export type StoreItemAction =
    | 'acquire'
    | 'remove'
    | 'open_components'
    | 'manage_components'
    | 'open_domain_apps'
    | 'none';

export interface StoreItemView {
    id: string;
    name: string;
    description: string;
    category: string;
    vendor: string;
    icon: string;
    tags: string[];
    entitlementState: StoreEntitlementState;
    state: StoreItemState;
    stateReason?: string;
    action: StoreItemAction;
    actionPath?: string;
}

export interface StoreSubscriptionView {
    id: number;
    name: string;
    owner: string;
}

interface StoreCatalogProps {
    items: StoreItemView[];
    subscription: StoreSubscriptionView;
    busyItemIDs: ReadonlySet<string>;
    onAction: (item: StoreItemView) => void;
}

type StateFilter = 'all' | StoreItemState;

type StoreTranslate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

const STORE_ICONS: Readonly<Record<string, LucideIcon>> = {
    activity: Activity,
    archive: Archive,
    ban: Ban,
    briefcase: Briefcase,
    cloud: Cloud,
    container: Container,
    'git-branch': GitBranch,
    key: Key,
    lock: Lock,
    mail: Mail,
    'mail-check': MailCheck,
    network: Network,
    'refresh-cw': RefreshCw,
    shield: Shield,
    'shield-alert': ShieldAlert,
    'shield-check': ShieldCheck,
    sparkles: Sparkles,
    wordpress: Package,
};

const STORE_CATEGORY_KEYS: Readonly<Partial<Record<string, TranslationKey>>> = {
    applications: 'addons.category.applications',
    automation: 'addons.category.automation',
    backup: 'addons.category.backup',
    containers: 'addons.category.containers',
    development: 'addons.category.development',
    dns: 'addons.category.dns',
    email: 'addons.category.email',
    monitoring: 'addons.category.monitoring',
    network: 'addons.category.network',
    security: 'addons.category.security',
};

export function StoreCatalog({ items, subscription, busyItemIDs, onAction }: StoreCatalogProps) {
    const { t, locale } = useI18n();
    const [query, setQuery] = useState('');
    const [category, setCategory] = useState('all');
    const [state, setState] = useState<StateFilter>('all');

    const categories = useMemo(
        () => Array.from(new Set(items.map((item) => item.category).filter(Boolean)))
            .sort((left, right) => categoryLabel(left, t)
                .localeCompare(categoryLabel(right, t), locale)),
        [items, locale, t],
    );
    const filteredItems = useMemo(() => {
        const normalizedQuery = query.trim().toLocaleLowerCase(locale === 'tr' ? 'tr-TR' : 'en-US');
        return items.filter((item) => {
            const haystack = [
                item.name,
                item.description,
                item.category,
                categoryLabel(item.category, t),
                item.vendor,
                ...item.tags,
            ].join(' ')
                .toLocaleLowerCase(locale === 'tr' ? 'tr-TR' : 'en-US');
            return (!normalizedQuery || haystack.includes(normalizedQuery)) &&
                (category === 'all' || item.category === category) &&
                (state === 'all' || item.state === state);
        });
    }, [category, items, locale, query, state, t]);
    const hasFilters = query !== '' || category !== 'all' || state !== 'all';

    const clearFilters = () => {
        setQuery('');
        setCategory('all');
        setState('all');
    };

    return (
        <div className="space-y-4">
            <Card>
                <div className="flex flex-col gap-4 p-4 sm:p-5 lg:flex-row lg:items-center lg:justify-between">
                    <div className="min-w-0">
                        <div className="flex items-center gap-2 text-sm font-semibold text-fg">
                            <ShieldCheck className="h-4 w-4 text-primary" />
                            <span>{t('addons.subscriptionContext')}</span>
                        </div>
                        <p className="mt-1 truncate text-base font-semibold text-fg">{subscription.name}</p>
                        <p className="truncate text-xs text-fg-muted">{subscription.owner}</p>
                    </div>
                    <p className="max-w-2xl text-sm text-fg-muted">{t('addons.boundaryHint')}</p>
                </div>
            </Card>

            <Card>
                <div className="border-b border-border p-4 sm:p-5">
                    <div className="flex flex-col gap-3 xl:flex-row xl:items-end xl:justify-between">
                        <div>
                            <h2 className="text-base font-semibold text-fg">{t('addons.catalogTitle')}</h2>
                            <p className="mt-0.5 text-xs text-fg-muted">
                                {t('addons.resultCount', { shown: filteredItems.length, total: items.length })}
                            </p>
                        </div>
                        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-[minmax(240px,1fr)_180px_180px]">
                            <label className="relative sm:col-span-2 xl:col-span-1">
                                <span className="sr-only">{t('addons.search')}</span>
                                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-subtle" />
                                <input
                                    type="search"
                                    value={query}
                                    onChange={(event) => setQuery(event.target.value)}
                                    placeholder={t('addons.searchPlaceholder')}
                                    className={`${inputClass} pl-9`}
                                />
                            </label>
                            <label>
                                <span className="sr-only">{t('addons.category')}</span>
                                <select value={category} onChange={(event) => setCategory(event.target.value)} className={inputClass}>
                                    <option value="all">{t('addons.allCategories')}</option>
                                    {categories.map((itemCategory) => (
                                        <option key={itemCategory} value={itemCategory}>{categoryLabel(itemCategory, t)}</option>
                                    ))}
                                </select>
                            </label>
                            <label>
                                <span className="sr-only">{t('addons.status')}</span>
                                <select value={state} onChange={(event) => setState(event.target.value as StateFilter)} className={inputClass}>
                                    <option value="all">{t('addons.allStatuses')}</option>
                                    <option value="available">{t('addons.state.available')}</option>
                                    <option value="included">{t('addons.state.included')}</option>
                                    <option value="coming_soon">{t('addons.state.comingSoon')}</option>
                                    <option value="unsupported">{t('addons.state.unsupported')}</option>
                                </select>
                            </label>
                        </div>
                    </div>
                </div>

                {filteredItems.length === 0 ? (
                    <div className="flex flex-col items-center px-6 py-14 text-center">
                        <PackageOpen className="h-10 w-10 text-fg-subtle" />
                        <h3 className="mt-3 text-base font-semibold text-fg">
                            {hasFilters ? t('addons.noResults') : t('addons.empty')}
                        </h3>
                        <p className="mt-1 max-w-md text-sm text-fg-muted">
                            {hasFilters ? t('addons.noResultsHint') : t('addons.emptyHint')}
                        </p>
                        {hasFilters && (
                            <Button className="mt-4" onClick={clearFilters}>{t('addons.clearFilters')}</Button>
                        )}
                    </div>
                ) : (
                    <div className="grid grid-cols-1 gap-3 p-4 sm:p-5 md:grid-cols-2 2xl:grid-cols-3">
                        {filteredItems.map((item) => (
                            <StoreItemCard
                                key={item.id}
                                item={item}
                                busy={busyItemIDs.has(item.id)}
                                onAction={() => onAction(item)}
                            />
                        ))}
                    </div>
                )}
            </Card>
        </div>
    );
}

function StoreItemCard({ item, busy, onAction }: { item: StoreItemView; busy: boolean; onAction: () => void }) {
    const { t } = useI18n();
    const visual = stateVisual(item.state);
    const StateIcon = visual.icon;
    const ProductIcon = storeProductIcon(item.icon);
    const entitlement = entitlementVisual(item.entitlementState);
    const EntitlementIcon = entitlement?.icon;
    const linkAction = item.action === 'open_components' ||
        item.action === 'manage_components' ||
        item.action === 'open_domain_apps';

    return (
        <article className="flex min-h-64 flex-col rounded-xl border border-border bg-surface-2/40 p-4 transition-shadow hover:shadow-card">
            <div className="flex items-start justify-between gap-3">
                <span className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-xl ${visual.iconClass}`}>
                    <ProductIcon className="h-5 w-5" />
                </span>
                <span className={`inline-flex items-center gap-1 rounded-full border px-2 py-1 text-[11px] font-semibold ${visual.badgeClass}`}>
                    <StateIcon className="h-3.5 w-3.5" />
                    {t(visual.labelKey)}
                </span>
            </div>
            <div className="mt-4 min-w-0 flex-1">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-fg-subtle">
                    {categoryLabel(item.category, t)}
                </p>
                <h3 className="mt-1 text-base font-semibold text-fg">{item.name}</h3>
                {item.vendor && (
                    <p className="mt-0.5 text-xs text-fg-subtle">{t('addons.vendor', { vendor: item.vendor })}</p>
                )}
                {entitlement && EntitlementIcon && (
                    <span className={`mt-2 inline-flex items-center gap-1 rounded-full border px-2 py-1 text-[11px] font-semibold ${entitlement.badgeClass}`}>
                        <EntitlementIcon className="h-3.5 w-3.5" />
                        {t(entitlement.labelKey)}
                    </span>
                )}
                <p className="mt-1.5 line-clamp-3 text-sm leading-5 text-fg-muted">{item.description}</p>
                {item.stateReason && (
                    <p className="mt-3 rounded-lg border border-border bg-surface px-2.5 py-2 text-xs leading-5 text-fg-muted">
                        {item.stateReason}
                    </p>
                )}
            </div>
            <div className="mt-4 border-t border-border pt-3">
                {linkAction && item.actionPath ? (
                    <Link
                        to={item.actionPath}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-sm font-medium text-fg transition-colors hover:bg-surface-2"
                    >
                        {t(actionLabelKey(item.action))}
                        {item.action === 'open_domain_apps'
                            ? <ExternalLink className="h-4 w-4" />
                            : <ArrowRight className="h-4 w-4" />}
                    </Link>
                ) : item.action === 'acquire' || item.action === 'remove' ? (
                    <Button
                        variant={item.action === 'acquire' ? 'primary' : 'danger'}
                        disabled={busy}
                        icon={busy ? Loader2 : item.action === 'acquire' ? Check : Ban}
                        className={busy ? '[&_svg]:animate-spin' : ''}
                        onClick={onAction}
                    >
                        {busy ? t(item.action === 'acquire' ? 'addons.action.granting' : 'addons.action.removing') : t(actionLabelKey(item.action))}
                    </Button>
                ) : (
                    <Button disabled>{t(disabledActionLabelKey(item.state))}</Button>
                )}
            </div>
        </article>
    );
}

function stateVisual(state: StoreItemState) {
    switch (state) {
    case 'included':
        return {
            icon: Check,
            labelKey: 'addons.state.included' as const,
            iconClass: 'bg-success/10 text-success',
            badgeClass: 'border-success/30 bg-success/10 text-success',
        };
    case 'coming_soon':
        return {
            icon: Clock3,
            labelKey: 'addons.state.comingSoon' as const,
            iconClass: 'bg-warning/10 text-warning',
            badgeClass: 'border-warning/30 bg-warning/10 text-warning',
        };
    case 'unsupported':
        return {
            icon: Ban,
            labelKey: 'addons.state.unsupported' as const,
            iconClass: 'bg-surface-2 text-fg-subtle',
            badgeClass: 'border-border bg-surface text-fg-muted',
        };
    default:
        return {
            icon: PackageOpen,
            labelKey: 'addons.state.available' as const,
            iconClass: 'bg-primary/10 text-primary',
            badgeClass: 'border-primary/30 bg-primary/10 text-primary',
        };
    }
}

function storeProductIcon(value: string): LucideIcon {
    const normalized = value.trim().toLocaleLowerCase('en-US');
    return STORE_ICONS[normalized] ?? Package;
}

function categoryLabel(value: string, t: StoreTranslate): string {
    const key = STORE_CATEGORY_KEYS[value];
    return key ? t(key) : value;
}

function entitlementVisual(state: StoreEntitlementState) {
    switch (state) {
    case 'owned':
        return {
            icon: Check,
            labelKey: 'addons.entitlement.owned' as const,
            badgeClass: 'border-success/30 bg-success/10 text-success',
        };
    case 'expired':
        return {
            icon: Clock3,
            labelKey: 'addons.entitlement.expired' as const,
            badgeClass: 'border-warning/30 bg-warning/10 text-warning',
        };
    case 'suspended':
        return {
            icon: Ban,
            labelKey: 'addons.entitlement.suspended' as const,
            badgeClass: 'border-warning/30 bg-warning/10 text-warning',
        };
    default:
        return null;
    }
}

function actionLabelKey(action: StoreItemAction) {
    switch (action) {
    case 'acquire': return 'addons.action.grant' as const;
    case 'remove': return 'addons.action.remove' as const;
    case 'manage_components': return 'addons.action.manageComponents' as const;
    case 'open_domain_apps': return 'addons.action.openDomainApps' as const;
    default: return 'addons.action.openComponents' as const;
    }
}

function disabledActionLabelKey(state: StoreItemState) {
    if (state === 'coming_soon') return 'addons.action.comingSoon' as const;
    if (state === 'unsupported') return 'addons.action.unsupported' as const;
    if (state === 'included') return 'addons.action.included' as const;
    return 'addons.action.unavailable' as const;
}
