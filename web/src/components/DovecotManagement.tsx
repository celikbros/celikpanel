import { Inbox, Clock, Users } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { ComponentPanels } from './ComponentDetail';
import { useI18n } from '../i18n';
import { useRemote } from '../lib/remote';
import { CouldNotCheck } from './ui';

interface DovecotManagementProps {
    onBack: () => void;
    onSelectConfig?: (path: string) => void;
}

interface DovecotStats {
    uptime: string;
    connections: number;
    logins: number;
    auth_success: number;
    auth_fail: number;
}

// The answer is the two measured figures or it is unknown: a refusal or an
// answer without them is not "no connections".
// Yanıt ya ölçülen iki değerdir ya da bilinmeyendir: ret ya da onları taşımayan
// yanıt "bağlantı yok" değildir.
function decodeDovecotStats(raw: unknown): DovecotStats {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Record<string, unknown>;
    if (typeof body.uptime !== 'string' || typeof body.connections !== 'number') throw new Error('field');
    return body as unknown as DovecotStats;
}

export function DovecotManagement({ onBack, onSelectConfig }: DovecotManagementProps) {
    const { t } = useI18n();
    // Each figure is a value only for an answer the server gave: "…" while it
    // is read and "–" when it could not be read, with the reason and the read
    // again under the cards (9 Oct 2026). Before, both cases drew "—" and
    // stayed silent.
    // Her değer yalnız sunucunun verdiği yanıt için değerdir: okunurken "…",
    // okunamayınca "–"; neden ve yeniden okuma kartların altındadır.
    const { remote, reading, retry } = useRemote('/api/v1/dovecot/stats', decodeDovecotStats);
    const stats = remote.state === 'known' ? remote.value : null;
    const pending = remote.state === 'loading' ? '…' : '–';

    // Only surface what is genuinely measured (uptime, live connections).
    // Login/auth counters need the stats plugin, so we don't show fabricated
    // zeros for them.
    // Yalnızca gerçekten ölçüleni göster (uptime, canlı bağlantı). Giriş/
    // kimlik sayaçları stats eklentisi gerektirir; onlar için uydurma sıfır
    // göstermeyiz.
    return (
        <ServiceShell serviceId="dovecot" name="Dovecot" icon={Inbox} onBack={onBack}>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <StatCard icon={Clock} label={t('dovecot.uptime')} value={stats ? stats.uptime || '—' : pending} />
                <StatCard icon={Users} label={t('dovecot.connections')} value={stats ? String(stats.connections) : pending} />
            </div>
            {remote.state === 'unknown' && (
                <CouldNotCheck text={t('dovecot.statsUnknown')} onRetry={() => void retry()} busy={reading} className="mt-4" />
            )}
            <p className="mt-4 text-xs text-fg-subtle">{t('dovecot.statsNote')}</p>
            {/* The panel already knows Dovecot's unit, ports, packages, config
                files and journal — show them instead of ending the page here
                (operator, 25 Jul). / Panel, Dovecot'un birimini, portlarını,
                paketlerini, ayar dosyalarını ve günlüğünü zaten biliyor —
                sayfayı burada bitirmek yerine onları göster (operatör, 25 Tem). */}
            <ComponentPanels serviceId="dovecot" onSelectConfig={onSelectConfig} />
        </ServiceShell>
    );
}

function StatCard({ icon: Icon, label, value }: { icon: typeof Clock; label: string; value: string }) {
    return (
        <div className="rounded-xl border border-border bg-surface p-5">
            <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-fg-muted">{label}</span>
                <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="h-4 w-4" />
                </span>
            </div>
            <p className="mt-2 text-3xl font-bold tracking-tight">{value}</p>
        </div>
    );
}
