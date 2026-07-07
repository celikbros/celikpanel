import { useEffect, useState } from 'react';
import { Package, Check, Plus, X } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { PageHeader, Button, EmptyState } from './ui';

interface Product {
    id: string;
    name: string;
    description: string;
    category: string;
    monthly_price_cents: number;
}
interface Subscription {
    id: number;
    name: string;
    owner: string;
}
interface Entitlement {
    product_id: string;
    status: string;
}

// Add-on management: the admin/reseller side of the product layer. Pick a
// subscription, then grant or revoke curated products for it. This is the UI
// over the entitlement ledger the backend enforces — grant here and the
// customer's gated features unlock; revoke and they lock again.
// Eklenti yönetimi: ürün katmanının admin/bayi tarafı. Bir abonelik seç,
// sonra ona kürlü ürünleri ver ya da geri al. Backend'in uyguladığı hak
// defterinin arayüzü — burada ver, müşterinin kapılı özellikleri açılır;
// geri al, yeniden kilitlenir.
export function AddonsPage() {
    const { t } = useI18n();
    const [products, setProducts] = useState<Product[]>([]);
    const [subs, setSubs] = useState<Subscription[]>([]);
    const [selected, setSelected] = useState<number | null>(null);
    const [held, setHeld] = useState<Set<string>>(new Set());
    const [busy, setBusy] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        Promise.all([
            fetch('/api/v1/products').then((r) => (r.ok ? r.json() : { products: [] })),
            fetch('/api/v1/subscriptions').then((r) => (r.ok ? r.json() : { subscriptions: [] })),
        ])
            .then(([p, s]) => {
                setProducts(p.products || []);
                const list: Subscription[] = s.subscriptions || [];
                setSubs(list);
                if (list.length) setSelected(list[0].id);
            })
            .catch(() => {})
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        if (selected == null) return;
        fetch(`/api/v1/subscriptions/${selected}/entitlements`)
            .then((r) => (r.ok ? r.json() : { entitlements: [] }))
            .then((d) => setHeld(new Set((d.entitlements || []).filter((e: Entitlement) => e.status === 'active').map((e: Entitlement) => e.product_id))))
            .catch(() => {});
    }, [selected]);

    const grant = async (productID: string) => {
        if (selected == null) return;
        setBusy(productID);
        try {
            const r = await fetch(`/api/v1/subscriptions/${selected}/entitlements`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ product_id: productID }),
            });
            if (!r.ok) throw new Error();
            setHeld((prev) => new Set(prev).add(productID));
            showToast('success', t('addons.granted'));
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(null);
        }
    };

    const revoke = async (productID: string) => {
        if (selected == null) return;
        setBusy(productID);
        try {
            const r = await fetch(`/api/v1/subscriptions/${selected}/entitlements/${productID}`, { method: 'DELETE' });
            if (!r.ok) throw new Error();
            setHeld((prev) => {
                const next = new Set(prev);
                next.delete(productID);
                return next;
            });
            showToast('success', t('addons.revoked'));
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(null);
        }
    };

    if (loading) {
        return (
            <div className="flex h-full items-center justify-center">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }

    return (
        <div className="p-6 md:p-8">
            <PageHeader
                title={t('addons.title')}
                subtitle={t('addons.subtitle')}
                breadcrumb={[t('common.home'), t('addons.title')]}
            />

            {subs.length === 0 ? (
                <EmptyState icon={Package} title={t('addons.noSubs')} hint={t('addons.noSubsHint')} />
            ) : (
                <div className="grid grid-cols-1 gap-5 lg:grid-cols-[260px_1fr]">
                    {/* Subscription picker */}
                    <aside className="rounded-xl border border-border bg-surface p-2 shadow-card lg:self-start">
                        {subs.map((s) => (
                            <button
                                key={s.id}
                                onClick={() => setSelected(s.id)}
                                className={`flex w-full flex-col rounded-lg px-3 py-2 text-left transition-colors ${
                                    selected === s.id ? 'bg-primary/10 text-primary' : 'text-fg-muted hover:bg-surface-2'
                                }`}
                            >
                                <span className="truncate text-sm font-medium">{s.name}</span>
                                <span className="truncate text-xs text-fg-subtle">{s.owner}</span>
                            </button>
                        ))}
                    </aside>

                    {/* Product grid for the selected subscription */}
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                        {products.map((prod) => {
                            const active = held.has(prod.id);
                            return (
                                <div key={prod.id} className="flex items-start gap-3 rounded-xl border border-border bg-surface p-4">
                                    <span className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-lg ${active ? 'bg-success/10 text-success' : 'bg-surface-2 text-fg-muted'}`}>
                                        {active ? <Check className="h-5 w-5" /> : <Package className="h-5 w-5" />}
                                    </span>
                                    <div className="min-w-0 flex-1">
                                        <div className="flex items-center gap-2">
                                            <span className="font-semibold text-fg">{prod.name}</span>
                                            {prod.monthly_price_cents > 0 && (
                                                <span className="rounded bg-surface-2 px-1.5 py-0.5 text-xs text-fg-muted">
                                                    {t('addons.perMonth', { price: (prod.monthly_price_cents / 100).toFixed(2) })}
                                                </span>
                                            )}
                                        </div>
                                        <p className="mb-3 text-xs text-fg-muted">{prod.description}</p>
                                        {active ? (
                                            <Button variant="secondary" icon={X} disabled={busy === prod.id} onClick={() => revoke(prod.id)}>
                                                {t('addons.revoke')}
                                            </Button>
                                        ) : (
                                            <Button variant="primary" icon={Plus} disabled={busy === prod.id} onClick={() => grant(prod.id)}>
                                                {t('addons.grant')}
                                            </Button>
                                        )}
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}
        </div>
    );
}
