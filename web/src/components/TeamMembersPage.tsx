import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import {
    Globe2,
    Layers,
    Pause,
    Pencil,
    Play,
    Plus,
    Save,
    ShieldCheck,
    Trash2,
    Users,
    X,
} from 'lucide-react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import {
    api,
    ApiResponseError,
    TEAM_CAPABILITIES,
    type TeamCapability,
    type TeamDomainPermission,
    type TeamMember,
    type TeamMemberAccess,
    type TeamMemberDomainScope,
    type TeamMemberStatus,
    type TeamMemberSubscriptionScope,
    type TeamPermissionMode,
    type TeamSubscriptionPermission,
} from '../lib/api';
import { apiErrorText, readApiError, type ApiError } from '../lib/apiError';
import { showToast } from './Toast';
import {
    Button,
    EmptyState,
    ErrorBanner,
    StatusDot,
    inputClass,
} from './ui';
import { PageHeader } from './PageHeader';

type ScopeKind = 'subscription' | 'domain';
type PermissionChoice = TeamPermissionMode | 'none';
type PermissionSelection = Record<string, TeamPermissionMode | undefined>;

interface MemberDraft {
    memberID: number | null;
    username: string;
    email: string;
    password: string;
    status: TeamMemberStatus;
    permissions: PermissionSelection;
}

interface ScopeResource {
    id: number;
    label: string;
}

function emptyDraft(): MemberDraft {
    return {
        memberID: null,
        username: '',
        email: '',
        password: '',
        status: 'active',
        permissions: {},
    };
}

function permissionKey(kind: ScopeKind, scopeID: number, capability: TeamCapability): string {
    return kind + ':' + scopeID + ':' + capability;
}

function draftFromMember(member: TeamMember): MemberDraft {
    const permissions: PermissionSelection = {};
    for (const permission of member.access.subscription_permissions) {
        permissions[permissionKey('subscription', permission.subscription_id, permission.capability)] = permission.mode;
    }
    for (const permission of member.access.domain_permissions) {
        permissions[permissionKey('domain', permission.domain_id, permission.capability)] = permission.mode;
    }
    return {
        memberID: member.id,
        username: member.username,
        email: member.email,
        password: '',
        status: member.status,
        permissions,
    };
}

function accessFromDraft(
    draft: MemberDraft,
    subscriptions: TeamMemberSubscriptionScope[],
    domains: TeamMemberDomainScope[],
): TeamMemberAccess {
    const subscriptionPermissions: TeamSubscriptionPermission[] = [];
    const domainPermissions: TeamDomainPermission[] = [];

    for (const subscription of subscriptions) {
        for (const capability of TEAM_CAPABILITIES) {
            const mode = draft.permissions[permissionKey('subscription', subscription.id, capability)];
            if (mode) {
                subscriptionPermissions.push({
                    subscription_id: subscription.id,
                    capability,
                    mode,
                });
            }
        }
    }
    for (const domain of domains) {
        for (const capability of TEAM_CAPABILITIES) {
            const mode = draft.permissions[permissionKey('domain', domain.id, capability)];
            if (mode) {
                domainPermissions.push({
                    domain_id: domain.id,
                    capability,
                    mode,
                });
            }
        }
    }

    return {
        subscription_permissions: subscriptionPermissions,
        domain_permissions: domainPermissions,
    };
}

function assertMemberScopes(
    members: TeamMember[],
    subscriptions: TeamMemberSubscriptionScope[],
    domains: TeamMemberDomainScope[],
): void {
    const subscriptionIDs = new Set(subscriptions.map((scope) => scope.id));
    const domainIDs = new Set(domains.map((scope) => scope.id));
    for (const member of members) {
        if (
            member.access.subscription_permissions.some((permission) => !subscriptionIDs.has(permission.subscription_id))
            || member.access.domain_permissions.some((permission) => !domainIDs.has(permission.domain_id))
        ) {
            throw new Error('team_member_scope_mismatch');
        }
    }
}

async function normalizeApiError(error: unknown): Promise<ApiError> {
    if (error instanceof ApiResponseError) return readApiError(error.response);
    return { message: '' };
}

function capabilityKey(capability: TeamCapability): TranslationKey {
    return ('team.capability.' + capability) as TranslationKey;
}

export function TeamMembersPage() {
    const { t } = useI18n();
    const [members, setMembers] = useState<TeamMember[]>([]);
    const [subscriptions, setSubscriptions] = useState<TeamMemberSubscriptionScope[]>([]);
    const [domains, setDomains] = useState<TeamMemberDomainScope[]>([]);
    const [draft, setDraft] = useState<MemberDraft | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState<ApiError | null>(null);
    const [busy, setBusy] = useState<string | null>(null);

    const load = async () => {
        setLoading(true);
        setLoadError(null);
        try {
            const [nextMembers, nextSubscriptions, nextDomains] = await Promise.all([
                api.getTeamMembers(),
                api.getTeamMemberSubscriptionScopes(),
                api.getTeamMemberDomainScopes(),
            ]);
            assertMemberScopes(nextMembers, nextSubscriptions, nextDomains);
            setMembers(nextMembers);
            setSubscriptions(nextSubscriptions);
            setDomains(nextDomains);
        } catch (error) {
            setLoadError(await normalizeApiError(error));
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void load();
    }, []);

    const saveDraft = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        if (!draft) return;

        const existing = draft.memberID === null
            ? null
            : members.find((member) => member.id === draft.memberID) ?? null;
        if (
            existing?.status === 'active'
            && draft.status === 'suspended'
            && !window.confirm(t('team.suspendConfirm', { name: draft.username }))
        ) {
            return;
        }

        setBusy('save');
        try {
            const access = accessFromDraft(draft, subscriptions, domains);
            let saved: TeamMember;
            if (draft.memberID === null) {
                saved = await api.createTeamMember({
                    username: draft.username.trim(),
                    email: draft.email.trim(),
                    password: draft.password,
                    status: draft.status,
                    access,
                });
                setMembers((current) => [...current, saved].sort((a, b) => a.username.localeCompare(b.username)));
                showToast('success', t('team.created'));
            } else {
                saved = await api.updateTeamMember(draft.memberID, {
                    username: draft.username.trim(),
                    email: draft.email.trim(),
                    status: draft.status,
                    access,
                    ...(draft.password ? { password: draft.password } : {}),
                });
                setMembers((current) => current.map((member) => member.id === saved.id ? saved : member));
                showToast('success', t('team.updated'));
            }
            setDraft(null);
        } catch (error) {
            showToast('error', apiErrorText(await normalizeApiError(error), t, 'team.saveFailed'));
        } finally {
            setBusy(null);
        }
    };

    const setStatus = async (member: TeamMember, status: TeamMemberStatus) => {
        if (
            status === 'suspended'
            && !window.confirm(t('team.suspendConfirm', { name: member.username }))
        ) {
            return;
        }
        setBusy('member:' + member.id);
        try {
            const updated = await api.updateTeamMember(member.id, { status });
            setMembers((current) => current.map((item) => item.id === member.id ? updated : item));
            showToast('success', t(status === 'active' ? 'team.activated' : 'team.suspended'));
        } catch (error) {
            showToast('error', apiErrorText(await normalizeApiError(error), t, 'team.actionFailed'));
        } finally {
            setBusy(null);
        }
    };

    const remove = async (member: TeamMember) => {
        if (!window.confirm(t('team.deleteConfirm', { name: member.username }))) return;
        setBusy('member:' + member.id);
        try {
            await api.deleteTeamMember(member.id);
            setMembers((current) => current.filter((item) => item.id !== member.id));
            if (draft?.memberID === member.id) setDraft(null);
            showToast('success', t('team.deleted'));
        } catch (error) {
            showToast('error', apiErrorText(await normalizeApiError(error), t, 'team.actionFailed'));
        } finally {
            setBusy(null);
        }
    };

    return (
        <div className='p-6 md:p-8'>
            <PageHeader
                title={t('team.title')}
                subtitle={t('team.subtitle')}
                breadcrumb={[t('common.home'), t('team.title')]}
                actions={(
                    <Button
                        variant='primary'
                        icon={Plus}
                        onClick={() => setDraft(emptyDraft())}
                        disabled={loading || Boolean(draft)}
                    >
                        {t('team.add')}
                    </Button>
                )}
            />

            {loadError && (
                <div className='mb-4 space-y-3'>
                    <ErrorBanner error={loadError} />
                    <Button onClick={() => void load()}>{t('team.retry')}</Button>
                </div>
            )}

            {draft && (
                <MemberEditor
                    draft={draft}
                    subscriptions={subscriptions}
                    domains={domains}
                    saving={busy === 'save'}
                    onChange={setDraft}
                    onCancel={() => setDraft(null)}
                    onSubmit={saveDraft}
                />
            )}

            {!loading && !loadError && (
                <div className='mb-3 text-xs text-fg-subtle'>
                    {t('common.itemsTotal', { n: members.length })}
                </div>
            )}

            {loading ? (
                <div className='flex items-center justify-center py-16' role='status' aria-label={t('common.loading')}>
                    <div className='h-8 w-8 animate-spin rounded-full border-b-2 border-primary' />
                </div>
            ) : loadError ? null : members.length === 0 ? (
                <EmptyState
                    icon={Users}
                    title={t('team.empty')}
                    hint={t('team.emptyHint')}
                    action={(
                        <Button variant='primary' icon={Plus} onClick={() => setDraft(emptyDraft())}>
                            {t('team.add')}
                        </Button>
                    )}
                />
            ) : (
                <MemberTable
                    members={members}
                    busy={busy}
                    onEdit={(member) => setDraft(draftFromMember(member))}
                    onStatus={setStatus}
                    onDelete={remove}
                />
            )}
        </div>
    );
}

function MemberTable({
    members,
    busy,
    onEdit,
    onStatus,
    onDelete,
}: {
    members: TeamMember[];
    busy: string | null;
    onEdit: (member: TeamMember) => void;
    onStatus: (member: TeamMember, status: TeamMemberStatus) => Promise<void>;
    onDelete: (member: TeamMember) => Promise<void>;
}) {
    const { t } = useI18n();
    return (
        <div className='overflow-x-auto rounded-xl border border-border bg-surface shadow-card'>
            <table className='w-full text-sm'>
                <thead>
                    <tr className='border-b border-border text-left text-xs font-semibold text-fg-muted'>
                        <th className='px-4 py-2.5'>{t('team.col.member')}</th>
                        <th className='px-4 py-2.5'>{t('team.col.status')}</th>
                        <th className='px-4 py-2.5'>{t('team.col.access')}</th>
                        <th className='px-4 py-2.5' aria-label={t('team.col.actions')} />
                    </tr>
                </thead>
                <tbody>
                    {members.map((member) => {
                        const memberBusy = busy === 'member:' + member.id;
                        return (
                            <tr key={member.id} className='border-b border-border last:border-0 hover:bg-surface-2/60'>
                                <td className='px-4 py-3'>
                                    <div className='text-base font-medium text-fg'>{member.username}</div>
                                    <div className='text-xs text-fg-subtle'>{member.email}</div>
                                </td>
                                <td className='px-4 py-3'>
                                    <span className='inline-flex items-center gap-1.5 text-fg-muted'>
                                        <StatusDot ok={member.status === 'active'} />
                                        {t(member.status === 'active' ? 'team.status.active' : 'team.status.suspended')}
                                    </span>
                                </td>
                                <td className='px-4 py-3 text-fg-muted'>
                                    <AccessSummary member={member} />
                                </td>
                                <td className='px-4 py-3'>
                                    <div className='flex items-center justify-end gap-0.5'>
                                        <RowButton
                                            title={t('team.edit')}
                                            onClick={() => onEdit(member)}
                                            disabled={Boolean(busy)}
                                        >
                                            <Pencil className='h-4 w-4' />
                                        </RowButton>
                                        {member.status === 'active' ? (
                                            <RowButton
                                                title={t('team.suspend')}
                                                onClick={() => void onStatus(member, 'suspended')}
                                                disabled={memberBusy || Boolean(busy)}
                                            >
                                                <Pause className='h-4 w-4' />
                                            </RowButton>
                                        ) : (
                                            <RowButton
                                                title={t('team.activate')}
                                                onClick={() => void onStatus(member, 'active')}
                                                disabled={memberBusy || Boolean(busy)}
                                            >
                                                <Play className='h-4 w-4' />
                                            </RowButton>
                                        )}
                                        <RowButton
                                            danger
                                            title={t('team.delete')}
                                            onClick={() => void onDelete(member)}
                                            disabled={memberBusy || Boolean(busy)}
                                        >
                                            <Trash2 className='h-4 w-4' />
                                        </RowButton>
                                    </div>
                                </td>
                            </tr>
                        );
                    })}
                </tbody>
            </table>
        </div>
    );
}

function AccessSummary({ member }: { member: TeamMember }) {
    const { t } = useI18n();
    const grants = member.access.subscription_permissions.length + member.access.domain_permissions.length;
    if (grants === 0) return <span>{t('team.noAccess')}</span>;
    const scopes = new Set([
        ...member.access.subscription_permissions.map((permission) => 's:' + permission.subscription_id),
        ...member.access.domain_permissions.map((permission) => 'd:' + permission.domain_id),
    ]).size;
    return <span>{t('team.accessSummary', { grants, scopes })}</span>;
}

function RowButton({
    children,
    title,
    onClick,
    danger,
    disabled,
}: {
    children: ReactNode;
    title: string;
    onClick: () => void;
    danger?: boolean;
    disabled?: boolean;
}) {
    return (
        <button
            type='button'
            title={title}
            aria-label={title}
            onClick={onClick}
            disabled={disabled}
            className={
                'rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-40 '
                + (danger ? 'hover:text-danger' : 'hover:text-fg')
            }
        >
            {children}
        </button>
    );
}

function MemberEditor({
    draft,
    subscriptions,
    domains,
    saving,
    onChange,
    onCancel,
    onSubmit,
}: {
    draft: MemberDraft;
    subscriptions: TeamMemberSubscriptionScope[];
    domains: TeamMemberDomainScope[];
    saving: boolean;
    onChange: (draft: MemberDraft) => void;
    onCancel: () => void;
    onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}) {
    const { t } = useI18n();
    const isNew = draft.memberID === null;
    const passwordValid = isNew
        ? draft.password.length >= 8
        : draft.password.length === 0 || draft.password.length >= 8;
    const canSave = draft.username.trim() !== '' && draft.email.trim() !== '' && passwordValid;
    const subscriptionResources = subscriptions.map((scope) => ({ id: scope.id, label: scope.name }));
    const domainResources = domains.map((scope) => ({ id: scope.id, label: scope.domain_name }));

    const setPermissions = (permissions: PermissionSelection) => {
        onChange({ ...draft, permissions });
    };

    return (
        <form onSubmit={onSubmit} className='mb-6 rounded-xl border border-border bg-surface shadow-card'>
            <div className='flex items-center justify-between border-b border-border px-4 py-3'>
                <div className='flex items-center gap-2 text-sm font-semibold text-fg'>
                    <ShieldCheck className='h-4 w-4 text-primary' />
                    {t(isNew ? 'team.editorCreate' : 'team.editorEdit', { name: draft.username })}
                </div>
                <button
                    type='button'
                    onClick={onCancel}
                    className='rounded-md p-1.5 text-fg-subtle hover:bg-surface-2 hover:text-fg'
                    aria-label={t('team.cancel')}
                >
                    <X className='h-4 w-4' />
                </button>
            </div>

            <div className='space-y-6 p-4'>
                <section>
                    <h3 className='text-sm font-semibold text-fg'>{t('team.identity')}</h3>
                    <p className='mt-0.5 text-xs text-fg-muted'>{t('team.identityHint')}</p>
                    <div className='mt-3 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4'>
                        <label>
                            <span className='mb-1 block text-xs text-fg-muted'>{t('team.username')}</span>
                            <input
                                value={draft.username}
                                onChange={(event) => onChange({ ...draft, username: event.target.value })}
                                className={inputClass}
                                autoComplete='off'
                                required
                                autoFocus
                            />
                        </label>
                        <label>
                            <span className='mb-1 block text-xs text-fg-muted'>{t('team.email')}</span>
                            <input
                                type='email'
                                value={draft.email}
                                onChange={(event) => onChange({ ...draft, email: event.target.value })}
                                className={inputClass}
                                autoComplete='off'
                                required
                            />
                        </label>
                        <label>
                            <span className='mb-1 block text-xs text-fg-muted'>
                                {t(isNew ? 'team.password' : 'team.newPassword')}
                            </span>
                            <input
                                type='password'
                                value={draft.password}
                                onChange={(event) => onChange({ ...draft, password: event.target.value })}
                                className={inputClass}
                                autoComplete='new-password'
                                minLength={8}
                                required={isNew}
                            />
                            <span className='mt-1 block text-xs text-fg-subtle'>
                                {t(isNew ? 'team.passwordHint' : 'team.newPasswordHint')}
                            </span>
                        </label>
                        <label>
                            <span className='mb-1 block text-xs text-fg-muted'>{t('team.status')}</span>
                            <select
                                value={draft.status}
                                onChange={(event) => onChange({
                                    ...draft,
                                    status: event.target.value as TeamMemberStatus,
                                })}
                                className={inputClass}
                            >
                                <option value='active'>{t('team.status.active')}</option>
                                <option value='suspended'>{t('team.status.suspended')}</option>
                            </select>
                        </label>
                    </div>
                    {!isNew && <p className='mt-3 text-xs text-warning'>{t('team.securityNote')}</p>}
                </section>

                <section className='border-t border-border pt-5'>
                    <h3 className='text-sm font-semibold text-fg'>{t('team.accessTitle')}</h3>
                    <p className='mt-0.5 text-xs text-fg-muted'>{t('team.accessHint')}</p>
                    <div className='mt-4 space-y-5'>
                        <PermissionMatrix
                            kind='subscription'
                            icon={<Layers className='h-4 w-4' />}
                            title={t('team.subscriptionTitle')}
                            hint={t('team.subscriptionHint')}
                            emptyText={t('team.noSubscriptions')}
                            resources={subscriptionResources}
                            permissions={draft.permissions}
                            onChange={setPermissions}
                        />
                        <PermissionMatrix
                            kind='domain'
                            icon={<Globe2 className='h-4 w-4' />}
                            title={t('team.domainTitle')}
                            hint={t('team.domainHint')}
                            emptyText={t('team.noDomains')}
                            resources={domainResources}
                            permissions={draft.permissions}
                            onChange={setPermissions}
                        />
                    </div>
                </section>
            </div>

            <div className='flex justify-end gap-2 border-t border-border px-4 py-3'>
                <Button type='button' icon={X} onClick={onCancel} disabled={saving}>
                    {t('team.cancel')}
                </Button>
                <Button type='submit' variant='primary' icon={Save} disabled={saving || !canSave}>
                    {t(isNew ? 'team.create' : 'team.save')}
                </Button>
            </div>
        </form>
    );
}

function PermissionMatrix({
    kind,
    icon,
    title,
    hint,
    emptyText,
    resources,
    permissions,
    onChange,
}: {
    kind: ScopeKind;
    icon: ReactNode;
    title: string;
    hint: string;
    emptyText: string;
    resources: ScopeResource[];
    permissions: PermissionSelection;
    onChange: (permissions: PermissionSelection) => void;
}) {
    const { t } = useI18n();

    const setPermission = (scopeID: number, capability: TeamCapability, choice: PermissionChoice) => {
        const key = permissionKey(kind, scopeID, capability);
        const next = { ...permissions };
        if (choice === 'none') delete next[key];
        else next[key] = choice;
        onChange(next);
    };

    const setResource = (scopeID: number, choice: PermissionChoice) => {
        const next = { ...permissions };
        for (const capability of TEAM_CAPABILITIES) {
            const key = permissionKey(kind, scopeID, capability);
            if (choice === 'none') delete next[key];
            else next[key] = choice;
        }
        onChange(next);
    };

    return (
        <div>
            <div className='flex items-start gap-2'>
                <span className='mt-0.5 text-fg-muted'>{icon}</span>
                <div>
                    <h4 className='text-sm font-medium text-fg'>{title}</h4>
                    <p className='text-xs text-fg-subtle'>{hint}</p>
                </div>
            </div>
            {resources.length === 0 ? (
                <p className='mt-3 rounded-lg border border-dashed border-border px-3 py-4 text-sm text-fg-subtle'>
                    {emptyText}
                </p>
            ) : (
                <div className='mt-3 space-y-3'>
                    {resources.map((resource) => (
                        <div key={resource.id} className='rounded-lg border border-border bg-surface-2/40 p-3'>
                            <div className='flex flex-wrap items-center justify-between gap-2'>
                                <span className='font-medium text-fg'>{resource.label}</span>
                                <div className='flex flex-wrap gap-1'>
                                    <ScopeButton onClick={() => setResource(resource.id, 'view')}>
                                        {t('team.allView')}
                                    </ScopeButton>
                                    <ScopeButton onClick={() => setResource(resource.id, 'manage')}>
                                        {t('team.allManage')}
                                    </ScopeButton>
                                    <ScopeButton onClick={() => setResource(resource.id, 'none')}>
                                        {t('team.clear')}
                                    </ScopeButton>
                                </div>
                            </div>
                            <div className='mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3'>
                                {TEAM_CAPABILITIES.map((capability) => (
                                    <label
                                        key={capability}
                                        className='flex items-center justify-between gap-2 rounded-md bg-surface px-2.5 py-2'
                                    >
                                        <span className='text-xs font-medium text-fg-muted'>
                                            {t(capabilityKey(capability))}
                                        </span>
                                        <select
                                            value={permissions[permissionKey(kind, resource.id, capability)] ?? 'none'}
                                            onChange={(event) => setPermission(
                                                resource.id,
                                                capability,
                                                event.target.value as PermissionChoice,
                                            )}
                                            className='rounded-md border border-border bg-surface px-2 py-1 text-xs text-fg outline-none focus:border-primary focus:ring-2 focus:ring-primary/30'
                                            aria-label={resource.label + ' — ' + t(capabilityKey(capability))}
                                        >
                                            <option value='none'>{t('team.mode.none')}</option>
                                            <option value='view'>{t('team.mode.view')}</option>
                                            <option value='manage'>{t('team.mode.manage')}</option>
                                        </select>
                                    </label>
                                ))}
                            </div>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

function ScopeButton({ children, onClick }: { children: ReactNode; onClick: () => void }) {
    return (
        <button
            type='button'
            onClick={onClick}
            className='rounded-md border border-border bg-surface px-2 py-1 text-xs text-fg-muted hover:border-border-strong hover:text-fg'
        >
            {children}
        </button>
    );
}
