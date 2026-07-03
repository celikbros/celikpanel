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
    ],
    theme: {
        extend: {
            colors: {
                bg: token('--bg'),
                surface: {
                    DEFAULT: token('--surface'),
                    2: token('--surface-2'),
                    3: token('--surface-3'),
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
                warning: { DEFAULT: token('--warning'), fg: token('--warning-fg') },
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
            fontFamily: {
                sans: ['Inter', 'ui-sans-serif', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
            },
            boxShadow: {
                card: '0 1px 2px 0 rgb(0 0 0 / 0.04), 0 1px 3px 0 rgb(0 0 0 / 0.06)',
            },
        },
    },
    plugins: [],
}
