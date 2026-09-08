/** @type {import('tailwindcss').Config} */

// Semantic colors resolve to CSS variables (space-separated RGB triplets)
// defined in index.css, so light and dark are two value sets of the same
// names. The rgb(var(--x) / <alpha-value>) form keeps Tailwind opacity
// modifiers (e.g. bg-primary/10) working.
//
// Semantic renkler, index.css'te tanımlı CSS değişkenlerine (boşlukla
// ayrılmış RGB üçlüleri) çözülür; böylece açık ve koyu, aynı isimlerin iki
// değer kümesidir. rgb(var(--x) / <alpha-value>) biçimi, Tailwind opaklık
// niteleyicilerinin (örn. bg-primary/10) çalışmasını sağlar.
const token = (name) => `rgb(var(${name}) / <alpha-value>)`;

export default {
    darkMode: 'class',
    content: [
        "./index.html",
        "./src/**/*.{js,ts,jsx,tsx}",
        // The design harness uses the same utilities. Without this line they
        // are never emitted, and a screenshot of it measures a page Tailwind
        // did not build. / Tasarim tezgahi da ayni siniflari kullanir; bu
        // satir olmadan uretilmezler ve ekran goruntusu yanlis olur.
        "./gallery/**/*.{ts,tsx}",
    ],
    theme: {
        extend: {
            colors: {
                bg: token('--bg'),
                scrim: token('--scrim'),
                surface: {
                    DEFAULT: token('--surface'),
                    2: token('--surface-2'),
                    3: token('--surface-3'),
                    subtle: token('--surface-subtle'),
                },
                border: {
                    DEFAULT: token('--border'),
                    strong: token('--border-strong'),
                },
                fg: {
                    DEFAULT: token('--fg'),
                    muted: token('--fg-muted'),
                    subtle: token('--fg-subtle'),
                },
                primary: {
                    DEFAULT: token('--primary'),
                    hover: token('--primary-hover'),
                    fg: token('--primary-fg'),
                },
                success: { DEFAULT: token('--success'), fg: token('--success-fg') },
                warning: { DEFAULT: token('--warning'), mark: token('--warning-mark'), fg: token('--warning-fg') },
                danger: { DEFAULT: token('--danger'), fg: token('--danger-fg') },
                sidebar: {
                    DEFAULT: token('--sidebar-bg'),
                    fg: token('--sidebar-fg'),
                    muted: token('--sidebar-fg-muted'),
                    heading: token('--sidebar-heading'),
                    hover: token('--sidebar-hover'),
                    active: token('--sidebar-active'),
                    'active-fg': token('--sidebar-active-fg'),
                    border: token('--sidebar-border'),
                },
            },
            // Font and corner radius come from CSS variables too, so a skin
            // can reshape typography and roundness, not just colors.
            // Yazı tipi ve köşe yuvarlaklığı da CSS değişkenlerinden gelir;
            // bir skin yalnız renkleri değil tipografiyi ve yuvarlaklığı da
            // biçimlendirebilir.
            fontFamily: {
                sans: ['var(--font-sans)'],
                mono: ['var(--font-mono)'],
            },
            borderRadius: {
                md: 'var(--radius-md)',
                lg: 'var(--radius-lg)',
                xl: 'var(--radius-xl)',
                '2xl': 'var(--radius-2xl)',
            },
        },
    },
    plugins: [],
}
