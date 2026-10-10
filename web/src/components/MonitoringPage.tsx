import { useEffect, useState } from 'react';
import { Activity } from 'lucide-react';
import { useI18n } from '../i18n';
import { PageHeader } from './PageHeader';
import { RemoteGate } from './ui';
import { decodeListIn, useRemote } from '../lib/remote';

// Server monitoring (operator request, 23 Jul: "we should have a monitoring
// page"). The dashboard strip answers "how is it NOW"; this page answers
// "what happened while I was away": 48h of 30-second samples from the same
// readers the strip trusts. Charts are plain SVG — no chart library, per the
// no-external-dependency constitution, and honestly these four lines don't
// need one.
//
// Sunucu izleme (operatör isteği, 23 Tem: "monitöring sayfamız olsun"). Pano
// şeridi "ŞU AN nasıl"a cevap verir; bu sayfa "ben yokken ne oldu"ya: şeridin
// güvendiği okuyuculardan 30 saniyelik örneklerle 48 saat. Grafikler düz
// SVG — dış-bağımlılık-yok anayasası gereği grafik kütüphanesi yok; dürüst
// olmak gerekirse bu dört çizginin ihtiyacı da yok.

interface Sample {
    ts: string;
    cpu: number;
    mem_used: number;
    mem_total: number;
    disk_used: number;
    disk_total: number;
    load1: number;
}

const RANGES = [1, 6, 24, 48] as const;
const REFRESH_MS = 60_000;

// An answer without the samples is not "no samples yet".
// Örnekleri taşımayan yanıt "henüz örnek yok" değildir.
const decodeSamples = (raw: unknown) => decodeListIn<Sample>(raw, 'samples');

export function MonitoringPage() {
    const { t } = useI18n();
    const [hours, setHours] = useState<number>(24);
    // One address per range. The page reads again every minute; a read that
    // fails keeps the charts it had, under a notice that says when they were
    // read, instead of wiping them into "no samples yet".
    // Aralık başına bir adres. Sayfa her dakika yeniden okur; başarısız okuma,
    // grafikleri "henüz örnek yok"a çevirmek yerine ne zaman okunduklarını
    // söyleyen bir bildirimin altında tutar.
    const history = useRemote(`/api/v1/metrics/history?hours=${hours}`, decodeSamples);
    const refresh = history.retry;

    useEffect(() => {
        const timer = setInterval(() => void refresh(), REFRESH_MS);
        return () => clearInterval(timer);
    }, [refresh]);

    const pct = (used: number, total: number) => (total > 0 ? (used / total) * 100 : 0);
    const fmtGB = (n: number) => `${(n / 1024 ** 3).toFixed(1)} GB`;

    return (
        <div className="p-6 md:p-8">
            <PageHeader
                title={t('nav.monitoring')}
                subtitle={t('monitoring.subtitle')}
                breadcrumb={[t('common.home'), t('nav.monitoring')]}
            />

            <div className="mb-4 flex items-center gap-1">
                {RANGES.map((h) => (
                    <button
                        key={h}
                        type="button"
                        aria-pressed={hours === h}
                        onClick={() => setHours(h)}
                        className={`rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                            hours === h ? 'bg-primary text-primary-fg' : 'text-fg-muted hover:bg-surface-2'
                        }`}
                    >
                        {t('monitoring.hours', { n: h })}
                    </button>
                ))}
            </div>

            <RemoteGate
                remote={history.remote}
                checking={t('monitoring.checking')}
                failed={t('monitoring.unknown')}
                onRetry={() => void history.retry()}
                busy={history.reading}
                className="py-3"
            >
                {({ value: samples }) => {
                    // "Not enough samples yet" is the server's answer: it sent
                    // fewer than two.
                    // "Henüz yeterli örnek yok" sunucunun yanıtıdır: ikiden az
                    // örnek gönderdi.
                    if (samples.length < 2) {
                        return (
                            <div className="flex flex-col items-center gap-2 rounded-xl border border-border bg-surface p-10 text-center">
                                <Activity className="h-8 w-8 text-fg-subtle" />
                                <p className="text-sm text-fg-muted">{t('monitoring.empty')}</p>
                            </div>
                        );
                    }
                    const last = samples[samples.length - 1];
                    const loadSeries = samples.map((s) => s.load1);
                    return (
                        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                            <MetricChart
                                title={t('monitoring.cpu')}
                                series={samples.map((s) => s.cpu)}
                                max={100}
                                current={`${last.cpu.toFixed(0)}%`}
                                stroke="var(--color-primary, #3b82f6)"
                            />
                            <MetricChart
                                title={t('monitoring.memory')}
                                series={samples.map((s) => pct(s.mem_used, s.mem_total))}
                                max={100}
                                current={`${pct(last.mem_used, last.mem_total).toFixed(0)}% · ${fmtGB(last.mem_used)} / ${fmtGB(last.mem_total)}`}
                                stroke="#8b5cf6"
                            />
                            <MetricChart
                                title={t('monitoring.disk')}
                                series={samples.map((s) => pct(s.disk_used, s.disk_total))}
                                max={100}
                                current={`${pct(last.disk_used, last.disk_total).toFixed(0)}% · ${fmtGB(last.disk_used)} / ${fmtGB(last.disk_total)}`}
                                stroke="#f59e0b"
                            />
                            <MetricChart
                                title={t('monitoring.load')}
                                series={loadSeries}
                                max={Math.max(1, ...loadSeries) * 1.15}
                                current={last.load1.toFixed(2)}
                                stroke="#10b981"
                            />
                        </div>
                    );
                }}
            </RemoteGate>
        </div>
    );
}

// One SVG area chart. viewBox 0..100 × 0..40 stretched to the card; the
// series is normalized against `max` so percent charts share a fixed scale
// (a half-empty disk must LOOK half empty) while load auto-scales.
// Tek SVG alan grafiği. viewBox 0..100 × 0..40 karta gerilir; seri `max`e
// normalize edilir — yüzde grafikleri sabit ölçek paylaşır (yarı dolu disk
// yarı dolu GÖRÜNMELİ), yük ise kendine ölçeklenir.
function MetricChart({
    title,
    series,
    max,
    current,
    stroke,
}: {
    title: string;
    series: number[];
    max: number;
    current: string;
    stroke: string;
}) {
    const W = 100;
    const H = 40;
    const n = series.length;
    const x = (i: number) => (n <= 1 ? 0 : (i / (n - 1)) * W);
    const y = (v: number) => H - Math.min(Math.max(v / max, 0), 1) * H;
    const line = series.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(2)},${y(v).toFixed(2)}`).join(' ');
    const area = `${line} L${W},${H} L0,${H} Z`;
    const peak = Math.max(...series);

    return (
        <div className="rounded-xl border border-border bg-surface p-4">
            <div className="mb-2 flex items-baseline justify-between gap-3">
                <h3 className="text-sm font-semibold text-fg">{title}</h3>
                <span className="font-mono text-sm text-fg-muted">{current}</span>
            </div>
            <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="h-28 w-full">
                <path d={area} fill={stroke} opacity="0.12" />
                <path d={line} fill="none" stroke={stroke} strokeWidth="0.8" vectorEffect="non-scaling-stroke" />
            </svg>
            <div className="mt-1 flex justify-between text-xs text-fg-subtle">
                <span>0</span>
                <span>
                    {'max '}
                    {max <= 100 && peak <= 100 ? `${peak.toFixed(0)}%` : peak.toFixed(2)}
                </span>
            </div>
        </div>
    );
}
