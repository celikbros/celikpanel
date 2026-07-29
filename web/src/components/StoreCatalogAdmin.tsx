import { useCallback, useEffect, useMemo, useState } from 'react';
import {
    Database,
    Loader2,
    LockKeyhole,
    PackageSearch,
    RefreshCw,
    Save,
    ShieldCheck,
    AlertTriangle,
} from 'lucide-react';
import { useI18n } from '../i18n';
import { apiErrorText, readApiError, type ApiError } from '../lib/apiError';
import { showToast } from './Toast';
import { Button, Card, EmptyState, ErrorBanner, Field, inputClass } from './ui';

type ReleaseState = 'available' | 'coming_soon' | 'retired';

interface LocalizedText {
    en: string;
    tr: string;
}

interface CatalogMetadata {
    name: LocalizedText;
    description: LocalizedText;
    icon: string;
    tags: string[];
}

interface AdminOffering {
    id: string;
    kind: string;
    category: string;
    vendor: string;
    release_state: ReleaseState;
    entitlement_mode: string;
    manage_path?: string;
    metadata: CatalogMetadata;
    component_ids: string[];
    sort_order: number;
    updated_at: string;
    active_entitlements: number;
}

interface AdminComponent {
    id: string;
    name: string;
    description: string;
    category: string;
    kind: string;
    lifecycle_operations: string[];
    policy_source: 'release_managed';
    editable: false;
}

interface OperationPolicy {
    mode: 'read_only';
    management: 'release_managed';
    catalog_format: 'manifest_v2_signed_sqlite';
    verification: 'implemented';
    runtime_activation: 'pending';
    browser_editable: false;
    database_path_hint: string;
    detached_signature_hint: string;
}

interface CatalogResponse {
    offerings: AdminOffering[];
    components: AdminComponent[];
    operation_policy: OperationPolicy;
}

interface DraftOffering extends AdminOffering {
    tagsText: string;
}

const RELEASE_STATES = new Set<ReleaseState>(['available', 'coming_soon', 'retired']);

export function StoreCatalogAdmin({ onDirtyChange }: { onDirtyChange?: (dirty: boolean) => void }) {
    const { t, locale } = useI18n();
    const [catalog, setCatalog] = useState<CatalogResponse | null>(null);
    const [selectedID, setSelectedID] = useState('');
    const [draft, setDraft] = useState<DraftOffering | null>(null);
    const [query, setQuery] = useState('');
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<ApiError | null>(null);

    const loadCatalog = useCallback(async (preferredID?: string) => {
        setLoading(true);
        setError(null);
        try {
            const response = await fetch('/api/v1/admin/store-catalog', { cache: 'no-store' });
            if (!response.ok) throw await readApiError(response);
            const normalized = normalizeCatalog(await response.json());
            setCatalog(normalized);
            const nextID = normalized.offerings.some((item) => item.id === preferredID)
                ? preferredID!
                : normalized.offerings[0]?.id ?? '';
            setSelectedID(nextID);
            setDraft(toDraft(normalized.offerings.find((item) => item.id === nextID) ?? null));
        } catch (cause) {
            const nextError = normalizeAdminError(cause, t('addons.admin.loadFailed'));
            setCatalog(null);
            setDraft(null);
            setError(nextError);
            showToast('error', apiErrorText(nextError, t, 'addons.admin.loadFailed'));
        } finally {
            setLoading(false);
        }
    }, [t]);

    useEffect(() => { void loadCatalog(); }, [loadCatalog]);

    const original = catalog?.offerings.find((item) => item.id === selectedID) ?? null;
    const dirty = useMemo(() => original != null && draft != null &&
        JSON.stringify(toComparable(original)) !== JSON.stringify(toComparable(fromDraft(draft))), [draft, original]);
    useEffect(() => {
        onDirtyChange?.(dirty);
        return () => onDirtyChange?.(false);
    }, [dirty, onDirtyChange]);
    useEffect(() => {
        if (!dirty) return;
        const guard = (event: BeforeUnloadEvent) => {
            event.preventDefault();
            event.returnValue = '';
        };
        window.addEventListener('beforeunload', guard);
        return () => window.removeEventListener('beforeunload', guard);
    }, [dirty]);
    const filtered = useMemo(() => {
        if (!catalog) return [];
        const needle = query.trim().toLocaleLowerCase(locale === 'tr' ? 'tr-TR' : 'en-US');
        if (!needle) return catalog.offerings;
        return catalog.offerings.filter((item) => {
            const text = [item.id, item.vendor, item.category, item.metadata.name.en, item.metadata.name.tr]
                .join(' ').toLocaleLowerCase(locale === 'tr' ? 'tr-TR' : 'en-US');
            return text.includes(needle);
        });
    }, [catalog, locale, query]);

    const selectOffering = (id: string) => {
        if (id === selectedID) return;
        if (dirty && !window.confirm(t('addons.admin.discardConfirm'))) return;
        const offering = catalog?.offerings.find((item) => item.id === id) ?? null;
        setSelectedID(id);
        setDraft(toDraft(offering));
        setError(null);
    };

    const refresh = () => {
        if (dirty && !window.confirm(t('addons.admin.discardConfirm'))) return;
        void loadCatalog(draft?.id);
    };

    const save = async () => {
        if (!draft || !original || saving || !dirty) return;
        if (original.release_state === 'available' && draft.release_state !== 'available') {
            const warning = original.active_entitlements > 0
                ? t('addons.admin.lifecycleConfirmActive', { count: original.active_entitlements })
                : t('addons.admin.lifecycleConfirm');
            if (!window.confirm(warning)) return;
        }

        setSaving(true);
        setError(null);
        try {
            const payload = fromDraft(draft);
            const response = await fetch(`/api/v1/admin/store-catalog/${encodeURIComponent(draft.id)}`, {
                method: 'PATCH',
                cache: 'no-store',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    category: payload.category,
                    vendor: payload.vendor,
                    release_state: payload.release_state,
                    metadata: payload.metadata,
                    component_ids: payload.component_ids,
                    sort_order: payload.sort_order,
                    expected_updated_at: original.updated_at,
                    acknowledge_entitlement_impact: original.release_state === 'available' &&
                        payload.release_state !== 'available' && original.active_entitlements > 0,
                }),
            });
            if (!response.ok) {
                const responseError = await readApiError(response);
                if (response.status === 409) await loadCatalog(draft.id);
                throw responseError;
            }
            const body = (await response.json()) as { offering?: unknown; unchanged?: boolean };
            const saved = normalizeOffering(body.offering);
            if (!saved) throw new Error(t('addons.admin.invalidResponse'));
            setCatalog((current) => current ? {
                ...current,
                offerings: current.offerings.map((item) => item.id === saved.id ? saved : item),
            } : current);
            setDraft(toDraft(saved));
            showToast('success', t(body.unchanged ? 'addons.admin.noChanges' : 'addons.admin.saved'));
        } catch (cause) {
            const nextError = normalizeAdminError(cause, t('addons.admin.saveFailed'));
            setError(nextError);
            showToast('error', apiErrorText(nextError, t, 'addons.admin.saveFailed'));
        } finally {
            setSaving(false);
        }
    };

    if (loading) {
        return (
            <Card>
                <div className="flex min-h-64 flex-col items-center justify-center px-6 py-14 text-center" role="status" aria-live="polite">
                    <Loader2 className="h-9 w-9 animate-spin text-primary" />
                    <h2 className="mt-4 text-base font-semibold text-fg">{t('addons.admin.loading')}</h2>
                </div>
            </Card>
        );
    }
    if (!catalog) {
        return (
            <div className="space-y-4">
                <ErrorBanner error={error} />
                <EmptyState
                    icon={PackageSearch}
                    title={t('addons.admin.loadFailed')}
                    action={<Button icon={RefreshCw} onClick={() => void loadCatalog()}>{t('addons.retry')}</Button>}
                />
            </div>
        );
    }

    return (
        <div className="space-y-4">
            <Card>
                <div className="grid gap-4 p-4 md:grid-cols-2 md:p-5">
                    <BoundaryCallout
                        icon={Database}
                        title={t('addons.admin.sqliteTitle')}
                        text={t('addons.admin.sqliteHint')}
                    />
                    <BoundaryCallout
                        icon={LockKeyhole}
                        title={t('addons.admin.boundaryTitle')}
                        text={t('addons.admin.boundaryHint')}
                    />
                </div>
                <div className="flex gap-3 border-t border-border px-4 py-3 text-xs leading-5 text-fg-muted md:px-5">
                    <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                    <p>{t('addons.admin.releaseSeedHint')}</p>
                </div>
            </Card>

            <ErrorBanner error={error} />

            <div className="grid items-start gap-4 xl:grid-cols-[minmax(18rem,0.75fr)_minmax(0,1.55fr)]">
                <Card title={t('addons.admin.offerings')} icon={PackageSearch}>
                    <div className="border-b border-border p-4">
                        <label htmlFor="catalog-admin-search" className="sr-only">
                            {t('addons.admin.searchLabel')}
                        </label>
                        <input
                            id="catalog-admin-search"
                            type="search"
                            value={query}
                            onChange={(event) => setQuery(event.target.value)}
                            placeholder={t('addons.admin.search')}
                            className={inputClass}
                        />
                    </div>
                    <div className="max-h-[42rem] divide-y divide-border overflow-y-auto">
                        {filtered.map((item) => (
                            <button
                                type="button"
                                key={item.id}
                                aria-pressed={selectedID === item.id}
                                onClick={() => selectOffering(item.id)}
                                className={`w-full px-4 py-3 text-left transition-colors hover:bg-surface-2 ${selectedID === item.id ? 'bg-primary/10' : ''}`}
                            >
                                <div className="flex items-start justify-between gap-3">
                                    <div className="min-w-0">
                                        <div className="truncate text-sm font-semibold text-fg">
                                            {locale === 'tr' ? item.metadata.name.tr : item.metadata.name.en}
                                        </div>
                                        <div className="mt-0.5 truncate font-mono text-xs text-fg-subtle">{item.id}</div>
                                    </div>
                                    <ReleaseBadge state={item.release_state} />
                                </div>
                                <div className="mt-2 flex flex-wrap gap-2 text-xs text-fg-muted">
                                    <span>{item.category}</span>
                                    <span>•</span>
                                    <span>{t('addons.admin.activeRights', { count: item.active_entitlements })}</span>
                                </div>
                            </button>
                        ))}
                        {filtered.length === 0 && (
                            <p className="px-4 py-8 text-center text-sm text-fg-muted">{t('addons.noResults')}</p>
                        )}
                    </div>
                </Card>

                {draft && original ? (
                    <Card
                        title={t('addons.admin.editOffering')}
                        icon={ShieldCheck}
                        action={<Button icon={RefreshCw} disabled={saving} onClick={refresh}>{t('addons.refresh')}</Button>}
                    >
                        <fieldset disabled={saving} className="space-y-5 p-4 md:p-5">
                            <div className="grid gap-3 rounded-lg border border-border bg-surface-2 p-3 sm:grid-cols-2 lg:grid-cols-4">
                                <ReadOnlyValue label={t('addons.admin.id')} value={draft.id} mono />
                                <ReadOnlyValue label={t('addons.admin.kind')} value={draft.kind} />
                                <ReadOnlyValue label={t('addons.admin.entitlementMode')} value={draft.entitlement_mode} />
                                <ReadOnlyValue label={t('addons.admin.managePath')} value={draft.manage_path || '—'} mono />
                            </div>

                            <div className="grid gap-4 md:grid-cols-2">
                                <Field label={t('addons.category')} htmlFor="catalog-category">
                                    <input id="catalog-category" value={draft.category} onChange={(event) => setDraft({ ...draft, category: event.target.value })} className={inputClass} />
                                </Field>
                                <Field label={t('addons.admin.vendor')} htmlFor="catalog-vendor">
                                    <input id="catalog-vendor" value={draft.vendor} onChange={(event) => setDraft({ ...draft, vendor: event.target.value })} className={inputClass} />
                                </Field>
                                <Field label={t('addons.status')} hint={original.release_state !== 'available' ? t('addons.admin.publishGuard') : undefined} htmlFor="catalog-state">
                                    <select id="catalog-state" value={draft.release_state} onChange={(event) => setDraft({ ...draft, release_state: event.target.value as ReleaseState })} className={inputClass}>
                                        {original.release_state === 'available' && <option value="available">{t('addons.state.available')}</option>}
                                        <option value="coming_soon">{t('addons.state.comingSoon')}</option>
                                        <option value="retired">{t('addons.admin.stateRetired')}</option>
                                    </select>
                                </Field>
                                <Field label={t('addons.admin.sortOrder')} htmlFor="catalog-sort">
                                    <input id="catalog-sort" type="number" min={0} max={1000000} value={draft.sort_order} onChange={(event) => setDraft({ ...draft, sort_order: Number(event.target.value) })} className={inputClass} />
                                </Field>
                            </div>

                            <div className="grid gap-4 md:grid-cols-2">
                                <LocalizedFields
                                    localeLabel="EN"
                                    name={draft.metadata.name.en}
                                    description={draft.metadata.description.en}
                                    onName={(value) => setDraft({ ...draft, metadata: { ...draft.metadata, name: { ...draft.metadata.name, en: value } } })}
                                    onDescription={(value) => setDraft({ ...draft, metadata: { ...draft.metadata, description: { ...draft.metadata.description, en: value } } })}
                                />
                                <LocalizedFields
                                    localeLabel="TR"
                                    name={draft.metadata.name.tr}
                                    description={draft.metadata.description.tr}
                                    onName={(value) => setDraft({ ...draft, metadata: { ...draft.metadata, name: { ...draft.metadata.name, tr: value } } })}
                                    onDescription={(value) => setDraft({ ...draft, metadata: { ...draft.metadata, description: { ...draft.metadata.description, tr: value } } })}
                                />
                            </div>

                            <div className="grid gap-4 md:grid-cols-2">
                                <Field label={t('addons.admin.icon')} htmlFor="catalog-icon">
                                    <input id="catalog-icon" value={draft.metadata.icon} onChange={(event) => setDraft({ ...draft, metadata: { ...draft.metadata, icon: event.target.value } })} className={inputClass} />
                                </Field>
                                <Field label={t('addons.admin.tags')} hint={t('addons.admin.tagsHint')} htmlFor="catalog-tags">
                                    <input id="catalog-tags" value={draft.tagsText} onChange={(event) => setDraft({ ...draft, tagsText: event.target.value })} className={inputClass} />
                                </Field>
                            </div>

                            <div>
                                <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                                    <div>
                                        <h3 className="text-sm font-semibold text-fg">{t('addons.admin.bindings')}</h3>
                                        <p className="text-xs text-fg-muted">{t('addons.admin.bindingsHint')}</p>
                                    </div>
                                    <span className="rounded-full bg-surface-2 px-2.5 py-1 text-xs text-fg-muted">
                                        {t('addons.admin.selectedCount', { count: draft.component_ids.length })}
                                    </span>
                                </div>
                                <div className="grid max-h-64 gap-2 overflow-y-auto rounded-lg border border-border p-3 sm:grid-cols-2">
                                    {catalog.components.map((component) => (
                                        <label key={component.id} className="flex cursor-pointer items-start gap-2 rounded-lg p-2 hover:bg-surface-2">
                                            <input
                                                type="checkbox"
                                                checked={draft.component_ids.includes(component.id)}
                                                onChange={(event) => setDraft({
                                                    ...draft,
                                                    component_ids: event.target.checked
                                                        ? [...draft.component_ids, component.id]
                                                        : draft.component_ids.filter((id) => id !== component.id),
                                                })}
                                                className="mt-0.5 h-4 w-4 accent-primary"
                                            />
                                            <span className="min-w-0">
                                                <span className="block truncate text-sm font-medium text-fg">{component.name}</span>
                                                <span className="block truncate font-mono text-xs text-fg-subtle">{component.id} · {component.kind}</span>
                                            </span>
                                        </label>
                                    ))}
                                </div>
                            </div>

                            {original.release_state === 'available' && draft.release_state !== 'available' && (
                                <div className="flex gap-3 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm text-fg">
                                    <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warning" />
                                    <div>
                                        <p className="font-semibold">{t('addons.admin.lifecycleImpactTitle')}</p>
                                        <p className="mt-0.5 text-fg-muted">{t('addons.admin.lifecycleImpact', { count: original.active_entitlements })}</p>
                                    </div>
                                </div>
                            )}

                            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
                                <p className="text-xs text-fg-subtle">{t('addons.admin.concurrencyHint')}</p>
                                <Button variant="primary" icon={saving ? Loader2 : Save} disabled={!dirty || saving} onClick={() => void save()}>
                                    {saving ? t('addons.admin.saving') : t('addons.admin.save')}
                                </Button>
                            </div>
                        </fieldset>
                    </Card>
                ) : (
                    <EmptyState icon={PackageSearch} title={t('addons.admin.empty')} />
                )}
            </div>

            <OperationPolicyCard policy={catalog.operation_policy} components={catalog.components} />
        </div>
    );
}

function BoundaryCallout({ icon: Icon, title, text }: { icon: typeof Database; title: string; text: string }) {
    return (
        <div className="flex gap-3 rounded-lg border border-border bg-surface-2 p-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"><Icon className="h-5 w-5" /></div>
            <div><h2 className="text-sm font-semibold text-fg">{title}</h2><p className="mt-0.5 text-xs leading-5 text-fg-muted">{text}</p></div>
        </div>
    );
}

function ReadOnlyValue({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
    return <div><div className="text-xs text-fg-subtle">{label}</div><div className={`mt-0.5 truncate text-sm text-fg ${mono ? 'font-mono' : ''}`}>{value}</div></div>;
}

function LocalizedFields({ localeLabel, name, description, onName, onDescription }: {
    localeLabel: string; name: string; description: string; onName: (value: string) => void; onDescription: (value: string) => void;
}) {
    const { t } = useI18n();
    const fieldPrefix = `catalog-${localeLabel.toLocaleLowerCase('en-US')}`;
    return (
        <div className="space-y-3 rounded-lg border border-border p-3">
            <div className="text-xs font-semibold text-fg-muted">{localeLabel}</div>
            <Field label={t('addons.admin.name')} htmlFor={`${fieldPrefix}-name`}>
                <input
                    id={`${fieldPrefix}-name`}
                    value={name}
                    onChange={(event) => onName(event.target.value)}
                    className={inputClass}
                />
            </Field>
            <Field label={t('addons.admin.description')} htmlFor={`${fieldPrefix}-description`}>
                <textarea
                    id={`${fieldPrefix}-description`}
                    rows={3}
                    value={description}
                    onChange={(event) => onDescription(event.target.value)}
                    className={inputClass}
                />
            </Field>
        </div>
    );
}

function ReleaseBadge({ state }: { state: ReleaseState }) {
    const { t } = useI18n();
    const label = state === 'available' ? t('addons.state.available') : state === 'coming_soon' ? t('addons.state.comingSoon') : t('addons.admin.stateRetired');
    const style = state === 'available' ? 'bg-success/10 text-success' : state === 'coming_soon' ? 'bg-warning/10 text-warning' : 'bg-surface-2 text-fg-muted';
    return <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] font-semibold ${style}`}>{label}</span>;
}

function OperationPolicyCard({ policy, components }: { policy: OperationPolicy; components: AdminComponent[] }) {
    const { t } = useI18n();
    return (
        <Card title={t('addons.admin.policyTitle')} icon={LockKeyhole}>
            <div className="p-4 md:p-5">
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                    <ReadOnlyValue label={t('addons.admin.policyManagement')} value={t('addons.admin.releaseManaged')} />
                    <ReadOnlyValue label={t('addons.admin.policyFormat')} value={policy.catalog_format} mono />
                    <ReadOnlyValue label={t('addons.admin.policyVerification')} value={t('addons.admin.implemented')} />
                    <ReadOnlyValue label={t('addons.admin.policyActivation')} value={t('addons.admin.pending')} />
                </div>
                <p className="mt-4 rounded-lg border border-warning/30 bg-warning/10 p-3 text-xs leading-5 text-fg-muted">{t('addons.admin.policyHint')}</p>
                <details className="mt-4">
                    <summary className="cursor-pointer text-sm font-semibold text-primary">{t('addons.admin.policyComponents', { count: components.length })}</summary>
                    <div className="mt-3 overflow-x-auto rounded-lg border border-border">
                        <table className="min-w-full divide-y divide-border text-left text-sm">
                            <thead className="bg-surface-2 text-xs text-fg-muted"><tr><th className="px-3 py-2">{t('addons.admin.component')}</th><th className="px-3 py-2">{t('addons.admin.kind')}</th><th className="px-3 py-2">{t('addons.admin.lifecycle')}</th><th className="px-3 py-2">{t('addons.admin.source')}</th></tr></thead>
                            <tbody className="divide-y divide-border">
                                {components.map((component) => <tr key={component.id}><td className="px-3 py-2"><div className="font-medium text-fg">{component.name}</div><div className="font-mono text-xs text-fg-subtle">{component.id}</div></td><td className="px-3 py-2 text-fg-muted">{component.kind}</td><td className="px-3 py-2 font-mono text-xs text-fg-muted">{component.lifecycle_operations.join(', ') || '—'}</td><td className="px-3 py-2 text-fg-muted">{t('addons.admin.readOnly')}</td></tr>)}
                            </tbody>
                        </table>
                    </div>
                </details>
            </div>
        </Card>
    );
}

function toDraft(offering: AdminOffering | null): DraftOffering | null {
    return offering ? { ...structuredClone(offering), tagsText: offering.metadata.tags.join(', ') } : null;
}

function fromDraft(draft: DraftOffering): AdminOffering {
    const { tagsText, ...offering } = draft;
    return { ...offering, metadata: { ...offering.metadata, tags: tagsText.split(',').map((tag) => tag.trim()).filter(Boolean) } };
}

function toComparable(offering: AdminOffering) {
    return { category: offering.category.trim(), vendor: offering.vendor.trim(), release_state: offering.release_state, metadata: { ...offering.metadata, name: { en: offering.metadata.name.en.trim(), tr: offering.metadata.name.tr.trim() }, description: { en: offering.metadata.description.en.trim(), tr: offering.metadata.description.tr.trim() }, icon: offering.metadata.icon.trim(), tags: offering.metadata.tags.map((tag) => tag.trim()).filter(Boolean) }, component_ids: offering.component_ids, sort_order: offering.sort_order };
}

function normalizeCatalog(value: unknown): CatalogResponse {
    if (!value || typeof value !== 'object') throw new Error('invalid catalog response');
    const raw = value as Partial<CatalogResponse>;
    const offerings = Array.isArray(raw.offerings) ? raw.offerings.map(normalizeOffering).filter((item): item is AdminOffering => item != null) : [];
    const components = Array.isArray(raw.components) ? raw.components.map(normalizeComponent).filter((item): item is AdminComponent => item != null) : [];
    if (!raw.operation_policy || raw.operation_policy.browser_editable !== false || raw.operation_policy.management !== 'release_managed' || raw.operation_policy.runtime_activation !== 'pending') throw new Error('invalid operation policy');
    return { offerings, components, operation_policy: raw.operation_policy };
}

function normalizeOffering(value: unknown): AdminOffering | null {
    if (!value || typeof value !== 'object') return null;
    const item = value as Partial<AdminOffering>;
    if (!item.id || !item.kind || !item.category || !item.vendor || !item.entitlement_mode || !item.updated_at || !RELEASE_STATES.has(item.release_state as ReleaseState) || !item.metadata || !Array.isArray(item.component_ids) || !Number.isSafeInteger(item.sort_order) || !Number.isSafeInteger(item.active_entitlements)) return null;
    if (!item.metadata.name?.en || !item.metadata.name?.tr || !item.metadata.description?.en || !item.metadata.description?.tr || !item.metadata.icon || !Array.isArray(item.metadata.tags)) return null;
    return item as AdminOffering;
}

function normalizeComponent(value: unknown): AdminComponent | null {
    if (!value || typeof value !== 'object') return null;
    const item = value as Partial<AdminComponent>;
    if (!item.id || !item.name || !item.kind || !Array.isArray(item.lifecycle_operations) || item.editable !== false || item.policy_source !== 'release_managed') return null;
    return item as AdminComponent;
}

function normalizeAdminError(cause: unknown, fallback: string): ApiError {
    if (cause && typeof cause === 'object' && 'message' in cause) return cause as ApiError;
    return { message: typeof cause === 'string' ? cause : fallback };
}
