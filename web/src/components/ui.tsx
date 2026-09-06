import { useEffect, useRef, type FormEvent, type ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { useNavigate } from '../router';
import { useI18n } from '../i18n';
import { apiErrorActionLabel, apiErrorText, type ApiError } from '../lib/apiError';
// Shared UI primitives so every page speaks one visual language: a page
// header with breadcrumb, raised cards with an icon+title, and a labelled
// usage bar. Reused across the panel to keep density consistent.
//
// Paylaşılan UI ilkelleri; böylece her sayfa tek bir görsel dil konuşur:
// breadcrumb'lı sayfa başlığı, ikon+başlıklı yükseltilmiş kartlar ve
// etiketli kullanım çubuğu.

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

// R-064. This spinner was written twenty-eight times across the product, in
// three spellings of the same six classes, and the design hook reported it once
// per file as an accent border on a rounded card. The false positive was
// sanctioned file by file, and the ignore list grew an entry every time
// somebody edited another screen - so the list was the measurement: a thing
// copied one file at a time.
//
// One component, so a change to a loading state is made once. The classes are
// exactly what the twenty-eight were, deliberately: this is a move, and a move
// that also changed how it looks would be two things at once.
//
// The one thing it adds is a name. Twenty-four of the copies sat inside a
// wrapper with role="status" and an accessible label; the rest were a bare
// spinning div, which a screen reader announces as nothing at all. Now every
// one of them says what it is.
//
// R-064. Bu gosterge urun genelinde yirmi sekiz kez, ayni alti sinifin uc ayri
// yazimiyla yazilmisti. Tasarim kancasi onu dosya basina bir kez bildiriyordu
// ve istisna listesi, biri baska bir ekrani her duzenlediginde bir kayit
// buyuyordu; yani liste olcumun kendisiydi. Siniflar bilerek yirmi sekizinin
// tasidiginin aynisi: bu bir tasima. Ekledigi tek sey bir ad.
export function Spinner({
    size = 'md',
    label,
    className,
}: {
    /** md is the size twenty-four of the copies used; sm is the other four. */
    size?: 'md' | 'sm';
    /** Overrides the default "Loading" for a wait that is about one thing. */
    label?: string;
    className?: string;
}) {
    const { t } = useI18n();
    const box = size === 'sm' ? 'h-7 w-7' : 'h-8 w-8';
    return (
        <div
            role="status"
            aria-label={label ?? t('common.loading')}
            className={`${box} animate-spin rounded-full border-b-2 border-primary ${className ?? ''}`}
        />
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
    // R-047's leftover, fixed in the one place it is decided. A disabled button
    // used to be its own enabled skin behind a 50% wash, which is the cheapest
    // possible answer and the least legible one: a disabled primary's label
    // read 2.1:1 against its own fill in light and 2.4:1 in dark, and a
    // disabled danger fared no better. That matters most exactly where the
    // wash was doing the most work - a refusal, where the unavailable control
    // is what names the action being refused, and a control an operator cannot
    // read explains nothing.
    //
    // Every variant now steps down to the same recessed pairing instead, which
    // clears AA in both themes and in every skin, says "unavailable" once
    // rather than three different ways, and is one rule where there were three.
    // Pointer events go with it: a disabled fill must not light up under the
    // cursor, and switching them off is what makes that true for the hover
    // colour of every variant at once, present and future.
    //
    // R-047'nin artigi, karara varildigi tek yerde giderildi. Devre disi bir
    // buton, kendi etkin gorunumunun %50 saydami idi; bu en ucuz ve en az
    // okunur cevaptir: devre disi bir birincil butonun etiketi kendi dolgusuna
    // karsi acikta 2.1:1, koyuda 2.4:1 okunuyordu. Bu, en cok bir rette onem
    // tasir: orada kullanilamayan denetim, reddedilen eylemi adlandiran seydir.
    // Artik her varyant ayni cukur eslesmeye iner; her temada ve her skin'de AA
    // gecer ve uc kural yerine tek kural olur.
    const styles = {
        primary: 'bg-primary text-primary-fg hover:bg-primary-hover border-transparent',
        secondary: 'bg-surface text-fg border-border-strong hover:bg-surface-2',
        danger: 'bg-surface text-danger border-border-strong hover:bg-danger/10 hover:border-danger/40',
    }[variant];
    return (
        <button
            {...props}
            className={`inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-sm font-medium transition-colors disabled:pointer-events-none disabled:bg-surface-2 disabled:text-fg-muted ${styles} ${props.className ?? ''}`}
        >
            {Icon && <Icon className="h-4 w-4" />}
            {children}
        </button>
    );
}

// Dialog: the one modal shape in the product.
//
// Register R-047 found the mail install dialogue with its confirm button below
// the fold at 1440x900 the moment it opened, and fixed it there. R-059 found
// the identical defect in the DNS review dialogue - 994 tall inside a box of
// 808 at 1440x900, 1608 inside 758 at 390x844, opening scrolled to the top, so
// an operator saw a refusal and no way to dismiss it. The two dialogues shared
// nothing: they were two hand-built overlays, and the product had thirteen more
// of them. Fixing it a third time in a third place is how a defect becomes a
// habit, so the shape lives here now and the dialogues call it.
//
// The shape is a bounded column, never one scrolling box:
//   - a header that names the dialogue and does not move,
//   - a body that scrolls and holds everything that can grow,
//   - an action row outside the scroller, so the decision is always on screen.
// A dialogue's height is therefore its content's, capped at 90vh, and its
// actions are visible on open at every viewport this product supports.
//
// Dismissal is a property of the dialogue, not a house style: a destructive
// confirmation passes dismissible={false} and then the backdrop, Escape and
// every other silent exit are gone together, because a dialogue that vanishes
// without a trace reads exactly like a button that did not work.
//
// Dialog: üründeki tek diyalog biçimi.
//
// R-047 defteri, posta kurulum diyaloğunun onay düğmesini 1440x900'de daha
// açılır açılmaz görünür alanın altında buldu ve orada düzeltti. R-059 aynı
// kusuru DNS inceleme diyaloğunda buldu. İkisi hiçbir şey paylaşmıyordu: elle
// kurulmuş iki ayrı kaplamaydılar ve üründe on üç tane daha vardı. Üçüncü kez
// üçüncü bir yerde düzeltmek, kusurun alışkanlığa dönüşme yoludur; bu yüzden
// biçim artık burada yaşar ve diyaloglar onu çağırır.
//
// Biçim, tek bir kayan kutu değil, sınırlı bir sütundur: kaymayan bir başlık,
// büyüyebilen her şeyi tutan kayan bir gövde ve kaydırıcının dışında duran bir
// eylem satırı. Kapatılabilirlik ise diyaloğun bir özelliğidir: yıkıcı bir onay
// dismissible={false} geçer ve sessiz çıkışların tamamı birlikte kalkar.
const dialogWidths = {
    sm: 'max-w-sm',
    md: 'max-w-md',
    lg: 'max-w-lg',
    xl: 'max-w-2xl',
} as const;

// A dialogue can open another one - the install dialogue asks a second time
// before it enables a vendor repository - and Escape belongs to whichever is on
// top. Without this, one keypress closes both and the operator loses the
// dialogue they were reading as well as the one they answered.
//
// Bir diyalog baska bir diyalog acabilir ve Escape en usttekine aittir. Bu
// olmadan tek tusa basmak ikisini birden kapatir.
const openDialogs: symbol[] = [];

export function Dialog({
    id,
    title,
    description,
    icon: Icon,
    iconSlot,
    tone = 'default',
    width = 'md',
    busy = false,
    dismissible = true,
    stacked = false,
    extraDescribedBy,
    onDismiss,
    onSubmit,
    noValidate = false,
    footerLead,
    actions,
    children,
}: {
    /** Root for the dialogue's own ids: `${id}-title`, `${id}-description`. */
    id: string;
    title: ReactNode;
    description?: ReactNode;
    icon?: LucideIcon;
    /** For a dialogue whose header mark is content rather than an icon. */
    iconSlot?: ReactNode;
    tone?: 'default' | 'danger';
    width?: keyof typeof dialogWidths;
    busy?: boolean;
    /** false removes the backdrop, Escape and every other silent exit at once. */
    dismissible?: boolean;
    /** A dialogue opened from inside another one. */
    stacked?: boolean;
    /** Ids of anything else that describes this dialogue - a warning in the
        body that an assistive reader should hear with the title. */
    extraDescribedBy?: string;
    onDismiss?: () => void;
    onSubmit?: (event: FormEvent<HTMLFormElement>) => void;
    /** Leave validation to the dialogue rather than the browser: a form that
        shows its own messages must not also raise a native bubble. */
    noValidate?: boolean;
    /** Sits in the fixed footer above the actions - an acknowledgement that
        gates the primary belongs here, beside the control it gates. */
    footerLead?: ReactNode;
    actions: ReactNode;
    /** The scrolling body. A dialogue that is only a question has none, and
        then the body takes no room at all. */
    children?: ReactNode;
}) {
    const canDismiss = dismissible && !busy && onDismiss !== undefined;
    const tokenRef = useRef<symbol>();
    if (tokenRef.current === undefined) tokenRef.current = Symbol('dialog');

    useEffect(() => {
        const token = tokenRef.current!;
        openDialogs.push(token);
        return () => {
            const at = openDialogs.lastIndexOf(token);
            if (at >= 0) openDialogs.splice(at, 1);
        };
    }, []);

    useEffect(() => {
        if (!canDismiss) return;
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key !== 'Escape') return;
            if (openDialogs[openDialogs.length - 1] !== tokenRef.current) return;
            onDismiss!();
        };
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, [canDismiss, onDismiss]);

    const panelProps = {
        role: 'dialog',
        'aria-modal': true,
        'aria-labelledby': `${id}-title`,
        'aria-describedby': [description === undefined ? null : `${id}-description`, extraDescribedBy]
            .filter((part) => part !== null && part !== undefined)
            .join(' ') || undefined,
        'aria-busy': busy || undefined,
        className:
            `flex max-h-[90vh] w-full ${dialogWidths[width]} flex-col rounded-2xl border ` +
            `${tone === 'danger' ? 'border-danger/40' : 'border-border'} bg-surface shadow-xl`,
    } as const;

    const inner = (
        <>
            <div className="shrink-0 border-b border-border px-6 pb-4 pt-6">
                <div className="flex items-start gap-3">
                    {iconSlot ??
                        (Icon && (
                            <span
                                className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${
                                    tone === 'danger' ? 'bg-danger/10 text-danger' : 'bg-primary/10 text-primary'
                                }`}
                            >
                                <Icon className="h-5 w-5" />
                            </span>
                        ))}
                    <div className="min-w-0">
                        <h3 id={`${id}-title`} className="text-lg font-semibold text-fg">
                            {title}
                        </h3>
                        {description !== undefined && (
                            <p id={`${id}-description`} className="mt-1 text-sm leading-5 text-fg-muted">
                                {description}
                            </p>
                        )}
                    </div>
                </div>
            </div>

            {/* A body that renders nothing this time - a refusal banner with no
                refusal to show - takes no room and leaves no second hairline
                against the footer's.
                Bu sefer hicbir sey cizmeyen bir govde yer kaplamaz ve altbilgi
                cizgisinin yaninda ikinci bir cizgi birakmaz. */}
            <div className="peer min-h-0 flex-1 overflow-y-auto px-6 py-5 empty:hidden">{children}</div>

            <div className="shrink-0 border-t border-border px-6 pb-6 pt-4 peer-empty:border-t-0">
                {footerLead}
                {/* Column-reverse below sm so the primary is the first control a
                    thumb reaches, row-end above it. The caller's DOM order
                    decides which control leads, at both widths at once.
                    sm altında ters sütun: birincil denetim, parmağın ulaştığı
                    ilk denetimdir; üstünde satır sonu hizası. */}
                <div
                    className={`flex flex-col-reverse gap-2 sm:flex-row sm:justify-end ${
                        footerLead === undefined ? '' : 'mt-4'
                    }`}
                >
                    {actions}
                </div>
            </div>
        </>
    );

    return (
        <div
            className={`fixed inset-0 ${stacked ? 'z-[60]' : 'z-50'} flex items-center justify-center bg-black/50 p-4`}
            onMouseDown={(event) => {
                if (canDismiss && event.currentTarget === event.target) onDismiss!();
            }}
        >
            {onSubmit === undefined ? (
                <div {...panelProps}>{inner}</div>
            ) : (
                <form {...panelProps} noValidate={noValidate} onSubmit={onSubmit}>
                    {inner}
                </form>
            )}
        </div>
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
            className={`rounded-lg border border-danger/30 bg-danger/10 px-3 py-2.5 text-sm text-danger ${className ?? ''}`}
        >
            <div className="flex flex-wrap items-center gap-3">
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
            {/* The refusal's evidence (B3d): who blocks, one line each — an
                admin must see what a click would break. The "+N" tail is the
                producer's honest truncation marker.
                Retin kanıtı (B3d): kimin engellediği, satır satır — admin
                tıklamanın neyi kıracağını görmeli. "+N" kuyruğu üreticinin
                dürüst kesme işaretidir. */}
            {error.details && error.details.length > 0 && (
                <ul className="mt-2 max-h-40 space-y-0.5 overflow-y-auto border-t border-danger/20 pt-2 font-mono text-xs">
                    {error.details.map((d) => (
                        <li key={d}>{d.startsWith('+') ? t('common.andMore', { n: d.slice(1) }) : d}</li>
                    ))}
                </ul>
            )}
        </div>
    );
}
