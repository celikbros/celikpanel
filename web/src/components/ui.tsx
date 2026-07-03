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

// Button: one primary (filled blue) call to action per view; everything
// else is secondary (outlined) or danger. Matches Plesk's toolbar buttons.
// Button: görünüm başına tek birincil (dolu mavi) eylem; gerisi ikincil
// (çerçeveli) ya da tehlike. Plesk'in araç çubuğu butonlarıyla uyumlu.
export function Button({
    variant = 'secondary',
    icon: Icon,
    children,
    ...props
}: {
    variant?: 'primary' | 'secondary' | 'danger';
    icon?: LucideIcon;
} & React.ButtonHTMLAttributes<HTMLButtonElement>) {
    const styles = {
        primary: 'bg-primary text-primary-fg hover:bg-primary-hover border-transparent',
        secondary: 'bg-surface text-fg border-border-strong hover:bg-surface-2',
        danger: 'bg-surface text-danger border-border-strong hover:bg-danger/10 hover:border-danger/40',
    }[variant];
    return (
        <button
            {...props}
            className={`inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${styles} ${props.className ?? ''}`}
        >
            {Icon && <Icon className="h-4 w-4" />}
            {children}
        </button>
    );
}

export function SearchInput({
    value,
    onChange,
    placeholder,
}: {
    value: string;
    onChange: (v: string) => void;
    placeholder?: string;
}) {
    return (
        <div className="relative">
            <SearchIcon />
            <input
                type="search"
                value={value}
                onChange={(e) => onChange(e.target.value)}
                placeholder={placeholder}
                className="w-56 rounded-lg border border-border bg-surface py-1.5 pl-9 pr-3 text-sm text-fg outline-none placeholder:text-fg-subtle focus:border-primary focus:ring-2 focus:ring-primary/30"
            />
        </div>
    );
}

function SearchIcon() {
    return (
        <svg
            className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-fg-subtle"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            viewBox="0 0 24 24"
            aria-hidden="true"
        >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
        </svg>
    );
}

export function EmptyState({
    icon: Icon,
    title,
    hint,
    action,
}: {
    icon: LucideIcon;
    title: string;
    hint?: string;
    action?: ReactNode;
}) {
    return (
        <div className="flex flex-col items-center justify-center rounded-xl border border-border bg-surface px-6 py-16 text-center shadow-card">
            <Icon className="mb-4 h-12 w-12 text-fg-subtle" />
            <h3 className="text-lg font-semibold text-fg">{title}</h3>
            {hint && <p className="mt-1 text-sm text-fg-muted">{hint}</p>}
            {action && <div className="mt-5">{action}</div>}
        </div>
    );
}
