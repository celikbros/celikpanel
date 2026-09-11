import { useCallback, useEffect, useRef, useState } from 'react';
import { useAuth } from '../auth/AuthContext';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Button, inputClass } from './ui';
import { decodeDNSClients, decodeDNSConnection, decodeDNSConnections, remoteDNSEndpoint, remoteDNSIdentity, remoteDNSPost, remoteDNSRequest, RemoteDNSError, type DNSClient, type DNSConnection } from '../lib/remoteDNS';

const remoteErrorKey = (error: unknown): TranslationKey => error instanceof RemoteDNSError && error.code === 'REMOTE_DNS_AUTHORITY_NOT_READY' ? 'setup.remote.authorityNotReady' : 'setup.remote.failed';

export function ServerSetupDNSConnection({ value, onChange, onValidityChange }: { value: string; onChange: (id: string) => void; onValidityChange?: (valid: boolean) => void }) {
    const { role } = useAuth();
    const { t } = useI18n();
    const [connections, setConnections] = useState<DNSConnection[] | null>(null);
    const [endpoint, setEndpoint] = useState('');
    const [code, setCode] = useState('');
    const [confirmed, setConfirmed] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<TranslationKey | null>(null);
    const [proof, setProof] = useState<'idle' | 'checking' | 'verified' | 'failed'>('idle');
    const [proofAttempt, setProofAttempt] = useState(0);
    const [proofNameservers, setProofNameservers] = useState<string[]>([]);
    const [remove, setRemove] = useState<DNSConnection | null>(null);
    const alive = useRef(true);
    const inFlight = useRef(false);
    const load = useCallback(async () => {
        try {
            const list = decodeDNSConnections(await remoteDNSRequest('/api/v1/dns/remote/connections'));
            if (!list) throw new Error('connections');
            if (alive.current) { setConnections(list); setError(null); }
            return list;
        } catch (failure) { if (alive.current) setError(remoteErrorKey(failure)); return null; }
    }, []);
    useEffect(() => {
        alive.current = true;
        if (role === 'admin') void load();
        return () => { alive.current = false; };
    }, [load, role]);
    useEffect(() => {
        const controller = new AbortController();
        let current = true;
        onValidityChange?.(false); setProofNameservers([]);
        if (role !== 'admin' || !remoteDNSIdentity(value)) { setProof('idle'); return () => controller.abort(); }
        setProof('checking');
        void remoteDNSRequest(`/api/v1/dns/remote/connections?id=${encodeURIComponent(value)}&check=1`, undefined, controller.signal)
            .then(payload => {
                const result = payload as { connection?: unknown; verified?: unknown } | null;
                const connection = decodeDNSConnection(result?.connection);
                const valid = result?.verified === true && connection?.id === value && connection.status === 'ready';
                if (current) { setProof(valid ? 'verified' : 'failed'); setProofNameservers(valid && connection ? connection.nameservers : []); onValidityChange?.(valid); }
            }).catch(() => { if (current) { setProof('failed'); onValidityChange?.(false); } });
        return () => { current = false; controller.abort(); };
    }, [value, role, proofAttempt, onValidityChange]);
    async function connect(id?: string) {
        const target = remoteDNSEndpoint(endpoint);
        if (inFlight.current || (!id && (!target || !code.trim() || !confirmed || connections === null))) return;
        inFlight.current = true; setBusy(true); setError(null);
        try {
            const payload = await remoteDNSRequest('/api/v1/dns/remote/connections', remoteDNSPost(id ? { id } : { endpoint: target, enrollment_code: code.trim() }));
            const connection = decodeDNSConnection((payload as { connection?: unknown } | null)?.connection);
            if (!connection || (id && connection.id !== id)) throw new Error('connection identity');
            if (alive.current) { setCode(''); setConfirmed(false); if (connection.status === 'ready') onChange(connection.id); }
            await load();
        } catch (failure) {
            const list = await load();
            // Reconcile the saved identity before allowing a replacement grant.
            // Yeni yetki vermeden önce kaydedilen kimliği uzlaştır.
            const saved = list?.find(item => id ? item.id === id : remoteDNSEndpoint(item.endpoint) === target);
            if (alive.current) {
                if (saved?.status === 'ready') { onChange(saved.id); setCode(''); setConfirmed(false); }
                else { if (saved?.status === 'pending') { setCode(''); setConfirmed(false); } setError(remoteErrorKey(failure)); }
            }
        } finally { inFlight.current = false; if (alive.current) setBusy(false); }
    }
    async function disconnect() {
        if (!remove || inFlight.current) return;
        inFlight.current = true; setBusy(true);
        try {
            await remoteDNSRequest(`/api/v1/dns/remote/connections?id=${encodeURIComponent(remove.id)}`, { method: 'DELETE' });
            if (alive.current) { if (value === remove.id) onChange(''); setRemove(null); }
            await load();
        } catch (failure) { if (alive.current) setError(remoteErrorKey(failure)); }
        finally { inFlight.current = false; if (alive.current) setBusy(false); }
    }
    if (role !== 'admin') return null;
    const ready = connections?.filter(item => item.status === 'ready') || [];
    const pending = connections?.filter(item => item.status === 'pending') || [];
    const selected = ready.find(item => item.id === value);
    const target = remoteDNSEndpoint(endpoint);
    return <fieldset className="space-y-4" disabled={busy}>
        <legend className="mb-3 font-semibold">{t('setup.remote.title')}</legend>
        <p className="text-sm leading-6 text-fg-muted">{t('setup.remote.help')}</p>
        {error && <div role="alert" className="space-y-3 text-sm text-danger"><p>{t(error)}</p><Button type="button" variant="secondary" onClick={() => void load()}>{t('common.retry')}</Button></div>}
        <label className="block" htmlFor="setup-remote-connection"><span className="mb-2 block text-sm font-medium">{t('setup.remote.choose')}</span><select id="setup-remote-connection" className={`${inputClass} w-full`} value={value} required onChange={event => onChange(event.target.value)}><option value="">{t('setup.remote.choose')}</option>{value && !selected && <option value={value} disabled>{t('setup.remote.selectionUnavailable')}</option>}{ready.map(connection => <option key={connection.id} value={connection.id}>{connection.endpoint}</option>)}</select></label>
        {proof !== 'idle' && <div className="space-y-2 text-sm" role={proof === 'failed' ? 'alert' : 'status'}><p className={proof === 'failed' ? 'text-danger' : 'text-fg-muted'}>{t(`setup.remote.proof.${proof}`)}</p>{proof === 'failed' && <Button type="button" variant="secondary" onClick={() => setProofAttempt(attempt => attempt + 1)}>{t('common.retry')}</Button>}{proof === 'verified' && selected && <p className="break-words text-fg-muted">{proofNameservers.join(', ')}</p>}</div>}
        {selected && <Button type="button" variant="secondary" onClick={() => setRemove(selected)}>{t('setup.remote.disconnect')}</Button>}
        {pending.map(connection => <div key={connection.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-surface-2 p-4"><p className="min-w-0 break-all text-sm">{connection.endpoint}<span className="mt-1 block text-fg-muted">{t('setup.remote.pending')}</span></p><div className="flex gap-2"><Button type="button" variant="secondary" onClick={() => void connect(connection.id)}>{t('setup.remote.resume')}</Button><Button type="button" variant="secondary" onClick={() => setRemove(connection)}>{t('setup.remote.disconnect')}</Button></div></div>)}
        {remove && <div className="rounded-lg border border-danger/30 p-4 text-sm" role="group" aria-label={t('setup.remote.disconnect')}><p>{t('setup.remote.disconnectConfirm', { endpoint: remove.endpoint })}</p><div className="mt-3 flex flex-wrap gap-3"><Button type="button" variant="danger" onClick={() => void disconnect()}>{t('setup.remote.disconnect')}</Button><Button type="button" variant="secondary" onClick={() => setRemove(null)}>{t('common.cancel')}</Button></div></div>}
        <details className="rounded-lg border border-border p-4" open={ready.length === 0 && pending.length === 0}>
            <summary className="cursor-pointer text-sm font-semibold text-primary">{t('setup.remote.connect')}</summary>
            <div className="mt-4 space-y-4"><label className="block" htmlFor="setup-remote-endpoint"><span className="mb-2 block text-sm font-medium">{t('setup.remote.endpoint')}</span><input id="setup-remote-endpoint" type="url" value={endpoint} onChange={event => { setEndpoint(event.target.value); setConfirmed(false); }} placeholder="https://dns-panel.example.com:2083" spellCheck={false} autoCapitalize="none" className={`${inputClass} w-full`} /></label>
                <label className="block" htmlFor="setup-remote-code"><span className="mb-2 block text-sm font-medium">{t('setup.remote.code')}</span><input id="setup-remote-code" type="password" value={code} onChange={event => { setCode(event.target.value); setConfirmed(false); }} autoComplete="off" spellCheck={false} autoCapitalize="none" className={`${inputClass} w-full`} /></label>
                <label className="flex items-start gap-3 text-sm leading-6"><input type="checkbox" checked={confirmed} disabled={!target || !code.trim()} onChange={event => setConfirmed(event.target.checked)} className="mt-1 h-4 w-4 shrink-0 accent-primary" /><span>{t('setup.remote.connectConfirm', { endpoint: target || t('setup.remote.endpoint') })}</span></label>
                <Button type="button" variant="secondary" disabled={busy || connections === null || !!error || !target || !code.trim() || !confirmed} onClick={() => void connect()}>{t('setup.remote.authorize')}</Button>
            </div>
        </details>
    </fieldset>;
}

export function ServerSetupDNSAccess() {
    const { role } = useAuth();
    const { t } = useI18n();
    const [clients, setClients] = useState<DNSClient[] | null>(null);
    const [grant, setGrant] = useState<{ enrollment_code: string; expires_at: string } | null>(null);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<TranslationKey | null>(null);
    const [copied, setCopied] = useState(false);
    const [confirmGrant, setConfirmGrant] = useState(false);
    const [remove, setRemove] = useState<DNSClient | null>(null);
    const alive = useRef(true);
    const inFlight = useRef(false);
    const load = useCallback(async () => {
        try {
            const list = decodeDNSClients(await remoteDNSRequest('/api/v1/dns/remote/clients'));
            if (!list) throw new Error('clients');
            if (alive.current) { setClients(list); setError(null); }
        } catch (failure) { if (alive.current) setError(remoteErrorKey(failure)); }
    }, []);
    useEffect(() => { alive.current = true; if (role === 'admin') void load(); return () => { alive.current = false; }; }, [load, role]);
    async function generate() {
        if (!confirmGrant || clients === null || inFlight.current) return;
        inFlight.current = true; setBusy(true); setError(null); setCopied(false);
        try {
            const value = await remoteDNSRequest('/api/v1/dns/remote/enrollments', remoteDNSPost({})) as { enrollment_code?: unknown; expires_at?: unknown } | null;
            if (typeof value?.enrollment_code !== 'string' || !value.enrollment_code || typeof value.expires_at !== 'string' || !Number.isFinite(Date.parse(value.expires_at))) throw new Error('enrollment');
            if (alive.current) { setGrant({ enrollment_code: value.enrollment_code, expires_at: value.expires_at }); setConfirmGrant(false); }
        } catch (failure) { if (alive.current) setError(remoteErrorKey(failure)); }
        finally { inFlight.current = false; if (alive.current) setBusy(false); }
    }
    async function copyGrant() {
        if (!grant) return;
        try { await navigator.clipboard.writeText(grant.enrollment_code); if (alive.current) setCopied(true); }
        catch { if (alive.current) setError('setup.remote.copyFailed'); }
    }
    async function revoke() {
        if (!remove || inFlight.current) return;
        inFlight.current = true; setBusy(true);
        try {
            await remoteDNSRequest(`/api/v1/dns/remote/clients?id=${encodeURIComponent(remove.id)}`, { method: 'DELETE' });
            if (alive.current) setRemove(null);
            await load();
        } catch (failure) { if (alive.current) setError(remoteErrorKey(failure)); }
        finally { inFlight.current = false; if (alive.current) setBusy(false); }
    }
    if (role !== 'admin') return null;
    return <section className="mt-6 rounded-xl border border-border bg-surface p-5 sm:p-6" aria-labelledby="dns-publishing-access-title">
        <h2 id="dns-publishing-access-title" className="text-lg font-semibold">{t('setup.remote.accessTitle')}</h2><p className="mt-2 max-w-3xl text-sm leading-6 text-fg-muted">{t('setup.remote.accessHelp')}</p>
        {error && <div role="alert" className="mt-4 space-y-3 text-sm text-danger"><p>{t(error)}</p><Button type="button" variant="secondary" onClick={() => void load()}>{t('common.retry')}</Button></div>}
        {grant ? <div className="mt-5 space-y-3"><label className="block" htmlFor="dns-enrollment-code"><span className="mb-2 block text-sm font-medium">{t('setup.remote.code')}</span><input id="dns-enrollment-code" type="password" readOnly value={grant.enrollment_code} className={`${inputClass} w-full`} autoComplete="off" /></label><p className="text-sm text-fg-muted">{t('setup.remote.expires', { date: grant.expires_at })}</p><div className="flex gap-3"><Button type="button" variant="primary" disabled={busy} onClick={() => void copyGrant()}>{t(copied ? 'conn.copied' : 'conn.copy')}</Button><Button type="button" variant="secondary" onClick={() => { setGrant(null); setCopied(false); }}>{t('setup.remote.hideCode')}</Button></div></div>
            : <div className="mt-5 space-y-4"><label className="flex items-start gap-3 text-sm leading-6"><input type="checkbox" checked={confirmGrant} disabled={busy} onChange={event => setConfirmGrant(event.target.checked)} className="mt-1 h-4 w-4 shrink-0 accent-primary" /><span>{t('setup.remote.grantConfirm')}</span></label><Button type="button" variant="primary" disabled={busy || clients === null || !confirmGrant} onClick={() => void generate()}>{t('setup.remote.generate')}</Button></div>}
        {!!clients?.length && <ul className="mt-6 divide-y divide-border">{clients.map(client => <li key={client.id} className="space-y-3 py-4 text-sm"><div className="flex flex-wrap items-center justify-between gap-3"><span className="break-all">{client.label || t('setup.remote.unnamedClient')}</span>{client.revoked ? <span className="text-fg-muted">{t('setup.remote.revoked')}</span> : <Button type="button" variant="secondary" disabled={busy} onClick={() => setRemove(client)}>{t('setup.remote.revoke')}</Button>}</div>{remove?.id === client.id && <div className="rounded-lg border border-danger/30 p-4"><p>{t('setup.remote.revokeConfirm')}</p><div className="mt-3 flex flex-wrap gap-3"><Button type="button" variant="danger" disabled={busy} onClick={() => void revoke()}>{t('setup.remote.revoke')}</Button><Button type="button" variant="secondary" disabled={busy} onClick={() => setRemove(null)}>{t('common.cancel')}</Button></div></div>}</li>)}</ul>}
    </section>;
}


export function ServerSetupDNSManagement() {
    const { role } = useAuth();
    const { t } = useI18n();
    const [selected, setSelected] = useState('');
    if (role !== 'admin') return null;
    return <section className="mt-6 rounded-xl border border-border bg-surface p-5 sm:p-6" aria-labelledby="dns-outgoing-title"><h2 id="dns-outgoing-title" className="text-lg font-semibold">{t('setup.remote.managementTitle')}</h2><p className="mt-2 text-sm leading-6 text-fg-muted">{t('setup.remote.managementHelp')}</p><details className="mt-4"><summary className="cursor-pointer text-sm font-semibold text-primary">{t('setup.remote.manageConnections')}</summary><div className="mt-5"><ServerSetupDNSConnection value={selected} onChange={setSelected} /></div></details></section>;
}
