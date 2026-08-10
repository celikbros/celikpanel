import { useState, useEffect } from 'react';
import { ShieldCheck, KeyRound, FileCheck2, Copy, Plus, Info, type LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Button, StatusDot, inputClass } from './ui';

interface AuthRecord {
    name: string;
    recommended: string;
    zone_value: string;
    dns_value: string;
    resolved: boolean;
    status: 'ok' | 'pending' | 'missing' | 'no_key';
}

interface AuthStatus {
    domain: string;
    zone_exists: boolean;
    spf: AuthRecord;
    dkim: AuthRecord;
    dmarc: AuthRecord;
    dkim_selector: string;
    signing_installed: boolean;
}

interface MailAuthPanelProps {
    domainId: number;
    readOnly?: boolean;
}

// Mail authentication (roadmap 3C): one card per record. The status is
// derived server-side from the zone AND a live DNS lookup, so "verified"
// means the world can actually see it — never an assumption.
//
// E-posta kimlik doğrulaması (yol haritası 3C): kayıt başına bir kart. Durum,
// sunucu tarafında zone'dan VE canlı DNS sorgusundan türetilir; "doğrulandı"
// demek dünyanın onu gerçekten görebildiği demektir — asla varsayım değil.
export function MailAuthPanel({ domainId, readOnly = false }: MailAuthPanelProps) {
    const { t } = useI18n();
    const [status, setStatus] = useState<AuthStatus | null>(null);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState<string | null>(null);
    const [dmarcPolicy, setDmarcPolicy] = useState<'none' | 'quarantine' | 'reject'>('none');

    const load = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/auth`);
            if (!res.ok) throw new Error();
            setStatus(await res.json());
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        setLoading(true);
        load();
    }, [domainId]);

    const generateKey = async () => {
        if (readOnly) return;
        setBusy('dkim-key');
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/auth/dkim`, { method: 'POST' });
            if (!res.ok) throw new Error();
            showToast('success', t('mailauth.keyGenerated'));
            await load();
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(null);
        }
    };

    const apply = async (record: 'spf' | 'dkim' | 'dmarc') => {
        if (readOnly) return;
        setBusy(record);
        try {
            const body: Record<string, string> = { record };
            if (record === 'dmarc') body.dmarc_policy = dmarcPolicy;
            const res = await fetch(`/api/v1/domains/${domainId}/mail/auth/apply`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('mailauth.applied'));
            await load();
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(null);
        }
    };

    const copy = (value: string) => {
        navigator.clipboard?.writeText(value).then(() => showToast('success', t('mailauth.copied')));
    };

    if (loading || !status) {
        return (
            <div className="flex items-center justify-center py-12">
                <div className="h-7 w-7 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }

    const anyUnresolved = !status.spf.resolved || !status.dmarc.resolved;

    return (
        <div className="space-y-4">
            <p className="text-sm text-fg-muted">{t('mailauth.intro')}</p>

            {!status.signing_installed && status.dkim.status !== 'no_key' && (
                <Note>{t('mailauth.signingMissing')}</Note>
            )}
            {anyUnresolved && <Note>{t('mailauth.unresolvedNote')}</Note>}

            <RecordCard
                icon={ShieldCheck}
                title="SPF"
                descKey="mailauth.spfDesc"
                record={status.spf}
                busy={busy === 'spf'}
                readOnly={readOnly}
                onApply={() => apply('spf')}
                onCopy={copy}
            />

            <RecordCard
                icon={KeyRound}
                title="DKIM"
                descKey="mailauth.dkimDesc"
                record={status.dkim}
                busy={busy === 'dkim'}
                readOnly={readOnly}
                onApply={() => apply('dkim')}
                onCopy={copy}
                extraAction={
                    !readOnly && status.dkim.status === 'no_key' ? (
                        <Button variant="primary" icon={KeyRound} onClick={generateKey} disabled={busy === 'dkim-key'}>
                            {t('mailauth.generateKey')}
                        </Button>
                    ) : undefined
                }
            />

            <RecordCard
                icon={FileCheck2}
                title="DMARC"
                descKey="mailauth.dmarcDesc"
                record={status.dmarc}
                busy={busy === 'dmarc'}
                readOnly={readOnly}
                onApply={() => apply('dmarc')}
                onCopy={copy}
                extraControls={!readOnly ? (
                    <label className="flex items-center gap-2 text-sm text-fg-muted">
                        {t('mailauth.dmarcPolicy')}
                        <select
                            value={dmarcPolicy}
                            onChange={(e) => setDmarcPolicy(e.target.value as typeof dmarcPolicy)}
                            className={`${inputClass} w-auto`}
                        >
                            <option value="none">{t('mailauth.policy.none')}</option>
                            <option value="quarantine">{t('mailauth.policy.quarantine')}</option>
                            <option value="reject">{t('mailauth.policy.reject')}</option>
                        </select>
                    </label>
                ) : undefined}
            />
        </div>
    );
}

function statusTone(status: AuthRecord['status']): { ok: boolean; labelKey: TranslationKey; cls: string } {
    switch (status) {
        case 'ok':
            return { ok: true, labelKey: 'mailauth.status.ok', cls: 'text-success' };
        case 'pending':
            return { ok: false, labelKey: 'mailauth.status.pending', cls: 'text-warning' };
        case 'no_key':
            return { ok: false, labelKey: 'mailauth.status.no_key', cls: 'text-fg-subtle' };
        default:
            return { ok: false, labelKey: 'mailauth.status.missing', cls: 'text-fg-subtle' };
    }
}

function RecordCard({
    icon: Icon,
    title,
    descKey,
    record,
    busy,
    readOnly,
    onApply,
    onCopy,
    extraAction,
    extraControls,
}: {
    icon: LucideIcon;
    title: string;
    descKey: TranslationKey;
    record: AuthRecord;
    busy: boolean;
    readOnly: boolean;
    onApply: () => void;
    onCopy: (v: string) => void;
    extraAction?: React.ReactNode;
    extraControls?: React.ReactNode;
}) {
    const { t } = useI18n();
    const tone = statusTone(record.status);
    const showValue = record.recommended !== '';

    return (
        <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
            <div className="mb-3 flex flex-wrap items-center gap-3">
                <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="h-5 w-5" />
                </span>
                <div className="min-w-0 flex-1">
                    <h3 className="text-sm font-semibold text-fg">{title}</h3>
                    <p className="text-xs text-fg-muted">{t(descKey)}</p>
                </div>
                <span className={`inline-flex items-center gap-1.5 text-sm font-medium ${tone.cls}`}>
                    <StatusDot ok={tone.ok} />
                    {t(tone.labelKey)}
                </span>
            </div>

            {showValue && (
                <div className="mb-3 space-y-2">
                    <div className="flex items-center gap-2 text-xs text-fg-subtle">
                        <span>{t('mailauth.recordName')}:</span>
                        <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-fg-muted">{record.name}</code>
                        <span className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-fg-muted">TXT</span>
                    </div>
                    <div className="flex items-start gap-2">
                        <p className="min-w-0 flex-1 break-all rounded-lg border border-border bg-surface-2/60 px-3 py-2 font-mono text-xs text-fg-muted">
                            {record.recommended}
                        </p>
                        <button
                            onClick={() => onCopy(record.recommended)}
                            title={t('mailauth.copy')}
                            className="mt-0.5 rounded-md p-2 text-fg-muted hover:bg-surface-2 hover:text-fg"
                        >
                            <Copy className="h-4 w-4" />
                        </button>
                    </div>
                </div>
            )}

            <div className="flex flex-wrap items-center justify-between gap-3">
                <div>{extraControls}</div>
                <div className="flex items-center gap-2">
                    {extraAction}
                    {!readOnly && record.status !== 'no_key' && record.status !== 'ok' && (
                        <Button variant="primary" icon={Plus} onClick={onApply} disabled={busy}>
                            {t('mailauth.apply')}
                        </Button>
                    )}
                </div>
            </div>
        </div>
    );
}

function Note({ children }: { children: React.ReactNode }) {
    return (
        <p className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-fg-muted">
            <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
            {children}
        </p>
    );
}
