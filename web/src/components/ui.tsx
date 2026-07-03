import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';

// Shared UI primitives so every page speaks one visual language: a page
// header with breadcrumb, raised cards with an icon+title, and a labelled
// usage bar. Reused across the panel to keep density consistent.
//
// Paylaşılan UI ilkelleri; böylece her sayfa tek bir görsel dil konuşur:
// breadcrumb'lı sayfa başlığı, ikon+başlıklı yükseltilmiş kartlar ve
// etiketli kullanım çubuğu.

export function PageHeader({
    title,
    subtitle,
    breadcrumb,
    actions,
}: {
    title: string;
    subtitle?: string;
    breadcrumb?: string[];
    actions?: ReactNode;
}) {
    return (
        <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
            <div>
                {breadcrumb && breadcrumb.length > 0 && (
                    <nav className="mb-1 flex items-center gap-1.5 text-xs text-fg-subtle">
                        {breadcrumb.map((crumb, i) => (
                            <span key={i} className="flex items-center gap-1.5">
                                {i > 0 && <span>/</span>}
                                <span>{crumb}</span>
                            </span>
                        ))}
                    </nav>
                )}
                <h1 className="text-2xl font-bold tracking-tight text-fg">{title}</h1>
                {subtitle && <p className="mt-0.5 text-sm text-fg-muted">{subtitle}</p>}
            </div>
            {actions && <div className="flex items-center gap-2">{actions}</div>}
        </div>
    );
}

export function Card({
    title,
    icon: Icon,
    action,
    children,
    className = '',
}: {
    title?: string;
    icon?: LucideIcon;
    action?: ReactNode;
    children: ReactNode;
    className?: string;
}) {
    return (
        <div className={`rounded-xl border border-border bg-surface shadow-card ${className}`}>
            {title && (
                <div className="flex items-center justify-between border-b border-border px-4 py-3">
                    <div className="flex items-center gap-2 text-sm font-semibold text-fg">
                        {Icon && <Icon className="h-4 w-4 text-fg-muted" />}
                        {title}
                    </div>
                    {action}
                </div>
            )}
            {children}
        </div>
    );
}

// UsageBar renders a labelled progress bar; it turns amber past 75% and red
// past 90% so a full disk or maxed CPU reads at a glance.
// UsageBar etiketli bir ilerleme çubuğu çizer; %75 üstünde sarıya, %90
// üstünde kırmızıya döner; böylece dolu disk ya da zorlanan CPU tek bakışta
// anlaşılır.
export function UsageBar({ percent }: { percent: number }) {
    const clamped = Math.max(0, Math.min(100, percent));
    const color = clamped >= 90 ? 'bg-danger' : clamped >= 75 ? 'bg-warning' : 'bg-primary';
    return (
        <div className="h-2 w-full overflow-hidden rounded-full bg-surface-2">
            <div className={`h-full rounded-full ${color} transition-all`} style={{ width: `${clamped}%` }} />
        </div>
    );
}

export function StatusDot({ ok }: { ok: boolean }) {
    return <span className={`inline-block h-2 w-2 rounded-full ${ok ? 'bg-success' : 'bg-fg-subtle'}`} />;
}
