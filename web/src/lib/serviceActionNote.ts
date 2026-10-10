import type { ApiError } from './apiError';

// A successful service action can leave a fact its success does not say
// (12 Oct 2026; cmd/panel/service_action_outcome.go): a Stop after which
// systemd shows the unit as failed, because a command of the unit exited with
// an error while it stopped (Postfix with a main.cf it refuses). The server
// leaves that mark, which is systemd's own record, and sends a `note` in the
// shape of the other explained answers: `code` SERVICE_ACTION_NOTE, `reason`,
// `error` (its own sentence) and `vars`. It is not a failure.
//
// Başarılı bir hizmet işlemi, başarının söylemediği bir bilgi bırakabilir:
// systemd'nin birimi `failed` gösterdiği bir Durdur. Sunucu o işareti bırakır
// ve bir `note` gönderir. Hata değildir.
export function readServiceActionNote(body: unknown): ApiError | null {
    if (!body || typeof body !== 'object') return null;
    const note = (body as { note?: unknown }).note;
    if (!note || typeof note !== 'object') return null;
    const { code, reason, error, vars } = note as Record<string, unknown>;
    if (code !== 'SERVICE_ACTION_NOTE') return null;
    const kept: Record<string, string> = {};
    if (vars && typeof vars === 'object' && !Array.isArray(vars)) {
        for (const [name, value] of Object.entries(vars)) if (typeof value === 'string') kept[name] = value;
    }
    return {
        message: typeof error === 'string' ? error : '',
        code,
        reason: typeof reason === 'string' && reason ? reason : undefined,
        vars: kept,
    };
}
