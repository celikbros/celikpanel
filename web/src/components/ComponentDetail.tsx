import { useEffect, useState } from 'react';
import { Boxes, FileText, RefreshCw, ScrollText, Search } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { EmptyState } from './ui';
import { useI18n } from '../i18n';
import { readApiError, apiErrorText } from '../lib/apiError';

interface Instance {
    version: string;
    unit: string;
    path: string;
    managed: boolean;
    status: string;
}

interface Component {
    id: string;
    name: string;
    description: string;
    icon: string;
    category: string;
    kind: string;
    unit?: string;
    status: string;
    /** null = never observed on this host, which is not the same as absent. */
    /** null = bu makinede hiç gözlenmedi; bu, yok demek değildir. */
    is_installed: boolean | null;
    versions: string[];
    instances?: Instance[];
    packages?: string[];
    ports?: string[];
    config_files?: { path: string; is_managed: boolean }[];
}

// The generic component page. Nine components had a hand-written management
// page; every other one — Rspamd, ClamAV, Redis, Memcached, Node — showed a
// Manage button that led to a dead end (operator, 25 Jul: "birçok servisin
// manage'i doğru düzgün çalışmıyor. dinamik manage sistemi yok sanırım").
//
// The fix is not nine more hand-written pages — that list would rot the same
// way the scanner's hand-written unit list did. This page is DERIVED: it shows
// what the panel already knows about any component (unit, kind, versions,
// packages, ports, config files) plus the one thing every daemon has and
// nobody could see from the panel — its own journal. A component added to the
// catalogue tomorrow gets a working Manage page without touching this file.
//
// Genel bileşen sayfası. Dokuz bileşenin elle yazılmış yönetim sayfası vardı;
// geri kalan her biri — Rspamd, ClamAV, Redis, Memcached, Node — çıkmaz sokağa
// çıkan bir Yönet düğmesi gösteriyordu (operatör, 25 Tem: "birçok servisin
// manage'i doğru düzgün çalışmıyor. dinamik manage sistemi yok sanırım").
//
// Çözüm dokuz elle yazılmış sayfa daha değil — öyle bir liste, tarayıcının elle
// yazılmış unit listesi gibi çürürdü. Bu sayfa TÜRETİLİR: panelin herhangi bir
// bileşen hakkında zaten bildiklerini (unit, tür, sürümler, paketler, portlar,
// ayar dosyaları) ve her daemon'da bulunup panelden görülemeyen tek şeyi —
// kendi günlüğünü — gösterir. Yarın kataloğa eklenen bir bileşen, bu dosyaya
// dokunulmadan çalışan bir Yönet sayfasına kavuşur.
export function ComponentDetail({ serviceId, onBack, onSelectConfig }: { serviceId: string; onBack: () => void; onSelectConfig?: (path: string) => void }) {
    const [svc, setSvc] = useState<Component | null>(null);
    // On an unchecked host this record carries no unit, no versions and no
    // config files — not because there are none, but because nobody has
    // looked. The moment the shell resolves that (the operator's check, or a
    // finished install) this copy is stale, and a stale copy under a resolved
    // header would state absences as facts: start/stop would target the id
    // instead of the real unit (BIND's id is "bind", its unit "named"), and
    // the panels would say "no configuration files". So it is reread.
    // Bakılmamış bir makinede bu kayıtta unit, sürüm ve ayar dosyası yoktur —
    // yok oldukları için değil, bakılmadığı için. Kabuk durumu çözdüğü anda
    // bu kopya bayatlar ve çözülmüş bir başlığın altındaki bayat kopya
    // yoklukları olgu diye söyler; bu yüzden yeniden okunur.
    const [recordToken, setRecordToken] = useState(0);

    useEffect(() => {
        let cancelled = false;
        fetch('/api/v1/managed-services')
            .then((r) => (r.ok ? r.json() : { services: [] }))
            .then((d: { services: Component[] }) => {
                if (!cancelled) setSvc((d.services || []).find((s) => s.id === serviceId) ?? null);
            })
            .catch(() => {});
        return () => {
            cancelled = true;
        };
    }, [serviceId, recordToken]);

    return (
        <ServiceShell
            serviceId={serviceId}
            unitName={svc?.unit}
            name={svc?.name ?? serviceId}
            icon={Boxes}
            onBack={onBack}
            onServiceRefreshed={() => setRecordToken((token) => token + 1)}
        >
            <ComponentPanels serviceId={serviceId} svc={svc} onSelectConfig={onSelectConfig} />
        </ServiceShell>
    );
}

// ComponentPanels: the derived sections, exported so the SPECIALISED pages can
// append them under their own content. Before this, a page like vsftpd's was
// an empty "coming soon" shell while the panel already KNEW its config file,
// its ports and its journal — the knowledge existed, only the page refused to
// show it (operator, 25 Jul: "birçok servisin manage sayfaları berbat").
// `show` lets a page skip sections it already covers better (PostgreSQL and
// MariaDB have real config editors, so they hide the plain file list).
//
// ComponentPanels: türetilmiş bölümler; ÖZEL sayfalar kendi içeriklerinin
// altına ekleyebilsin diye dışa açıldı. Bundan önce vsftpd'ninki gibi bir
// sayfa boş bir "yakında" kabuğuyken panel onun ayar dosyasını, portlarını ve
// günlüğünü zaten BİLİYORDU — bilgi vardı, yalnız sayfa göstermeyi
// reddediyordu (operatör, 25 Tem: "birçok servisin manage sayfaları berbat").
// `show`, bir sayfanın zaten daha iyi kapsadığı bölümü atlamasını sağlar
// (PostgreSQL ve MariaDB'nin gerçek ayar editörleri var; düz dosya listesini
// gizlerler).
export function ComponentPanels({
    serviceId,
    svc: svcProp,
    onSelectConfig,
    show,
}: {
    serviceId: string;
    svc?: Component | null;
    onSelectConfig?: (path: string) => void;
    show?: { facts?: boolean; configs?: boolean; journal?: boolean };
}) {
    const [fetched, setFetched] = useState<Component | null>(null);
    const needFetch = svcProp === undefined;

    useEffect(() => {
        if (!needFetch) return;
        fetch('/api/v1/managed-services')
            .then((r) => (r.ok ? r.json() : { services: [] }))
            .then((d: { services: Component[] }) => setFetched((d.services || []).find((s) => s.id === serviceId) ?? null))
            .catch(() => {});
    }, [serviceId, needFetch]);

    const svc = needFetch ? fetched : svcProp;
    const want = { facts: true, configs: true, journal: true, ...show };

    return (
        <div className="mt-6 space-y-6 first:mt-0">
            {want.facts && <Facts svc={svc ?? null} />}
            {want.configs && <ConfigFiles svc={svc ?? null} onSelectConfig={onSelectConfig} />}
            {want.journal && <Journal unit={svc?.unit || serviceId} />}
        </div>
    );
}

function Card({ title, icon: Icon, right, children }: { title: string; icon: typeof Boxes; right?: React.ReactNode; children: React.ReactNode }) {
    return (
        <section className="rounded-2xl border border-border bg-surface">
            <header className="flex items-center gap-2 border-b border-border px-5 py-3">
                <Icon className="h-4 w-4 text-fg-muted" />
                <h2 className="text-sm font-semibold text-fg">{title}</h2>
                {right && <div className="ml-auto">{right}</div>}
            </header>
            <div className="p-5">{children}</div>
        </section>
    );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 py-1.5">
            <dt className="w-40 shrink-0 text-xs uppercase tracking-wide text-fg-subtle">{label}</dt>
            <dd className="min-w-0 flex-1 text-sm text-fg">{children}</dd>
        </div>
    );
}

const Chip = ({ children }: { children: React.ReactNode }) => (
    <span className="mr-1.5 inline-block rounded bg-surface-2 px-1.5 py-0.5 font-mono text-xs text-fg-muted">{children}</span>
);

function Facts({ svc }: { svc: Component | null }) {
    const { t } = useI18n();
    if (!svc) return null;
    const kindLabel =
        svc.kind === 'service' ? t('component.kindService') : svc.kind === 'runtime' ? t('component.kindRuntime') : t('component.kindTool');
    const managed = (svc.instances ?? []).filter((i) => i.managed);
    return (
        <Card title={t('component.overview')} icon={Boxes}>
            <dl className="divide-y divide-border/60">
                <Row label={t('component.kind')}>{kindLabel}</Row>
                {/* The unit is the thing start/stop actually targets, and it is
                    not always the id (BIND's id is "bind", its unit "named").
                    Showing it makes that difference visible instead of puzzling.
                    Unit, başlat/durdur'un gerçekten hedeflediği şeydir ve her
                    zaman id değildir (BIND'in id'si "bind", unit'i "named").
                    Göstermek, bu farkı bulmaca olmaktan çıkarır. */}
                {svc.unit && <Row label={t('component.unit')}><Chip>{svc.unit}</Chip></Row>}
                {managed.length > 0 && (
                    <Row label={t('component.versions')}>
                        {managed.map((i) => (
                            <Chip key={i.unit || i.path || i.version}>{i.version || '—'}</Chip>
                        ))}
                    </Row>
                )}
                {svc.packages && svc.packages.length > 0 && (
                    <Row label={t('component.packages')}>
                        {svc.packages.map((p) => (
                            <Chip key={p}>{p}</Chip>
                        ))}
                    </Row>
                )}
                <Row label={t('component.ports')}>
                    {svc.ports && svc.ports.length > 0 ? (
                        svc.ports.map((p) => <Chip key={p}>{p}</Chip>)
                    ) : (
                        /* No ports is a real, reassuring answer — a local-only
                           component (redis, php-fpm) exposes nothing.
                           Port yok, gerçek ve rahatlatıcı bir cevaptır — yalnız
                           yerel bir bileşen (redis, php-fpm) hiçbir şey açmaz. */
                        <span className="text-fg-subtle">{t('component.portsNone')}</span>
                    )}
                </Row>
            </dl>
        </Card>
    );
}

function ConfigFiles({ svc, onSelectConfig }: { svc: Component | null; onSelectConfig?: (path: string) => void }) {
    const { t } = useI18n();
    const files = svc?.config_files ?? [];
    return (
        <Card title={t('component.configFiles')} icon={FileText}>
            {files.length === 0 ? (
                <p className="text-sm text-fg-subtle">{t('component.configNone')}</p>
            ) : (
                <ul className="space-y-1">
                    {files.map((f) => (
                        <li key={f.path}>
                            <button
                                onClick={() => onSelectConfig?.(f.path)}
                                disabled={!onSelectConfig}
                                className="w-full rounded-lg px-2 py-1.5 text-left font-mono text-xs text-fg-muted transition-colors hover:bg-surface-2 hover:text-fg disabled:cursor-default disabled:hover:bg-transparent"
                            >
                                {f.path}
                                {f.is_managed && <span className="ml-2 rounded bg-primary/10 px-1.5 py-0.5 font-sans text-[10px] text-primary">{t('component.managed')}</span>}
                            </button>
                        </li>
                    ))}
                </ul>
            )}
        </Card>
    );
}

function Journal({ unit }: { unit: string }) {
    const { t } = useI18n();
    const [lines, setLines] = useState<string[] | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);
    const [filter, setFilter] = useState('');

    const load = async () => {
        setBusy(true);
        setError(null);
        try {
            const r = await fetch(`/api/v1/service/logs?unit=${encodeURIComponent(unit)}&lines=200`);
            if (!r.ok) {
                setError(apiErrorText(await readApiError(r), t, 'component.logsFailed'));
                setLines([]);
                return;
            }
            const d: { lines?: string[] } = await r.json();
            setLines(d.lines ?? []);
        } catch {
            setError(t('component.logsFailed'));
            setLines([]);
        } finally {
            setBusy(false);
        }
    };

    useEffect(() => {
        load();
    }, [unit]);

    const shown = (lines ?? []).filter((l) => (filter ? l.toLowerCase().includes(filter.toLowerCase()) : true));

    return (
        <Card
            title={t('component.logs')}
            icon={ScrollText}
            right={
                <div className="flex items-center gap-2">
                    <div className="relative">
                        <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-fg-subtle" />
                        <input
                            value={filter}
                            onChange={(e) => setFilter(e.target.value)}
                            placeholder={t('component.logsFilter')}
                            className="w-40 rounded-lg border border-border bg-surface-2 py-1 pl-7 pr-2 text-xs text-fg placeholder:text-fg-subtle focus:outline-none focus:ring-1 focus:ring-primary"
                        />
                    </div>
                    <button
                        onClick={load}
                        disabled={busy}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1 text-xs font-medium text-fg transition-colors hover:bg-surface-2 disabled:opacity-50"
                    >
                        <RefreshCw className={`h-3.5 w-3.5 ${busy ? 'animate-spin' : ''}`} />
                        {t('component.logsRefresh')}
                    </button>
                </div>
            }
        >
            {error ? (
                <p className="text-sm text-danger">{error}</p>
            ) : lines === null ? (
                <p className="text-sm text-fg-subtle">{t('common.loading')}</p>
            ) : shown.length === 0 ? (
                <EmptyState icon={ScrollText} title={t('component.logsEmpty')} hint={t('component.logsEmptyHint', { unit })} />
            ) : (
                <pre className="max-h-96 overflow-auto rounded-lg bg-surface-2 p-3 font-mono text-[11px] leading-relaxed text-fg-muted">
                    {shown.join('\n')}
                </pre>
            )}
        </Card>
    );
}
