import { savedRecoveryRequestId, UPDATE_MARKER_KEY } from '../lib/recoveryObservation';
import { copy } from './copy';
import './style.css';

const read = (key: string) => { try { return localStorage.getItem(key); } catch { return null; } };
let locale: 'en' | 'tr' = read('celikpanel.lang') === 'tr' || (!read('celikpanel.lang') && navigator.language.startsWith('tr')) ? 'tr' : 'en';
const id = savedRecoveryRequestId(read(UPDATE_MARKER_KEY));
const get = <T extends HTMLElement>(name: string) => document.getElementById(name) as T;
let checking = false;
let reachable = false;
let pending: AbortController | null = null;

function render() {
    const labels = copy[locale];
    document.documentElement.lang = locale;
    document.title = `${labels.title} | CelikPanel`;
    document.querySelectorAll<HTMLElement>('[data-copy]').forEach(el => {
        el.textContent = labels[el.dataset.copy as keyof typeof labels];
    });
    for (const lang of ['en', 'tr']) get('lang-' + lang).setAttribute('aria-pressed', String(lang === locale));
    get<HTMLButtonElement>('check').disabled = checking;
    get('check').textContent = checking ? labels.checking : labels.check;
    get('connection').textContent = checking ? labels.checking : reachable ? labels.reachable : labels.waiting;
    get('open').hidden = !reachable;
    get('reference').hidden = !id;
    get('no-reference').hidden = !!id;
    get('operation-command').hidden = !id;
    if (id) {
        get('operation-id').textContent = id;
        get('status-command').textContent = `sudo /usr/libexec/celikpanel/recovery status --request-id ${id} --lang ${locale}`;
    }
}

async function check() {
    if (checking) return;
    checking = true;
    render();
    const request = new AbortController(); pending = request;
    const timeout = window.setTimeout(() => request.abort(), 5000);
    try {
        const response = await fetch('/api/v1/panel/availability', { method: 'GET', credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal: request.signal });
        const value = await response.json();
        // This proves only a response, never an update result or authorization.
        reachable = response.ok && value?.schema === 'celikpanel-panel-availability/v1' && ['starting', 'ready'].includes(value.state)
            || response.status === 401 && value?.code === 'AUTH_REQUIRED';
    } catch { reachable = false; }
    finally { window.clearTimeout(timeout); pending = null; checking = false; render(); }
}
for (const lang of ['en', 'tr'] as const) get('lang-' + lang).addEventListener('click', () => { locale = lang; render(); });
get('check').addEventListener('click', () => void check());
get('open').addEventListener('click', () => {
    if (location.pathname === '/recovery-offline.html') location.replace('/');
    else location.reload();
});
const interval = window.setInterval(() => { if (document.visibilityState === 'visible') void check(); }, 15000);
window.addEventListener('pagehide', () => { clearInterval(interval); pending?.abort(); });
render();
void check();
