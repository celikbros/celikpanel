/**
 * CelikPanel Theme Configuration
 * 
 * Tüm renk ve stil değişkenleri burada tanımlı.
 * Tema değiştirmek için sadece bu dosyayı düzenlemeniz yeterli.
 */

export const theme = {
    // Ana Renkler
    colors: {
        primary: {
            50: '#eff6ff',
            100: '#dbeafe',
            200: '#bfdbfe',
            300: '#93c5fd',
            400: '#60a5fa',
            500: '#3b82f6',  // Ana mavi
            600: '#2563eb',
            700: '#1d4ed8',
            800: '#1e40af',
            900: '#1e3a8a',
        },
        secondary: {
            50: '#f8fafc',
            100: '#f1f5f9',
            200: '#e2e8f0',
            300: '#cbd5e1',
            400: '#94a3b8',
            500: '#64748b',
            600: '#475569',
            700: '#334155',
            800: '#1e293b',
            900: '#0f172a',
        },
        success: {
            400: '#4ade80',
            500: '#22c55e',
            600: '#16a34a',
            900: '#14532d',
        },
        warning: {
            400: '#facc15',
            500: '#eab308',
            600: '#ca8a04',
            900: '#713f12',
        },
        danger: {
            400: '#f87171',
            500: '#ef4444',
            600: '#dc2626',
            900: '#7f1d1d',
        },
        // Arka plan renkleri
        background: {
            primary: '#0f172a',    // Ana arka plan (slate-900)
            secondary: '#1e293b',  // Kart arka planı (slate-800)
            tertiary: '#334155',   // Hover arka plan (slate-700)
            elevated: '#1e293b',   // Yükseltilmiş elementler
        },
        // Metin renkleri
        text: {
            primary: '#f1f5f9',    // Ana metin (slate-100)
            secondary: '#94a3b8',  // İkincil metin (slate-400)
            tertiary: '#64748b',   // Üçüncül metin (slate-500)
            muted: '#475569',      // Silik metin (slate-600)
        },
        // Kenar renkleri
        border: {
            default: '#334155',    // Varsayılan kenar (slate-700)
            light: '#475569',      // Açık kenar (slate-600)
            focus: '#3b82f6',      // Odak kenarlık (blue-500)
        },
    },

    // Kenar yuvarlaklıkları
    borderRadius: {
        none: '0',
        sm: '0.125rem',
        default: '0.25rem',
        md: '0.375rem',
        lg: '0.5rem',
        xl: '0.75rem',
        '2xl': '1rem',
        '3xl': '1.5rem',
        full: '9999px',
    },

    // Gölgeler
    shadows: {
        sm: '0 1px 2px 0 rgb(0 0 0 / 0.05)',
        default: '0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)',
        md: '0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)',
        lg: '0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)',
        xl: '0 20px 25px -5px rgb(0 0 0 / 0.1), 0 8px 10px -6px rgb(0 0 0 / 0.1)',
        glow: {
            primary: '0 0 15px rgba(59, 130, 246, 0.5)',
            success: '0 0 15px rgba(34, 197, 94, 0.5)',
            danger: '0 0 15px rgba(239, 68, 68, 0.5)',
        },
    },

    // Boşluklar
    spacing: {
        xs: '0.25rem',   // 4px
        sm: '0.5rem',    // 8px
        md: '1rem',      // 16px
        lg: '1.5rem',    // 24px
        xl: '2rem',      // 32px
        '2xl': '3rem',   // 48px
    },

    // Font boyutları
    fontSize: {
        xs: '0.75rem',
        sm: '0.875rem',
        base: '1rem',
        lg: '1.125rem',
        xl: '1.25rem',
        '2xl': '1.5rem',
        '3xl': '1.875rem',
        '4xl': '2.25rem',
    },

    // Geçiş animasyonları
    transitions: {
        default: 'all 0.2s ease-in-out',
        fast: 'all 0.1s ease-in-out',
        slow: 'all 0.3s ease-in-out',
    },
};

// CSS değişkenleri olarak tema
export const cssVariables = `
:root {
    /* Primary Colors */
    --color-primary-50: ${theme.colors.primary[50]};
    --color-primary-100: ${theme.colors.primary[100]};
    --color-primary-200: ${theme.colors.primary[200]};
    --color-primary-300: ${theme.colors.primary[300]};
    --color-primary-400: ${theme.colors.primary[400]};
    --color-primary-500: ${theme.colors.primary[500]};
    --color-primary-600: ${theme.colors.primary[600]};
    --color-primary-700: ${theme.colors.primary[700]};
    --color-primary-800: ${theme.colors.primary[800]};
    --color-primary-900: ${theme.colors.primary[900]};
    
    /* Background Colors */
    --bg-primary: ${theme.colors.background.primary};
    --bg-secondary: ${theme.colors.background.secondary};
    --bg-tertiary: ${theme.colors.background.tertiary};
    --bg-elevated: ${theme.colors.background.elevated};
    
    /* Text Colors */
    --text-primary: ${theme.colors.text.primary};
    --text-secondary: ${theme.colors.text.secondary};
    --text-tertiary: ${theme.colors.text.tertiary};
    --text-muted: ${theme.colors.text.muted};
    
    /* Border Colors */
    --border-default: ${theme.colors.border.default};
    --border-light: ${theme.colors.border.light};
    --border-focus: ${theme.colors.border.focus};
    
    /* Status Colors */
    --color-success: ${theme.colors.success[500]};
    --color-warning: ${theme.colors.warning[500]};
    --color-danger: ${theme.colors.danger[500]};
}
`;

export default theme;
