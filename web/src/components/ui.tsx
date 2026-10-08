import { useEffect, useRef, type FormEvent, type ReactNode } from 'react';
import { AlertTriangle, type LucideIcon } from 'lucide-react';
import { useNavigate } from '../router';
import { useI18n } from '../i18n';
import { apiErrorActionLabel, apiErrorText, type ApiError } from '../lib/apiError';
import type { Observed, Remote } from '../lib/remote';
import type { LostAnswerHandle } from '../lib/lostAnswer';
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
        <div className={`rounded-xl border border-border-strong bg-surface ${className}`}>
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

// UsageBar renders a labelled progress bar. It is navy until it is a problem
// and red past 90%. There is deliberately no yellow band: meters sit beside
// each other in a grid, and a yellow bar next to a red one is the one pairing
// this design system forbids. The 75-90 band is said in the number instead.
//
// UsageBar etiketli bir ilerleme çubuğu çizer. Sorun olana kadar lacivert,
// %90 üstünde kırmızıdır. Sarı bant bilerek yok: ölçerler yan yana durur ve
// sarı çubuğun kırmızının yanına gelmesi bu sistemde yasaktır.
export function UsageBar({ percent }: { percent: number }) {
    const clamped = Math.max(0, Math.min(100, percent));
    const color = clamped >= 90 ? 'bg-danger' : 'bg-fg-subtle';
    return (
        <div className="h-2 w-full overflow-hidden rounded-md bg-surface-2">
            <div className={`h-full rounded-md ${color} transition-all`} style={{ width: `${clamped}%` }} />
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
    tone = 'primary',
    label,
    className,
}: {
    /** current takes the colour of the text around it, for filled controls. */
    tone?: 'primary' | 'current';
    /** md is the size twenty-four of the copies used; sm is the other four;
        xs sits inside a control, beside its label. */
    size?: 'md' | 'sm' | 'xs';
    /** Overrides the default "Loading" for a wait that is about one thing. */
    label?: string;
    className?: string;
}) {
    const { t } = useI18n();
    const box = size === 'xs' ? 'h-4 w-4' : size === 'sm' ? 'h-7 w-7' : 'h-8 w-8';
    const arc = tone === 'current' ? 'border-current' : 'border-primary';
    return (
        <div
            role="status"
            aria-label={label ?? t('common.loading')}
            className={`${box} animate-spin rounded-full border-b-2 ${arc} ${className ?? ''}`}
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
    loading = false,
    children,
    ...props
}: {
    variant?: 'primary' | 'secondary' | 'danger';
    icon?: LucideIcon;
    // A button that has started work says so in place, at the size it already
    // is: the spinner takes the icon's slot so the control never changes
    // height, and the button is disabled for as long as it spins.
    // Ise baslamis bir dugme bunu yerinde soyler: donen isaret ikonun yerini
    // alir, dugme boyu degismez ve donerken devre disidir.
    loading?: boolean;
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
    // 9 Oct 2026, seen in a browser: in the dark theme the recessed fill was
    // a navy block with a light label, and it read as a filled call to action
    // - more like a button to press than the enabled one beside it. A control
    // that cannot be used now has no fill at all and a dashed outline in the
    // colour of its own label, which says "not available" by shape as well as
    // by colour, in both themes, in every skin and on every surface a button
    // stands on. A button that is working keeps the recessed fill: it is busy,
    // not unavailable, and its spinner says so.
    // 9 Eki 2026, tarayicida goruldu: koyu temada cukur dolgu, acik etiketli
    // lacivert bir bloktu ve yanindaki etkin dugmeden daha cok basilacak bir
    // dugmeye benziyordu. Kullanilamayan denetimin artik dolgusu yoktur ve
    // cercevesi kesik cizgilidir; calisan dugme ise cukur dolguyu korur.
    const off = loading
        ? 'disabled:border-transparent disabled:bg-surface-2'
        : 'disabled:border-dashed disabled:border-current disabled:bg-transparent';
    return (
        <button
            {...props}
            disabled={props.disabled || loading}
            aria-busy={loading || undefined}
            className={`inline-flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm font-medium transition-colors disabled:pointer-events-none disabled:text-fg-muted ${off} ${styles} ${props.className ?? ''}`}
        >
            {loading ? <Spinner size="xs" tone="current" /> : Icon && <Icon className="h-4 w-4" />}
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
            `flex max-h-[90vh] w-full ${dialogWidths[width]} flex-col rounded-xl border ` +
            `${tone === 'danger' ? 'border-danger/40' : 'border-border-strong'} bg-surface`,
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
            className={`fixed inset-0 ${stacked ? 'z-[60]' : 'z-50'} flex items-center justify-center bg-scrim/80 p-4`}
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
                className="w-56 rounded-lg border border-border-strong bg-surface py-1.5 pl-9 pr-3 text-sm text-fg outline-none placeholder:text-fg-subtle focus:border-primary"
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
    'w-full rounded-lg border border-border-strong bg-surface px-3 py-2 text-sm text-fg outline-none transition-shadow focus:border-primary';

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
        <div className="flex flex-col items-center justify-center rounded-xl border border-border-strong bg-surface px-6 py-16 text-center">
            <Icon className="mb-4 h-12 w-12 text-fg-subtle" />
            <h3 className="text-lg font-semibold text-fg">{title}</h3>
            {hint && <p className="mt-1 text-sm text-fg-muted">{hint}</p>}
            {action && <div className="mt-5">{action}</div>}
        </div>
    );
}

// --- No negative UI unless known (9 Oct 2026, D-024) -------------------------
//
// A screen that reads the server is in one of three states, and they never
// look alike (see lib/remote.ts):
//
//   Checking       not known yet: one quiet line, the colour of ordinary text.
//                  Nothing has failed, so nothing here looks like a problem.
//   CouldNotCheck  the read failed: the screen's own sentence and Retry.
//   known          only now "missing", "not ready", "none" or an empty list.
//
// RemoteGate is the three of them in order. Its children receive a value the
// server really sent and nothing else, so whatever they draw - an empty state,
// a blocker, a disabled control with its reason - cannot be drawn before the
// answer exists. KnownEmpty is EmptyState with that proof as a required prop.
//
// Sunucuyu okuyan ekran üç durumdan birindedir ve bunlar birbirine benzemez:
// Checking (henüz bilinmiyor; sakin tek satır, hiçbir şey başarısız değil),
// CouldNotCheck (okuma başarısız; ekranın kendi cümlesi ve Tekrar dene) ve
// biliniyor (ancak şimdi "eksik", "hazır değil", "yok" ya da boş liste).
// RemoteGate üçünü sırayla çizer; çocukları yalnız sunucunun gerçekten
// gönderdiği değeri alır.
export function Checking({ label, className }: { label: string; className?: string }) {
    return (
        <div className={`flex items-center gap-2 text-sm text-fg-muted ${className ?? ''}`}>
            <Spinner size="xs" label={label} />
            <span aria-hidden="true">{label}</span>
        </div>
    );
}

export function CouldNotCheck({
    text,
    onRetry,
    busy,
    actionLabel,
    beside,
    className,
}: {
    /** The screen's own sentence: what could not be read, and that nothing changed. */
    text: ReactNode;
    /** Reads again. It must not change anything on the server. */
    onRetry: () => void;
    busy?: boolean;
    actionLabel?: string;
    /** A second way to look, beside the read: a link that only opens something. */
    beside?: ReactNode;
    className?: string;
}) {
    const { t } = useI18n();
    return (
        <div
            role="alert"
            className={`flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm leading-relaxed text-fg ${className ?? ''}`}
        >
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
            <div className="min-w-0">
                <p className="max-w-[75ch] break-words">{text}</p>
                <div className="mt-2 flex flex-wrap items-center gap-2">
                    <Button type="button" loading={busy} onClick={onRetry}>
                        {actionLabel ?? t('common.retry')}
                    </Button>
                    {beside}
                </div>
            </div>
        </div>
    );
}

/** A value the server really sent, and whether a later read of it failed. */
export interface Shown<T> extends Observed<T> {
    stale: boolean;
}

export function RemoteGate<T>({
    remote,
    checking,
    failed,
    onRetry,
    busy,
    className,
    children,
}: {
    remote: Remote<T>;
    /** The checking line, about this one thing: "Reading the domains…". */
    checking: string;
    /** The screen's own "could not check" sentence. */
    failed: string;
    onRetry: () => void;
    busy?: boolean;
    /** Spacing for the checking line and the notice, where the content has its own. */
    className?: string;
    children: (shown: Shown<T>) => ReactNode;
}) {
    const { t, locale } = useI18n();
    if (remote.state === 'loading') return <Checking label={checking} className={className} />;
    if (remote.state === 'known') return <>{children({ value: remote.value, observedAt: remote.observedAt, stale: false })}</>;
    if (!remote.previous) return <CouldNotCheck text={failed} onRetry={onRetry} busy={busy} className={className} />;
    // The earlier answer stays on screen, under a notice that says it is the
    // earlier answer and when it was read.
    // Önceki yanıt ekranda kalır; üstündeki bildirim bunun önceki yanıt
    // olduğunu ve ne zaman okunduğunu söyler.
    const at = new Date(remote.previous.observedAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' });
    return (
        <>
            <CouldNotCheck
                text={t('common.staleNotice', { time: at })}
                onRetry={onRetry}
                busy={busy}
                className={`mb-4 ${className ?? ''}`}
            />
            {children({ ...remote.previous, stale: true })}
        </>
    );
}

export function KnownEmpty({
    of,
    ...props
}: {
    /** The answer that proves there is nothing: an empty state is a claim. */
    of: Observed<unknown>;
    icon: LucideIcon;
    title: string;
    hint?: string;
    action?: ReactNode;
}) {
    void of;
    return <EmptyState {...props} />;
}

// ResultUnknown: a change was sent and its answer did not arrive, so it is not
// known whether it was made (lib/lostAnswer.ts). The notice stands where the
// change was asked for and is scrolled into view. It is the attention surface,
// not the failure one: nothing is known to have failed. "Check again" only
// reads. Until that read has answered, the screen keeps its changing controls
// off; after it, the notice says when the state was read again and stays until
// the person closes it.
//
// Where the form that sent the change asked the re-read state a question
// (10 Oct 2026), the notice says the answer. The state shows the change: it was
// saved, the form is closed, and the notice is a plain confirmation with a
// check mark, no longer the attention surface. The state does not show it: the
// attention surface stays and the sentence says so, and that what was typed is
// still there. In both cases it stays until the person closes it.
// ResultUnknown: bir değişiklik gönderildi ve yanıtı gelmedi; yapılıp
// yapılmadığı bilinmiyor. Bildirim değişikliğin istendiği yerde durur ve
// görünür alana kaydırılır. "Tekrar kontrol et" yalnız okur. Değişikliği
// gönderen form yeniden okunan duruma soru sorduysa bildirim yanıtı söyler:
// durum değişikliği gösteriyorsa kaydedilmiştir (onay işaretli sade bildirim),
// göstermiyorsa bunu ve yazılanın yerinde durduğunu söyler.
export function ResultUnknown({
    answer,
    where,
    className,
}: {
    answer: LostAnswerHandle;
    /** Where to look, when what is read again here cannot show what the change did. */
    where?: string;
    className?: string;
}) {
    const { t, locale } = useI18n();
    const box = useRef<HTMLDivElement>(null);
    const raised = answer.lost?.at;
    useEffect(() => {
        if (raised !== undefined) box.current?.scrollIntoView?.({ block: 'nearest' });
    }, [raised]);
    const lost = answer.lost;
    if (!lost) return null;
    const made = lost.readAt !== null && lost.shows === true;
    const readKey = lost.shows === true ? 'common.resultUnknownMade' : lost.shows === false ? 'common.resultUnknownNotMade' : 'common.resultUnknownRead';
    const text = lost.readAt !== null
        ? t(readKey, { time: new Date(lost.readAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' }) })
        : t(answer.checking ? 'common.resultUnknown' : 'common.resultUnknownUnread');
    return (
        <div
            ref={box}
            role={made ? 'status' : 'alert'}
            data-result-unknown={lost.readAt === null ? 'holding' : made ? 'made' : lost.shows === false ? 'not-made' : 'read'}
            className={`flex items-start gap-2 rounded-lg border p-3 text-sm leading-relaxed text-fg ${made ? 'border-border-strong bg-surface-2' : 'border-warning-mark/50 bg-warning-mark/20'} ${className ?? ''}`}
        >
            {made ? <CheckMark /> : <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />}
            <div className="min-w-0">
                <p className="max-w-[75ch] break-words">{text}</p>
                {where && !made && <p className="mt-1 max-w-[75ch] break-words">{where}</p>}
                <div className="mt-2 flex flex-wrap items-center gap-2">
                    {/* Once the state shows the change there is nothing left
                        to check: only "Close".
                        Durum değişikliği gösterdiğinde kontrol edilecek bir şey
                        kalmaz: yalnız "Kapat". */}
                    {!made && (
                        <Button type="button" loading={answer.checking} onClick={() => void answer.check()}>
                            {t('common.checkAgain')}
                        </Button>
                    )}
                    {lost.readAt !== null && (
                        <Button type="button" onClick={answer.dismiss}>
                            {t('common.close')}
                        </Button>
                    )}
                </div>
            </div>
        </div>
    );
}

// The mark of something the server confirmed. Drawn here, like SearchIcon, so
// the shared layer needs no icon beyond the triangle.
// Sunucunun doğruladığı şeyin işareti.
function CheckMark() {
    return (
        <svg
            className="mt-0.5 h-4 w-4 shrink-0 text-success"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            viewBox="0 0 24 24"
            aria-hidden="true"
        >
            <circle cx="12" cy="12" r="10" />
            <path d="m9 12 2 2 4-4" />
        </svg>
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
