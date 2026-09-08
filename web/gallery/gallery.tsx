// A design harness, not a product screen. It mounts the panel's real shared
// components with fixture data so the skin can be inspected without a server,
// in both themes, at every breakpoint. Vite builds it only when this folder is
// passed as the root; nothing here ships in the panel bundle.
//
// Tasarım tezgahı, ürün ekranı değil. Panelin gerçek ortak bileşenlerini
// örnek verilerle mount eder; böylece skin, sunucu olmadan, iki temada ve her
// genişlikte incelenebilir. Panel paketine dahil edilmez.
import { useState } from 'react';
import ReactDOM from 'react-dom/client';
import {
    Activity, Boxes, Database, Globe, Mail, Network, Server, Shield, Terminal, Zap,
} from 'lucide-react';
import {
    Button, Card, Dialog, EmptyState, Field, FormActions, FormSection, SearchInput,
    Spinner, StatusDot, ToggleRow, UsageBar, inputClass,
} from '../src/components/ui';
import { PageHeader } from '../src/components/PageHeader';
import { ThemeProvider } from '../src/theme/ThemeProvider';
import { I18nProvider } from '../src/i18n';
import '../src/index.css';

function Section({ title, children }: { title: string; children: React.ReactNode }) {
    return (
        <section className="space-y-3">
            <h2 className="border-b border-border pb-2 text-base font-bold tracking-tight text-fg">{title}</h2>
            {children}
        </section>
    );
}

const NAV = [
    { icon: Activity, label: 'Genel bakış', active: true },
    { icon: Globe, label: 'Alan adları' },
    { icon: Boxes, label: 'Uygulamalar' },
    { icon: Mail, label: 'E-posta' },
    { icon: Database, label: 'Veritabanları' },
    { icon: Network, label: 'DNS' },
    { icon: Shield, label: 'Güvenlik' },
    { icon: Server, label: 'Servisler' },
];

const SERVICES = [
    { name: 'nginx', category: 'Web', state: 'running' as const, version: '1.29.2' },
    { name: 'MariaDB', category: 'Veritabanı', state: 'running' as const, version: '11.4.4' },
    { name: 'BIND', category: 'DNS', state: 'running' as const, version: '9.20.4' },
    { name: 'Postfix', category: 'E-posta', state: 'stopped' as const, version: '3.9.1' },
    { name: 'nftables', category: 'Güvenlik', state: 'blocked' as const, version: '1.1.1' },
];

function StateBadge({ state }: { state: 'running' | 'stopped' | 'blocked' }) {
    const tone =
        state === 'running' ? 'bg-success/10 text-success'
            : state === 'blocked' ? 'bg-danger/10 text-danger'
                : 'bg-surface-2 text-fg-muted';
    const label = state === 'running' ? 'çalışıyor' : state === 'blocked' ? 'kilitli' : 'durdu';
    return <span className={`inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-semibold ${tone}`}>{label}</span>;
}

function Gallery() {
    const [dialogOpen, setDialogOpen] = useState(false);
    const [toggle, setToggle] = useState(true);
    const [search, setSearch] = useState('');

    return (
        <div className="flex min-h-screen bg-bg text-fg">
            {/* the rail is the illuminated board */}
            <aside className="hidden w-64 shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-fg lg:flex">
                <div className="flex items-center gap-2.5 px-4 py-4">
                    <svg viewBox="0 0 32 32" className="h-7 w-7" aria-hidden>
                        <rect x="9" y="1" width="14" height="30" rx="4" fill="currentColor" opacity=".25" />
                        <circle cx="16" cy="10" r="4.2" fill="currentColor" opacity=".35" />
                        <circle cx="16" cy="22" r="4.2" className="fill-success" />
                    </svg>
                    <span className="text-base font-bold tracking-tight">CelikPanel</span>
                </div>
                <nav className="flex-1 space-y-0.5 px-2 py-2">
                    <p className="px-2 pb-1 pt-3 text-xs font-semibold uppercase tracking-wider text-sidebar-heading">Genel</p>
                    {NAV.map(({ icon: Icon, label, active }) => (
                        <a
                            key={label}
                            href="#"
                            className={`flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm font-medium transition-colors ${
                                active
                                    ? 'bg-sidebar-active text-sidebar-active-fg'
                                    : 'text-sidebar-muted hover:bg-sidebar-hover hover:text-sidebar-fg'
                            }`}
                        >
                            <Icon className="h-4 w-4" />
                            {label}
                        </a>
                    ))}
                </nav>
                <div className="border-t border-sidebar-border px-4 py-3 font-mono text-xs text-sidebar-muted">
                    sunucu-01 · v0.1.0-alpha.53
                </div>
            </aside>

            <main className="min-w-0 flex-1">
                <PageHeader title="Genel bakış" description="Sunucunun durumu ve son değişiklikler." />
                <div className="space-y-10 px-6 pb-16 pt-6">
                    <Section title="Durum">
                        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                            <Card title="Sunucu" icon={Server}>
                                <div className="space-y-3 p-4">
                                    <div className="flex items-center gap-2 text-sm"><StatusDot ok /> Çalışıyor, 14 gündür açık</div>
                                    <div><p className="mb-1 flex justify-between text-xs text-fg-subtle"><span>Disk</span><span className="font-mono">%36</span></p><UsageBar percent={36} /></div>
                                    <div><p className="mb-1 flex justify-between text-xs text-fg-subtle"><span>Bellek</span><span className="font-mono">%78</span></p><UsageBar percent={78} /></div>
                                    <div><p className="mb-1 flex justify-between text-xs text-fg-subtle"><span>İşlemci</span><span className="font-mono">%94</span></p><UsageBar percent={94} /></div>
                                </div>
                            </Card>
                            <Card title="Son değişiklik" icon={Terminal}>
                                <div className="p-4"><p className="text-sm text-fg-muted">
                                    Güvenlik duvarı rotası kurulamadı: bu sunucu 7.1.8 çekirdeğiyle çalışıyor ve modülleri
                                    diskte yok. Yeniden başlatılana kadar nftables yüklenemez.
                                </p>
                                <p className="mt-3 font-mono text-xs text-fg-subtle">12:04:31 · defter #2 481</p></div>
                            </Card>
                            <Card title="Hız" icon={Zap}>
                                <dl className="space-y-2 p-4 text-sm">
                                    <div className="flex justify-between"><dt className="text-fg-muted">Felaketten dönüş</dt><dd className="font-mono">105 sn</dd></div>
                                    <div className="flex justify-between"><dt className="text-fg-muted">Arşiv yaşı</dt><dd className="font-mono">40,9 sn</dd></div>
                                    <div className="flex justify-between"><dt className="text-fg-muted">Cevapsız sorgu</dt><dd className="font-mono">0 / 2508</dd></div>
                                </dl>
                            </Card>
                        </div>
                    </Section>

                    <Section title="Servisler">
                        <Card>
                            <div className="flex flex-wrap items-center justify-between gap-3 p-4">
                                <SearchInput value={search} onChange={setSearch} placeholder="Servis ara" />
                                <Button variant="primary">Servis kur</Button>
                            </div>
                            <div className="overflow-x-auto px-4 pb-4">
                                <table className="w-full text-sm">
                                    <thead>
                                        <tr className="border-b border-border text-left text-xs uppercase tracking-wider text-fg-subtle">
                                            <th className="py-2 pr-4 font-semibold">Servis</th>
                                            <th className="py-2 pr-4 font-semibold">Kategori</th>
                                            <th className="py-2 pr-4 font-semibold">Sürüm</th>
                                            <th className="py-2 pr-4 font-semibold">Durum</th>
                                            <th className="py-2 font-semibold text-right">İşlem</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {SERVICES.map((s) => (
                                            <tr key={s.name} className="border-b border-border last:border-0">
                                                <td className="py-2.5 pr-4 font-medium">{s.name}</td>
                                                <td className="py-2.5 pr-4 font-mono text-xs uppercase tracking-[0.06em] text-fg-subtle">{s.category}</td>
                                                <td className="py-2.5 pr-4 font-mono text-xs text-fg-muted">{s.version}</td>
                                                <td className="py-2.5 pr-4"><StateBadge state={s.state} /></td>
                                                <td className="py-2.5 text-right"><Button variant="secondary">Aç</Button></td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                        </Card>
                    </Section>

                    <Section title="Düğmeler">
                        <div className="flex flex-wrap items-center gap-3">
                            <Button variant="primary">Kaydet</Button>
                            <Button variant="secondary">Vazgeç</Button>
                            <Button variant="danger">Kaldır</Button>
                            <Button variant="secondary">Ayrıntılar</Button>
                            <Button variant="primary" disabled>Kaydet</Button>
                            <Button variant="primary" disabled><Spinner />Kuruluyor</Button>
                            <Button variant="primary" onClick={() => setDialogOpen(true)}>Pencereyi aç</Button>
                            <Spinner />
                        </div>
                    </Section>

                    <Section title="Form">
                        <Card>
                            <div className="p-4"><FormSection title="Alan adı" description="Yeni alan adını panele bağlayın.">
                                <Field label="Alan adı" hint="Örnek: magazam.com">
                                    <input className={inputClass} defaultValue="magazam.com" />
                                </Field>
                                <Field label="Kök dizin">
                                    <input className={inputClass} defaultValue="/var/www/magazam.com" />
                                </Field>
                                <ToggleRow
                                    label="Sertifikayı otomatik al"
                                    description="Let's Encrypt sertifikası kurulumdan sonra alınır ve yenilenir."
                                    checked={toggle}
                                    onChange={setToggle}
                                />
                                <FormActions>
                                    <Button variant="secondary">Vazgeç</Button>
                                    <Button variant="primary">Alan adını ekle</Button>
                                </FormActions>
                            </FormSection></div>
                        </Card>
                    </Section>

                    <Section title="Boş durum">
                        <Card className="border-0">
                            <EmptyState
                                icon={Globe}
                                title="Henüz alan adı yok"
                                hint="İlk alan adınızı ekleyin; DNS ve sertifika aynı akışta hazırlanır."
                                action={<Button variant="primary">Alan adı ekle</Button>}
                            />
                        </Card>
                    </Section>
                </div>
            </main>

            {dialogOpen && (
                <Dialog title="Servisi kaldır" onClose={() => setDialogOpen(false)}>
                    <p className="text-sm text-fg-muted">
                        Postfix kaldırılacak. Posta kutuları ve yapılandırma dosyaları silinmez; servis
                        durdurulur ve paket kaldırılır.
                    </p>
                    <FormActions>
                        <Button variant="secondary" onClick={() => setDialogOpen(false)}>Vazgeç</Button>
                        <Button variant="danger">Kaldır</Button>
                    </FormActions>
                </Dialog>
            )}
        </div>
    );
}

ReactDOM.createRoot(document.getElementById('root')!).render(
    <I18nProvider>
        <ThemeProvider>
            <Gallery />
        </ThemeProvider>
    </I18nProvider>,
);
