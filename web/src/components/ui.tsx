import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useI18n } from '../i18n';
import { apiErrorActionLabel, apiErrorText, type ApiError } from '../lib/apiError';

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

// Form primitives — a sectioned form (heading + fields separated by
// dividers, no card-in-card nesting), a labelled field with hint, a toggle
// row, and a right-aligned action bar. These give every settings screen the
// same clean, dense layout.
//
// Form ilkelleri — bölümlü form (başlık + bölücülerle ayrılmış alanlar,
// kart-içinde-kart yok), ipuçlu etiketli alan, anahtar satırı ve sağa
// hizalı eylem çubuğu.
export function FormSection({
    title,
    description,
    children,
}: {
    title: string;
    description?: string;
    children: ReactNode;
}) {
    return (
        <section className="border-b border-border py-5 first:pt-0 last:border-0 last:pb-0">
            <h3 className="text-sm font-semibold text-fg">{title}</h3>
            {description && <p className="mt-0.5 text-xs text-fg-muted">{description}</p>}
            <div className="mt-3 space-y-3">{children}</div>
        </section>
    );
}

export function Field({
    label,
    hint,
    htmlFor,
    children,
}: {
    label: string;
    hint?: string;
    htmlFor?: string;
    children: ReactNode;
}) {
    return (
        <div>
            <label htmlFor={htmlFor} className="mb-1 block text-sm font-medium text-fg-muted">
                {label}
            </label>
            {children}
            {hint && <p className="mt-1 text-xs text-fg-subtle">{hint}</p>}
        </div>
    );
}

// inputClass is the shared text-input styling; spread onto <input>/<select>.
// inputClass paylaşılan metin-girdi stilidir; <input>/<select> üzerine geçir.
export const inputClass =
    'w-full rounded-lg border border-border bg-surface px-3 py-2 text-sm text-fg outline-none transition-shadow focus:border-primary focus:ring-2 focus:ring-primary/30';

export function ToggleRow({
    label,
    hint,
    name,
    defaultChecked,
}: {
    label: string;
    hint?: string;
    name: string;
    defaultChecked?: boolean;
}) {
    return (
        <label className="flex cursor-pointer items-start gap-3">
            <input
                type="checkbox"
                name={name}
                defaultChecked={defaultChecked}
                className="mt-0.5 h-4 w-4 accent-primary"
            />
            <span>
                <span className="block text-sm text-fg">{label}</span>
                {hint && <span className="block text-xs text-fg-subtle">{hint}</span>}
            </span>
        </label>
    );
}

export function FormActions({ children }: { children: ReactNode }) {
    return <div className="flex justify-end gap-2 pt-5">{children}</div>;
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

// ErrorBanner: the ONE renderer of the API error contract. Shows the
// localized text of a coded refusal and, when the refusal carries an
// in-panel fix path, a button that goes there. Every new error display
// uses this — hand-rolled red divs are legacy.
// ErrorBanner: API hata sözleşmesinin TEK çizicisi. Kodlu reddin
// yerelleştirilmiş metnini ve ret panel-içi çözüm yolu taşıyorsa oraya
// giden düğmeyi gösterir. Her yeni hata gösterimi bunu kullanır — elle
// yazılmış kırmızı div'ler eskidir.
export function ErrorBanner({ error, className }: { error: ApiError | null; className?: string }) {
    const { t } = useI18n();
    const navigate = useNavigate();
    if (!error) return null;
    const actionLabel = apiErrorActionLabel(error, t);
    return (
        <div
            className={`flex flex-wrap items-center gap-3 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2.5 text-sm text-danger ${className ?? ''}`}
        >
            <span className="min-w-0 flex-1">{apiErrorText(error, t)}</span>
            {error.action && (
                <button
                    type="button"
                    onClick={() => navigate(error.action!)}
                    className="rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg transition-colors hover:bg-primary/90"
                >
                    {actionLabel}
                </button>
            )}
        </div>
    );
}
