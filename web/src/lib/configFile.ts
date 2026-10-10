import { useCallback, useEffect, useState } from 'react';
import { api } from './api';
import { readApiError, type ApiError } from './apiError';
import { useRemote, type Remote } from './remote';

// A managed configuration file as the server holds it, and the one way a
// screen writes it back (9 Oct 2026; D-022, D-024).
//
// The read answers the file's text with the version of the exact bytes read.
// A write must carry that version: the server refuses it, before anything is
// written, when the write carries none or when the file changed since. A file
// that could not be read is never an empty file here: the decoder throws on an
// answer without a version, so the screen is "could not be read", has no
// editor and cannot save.
//
// Before this the three database editors opened on nothing after a failed
// read, with Save enabled. Save only failed because it posted the file as
// text/plain to a handler that reads JSON, so the editors did not work at all.
//
// Sunucunun tuttuğu hâliyle yönetilen bir yapılandırma dosyası ve bir ekranın
// onu geri yazmasının tek yolu. Okuma, dosyanın metnini okunan baytların
// sürümüyle yanıtlar; yazı o sürümü taşımak zorundadır. Okunamayan dosya burada
// asla boş dosya değildir.

export interface ConfigFile {
    content: string;
    /** Identifies the exact bytes that were read; every save carries it back. */
    version: string;
}

/** What the server did with a save it accepted. */
export interface ConfigSaved {
    version: string;
    /** The file already held this content; nothing was written. */
    unchanged: boolean;
    /** The copy of the previous file kept next to it on the server. */
    backup: string;
    /** 'reloaded' | 'restart_required' | 'not_running' | '' */
    applied: string;
    /** 'accepted' | 'not_checked' | '' */
    daemonCheck: string;
    /** Settings the service takes up only when it is restarted. */
    restartRequired: string[];
}

export const configFileURL = (path: string) => `/api/v1/config?path=${encodeURIComponent(path)}`;

// decodeConfigFile is the decoder of the configuration read. An answer without
// text or without a version is not the contract and is unknown.
// decodeConfigFile, yapılandırma okumasının çözücüsüdür.
export function decodeConfigFile(raw: unknown): ConfigFile {
    const answer = raw as { Content?: unknown; Version?: unknown } | null;
    if (!answer || typeof answer.Content !== 'string' || typeof answer.Version !== 'string' || !answer.Version) {
        throw new Error('config file');
    }
    return { content: answer.Content, version: answer.Version };
}

export type ConfigSaveResult = { ok: true; saved: ConfigSaved } | { ok: false; error: ApiError };

// saveConfigFile never throws: a refused, failed or dropped save is an error
// the screen shows, with what was typed still on it.
// saveConfigFile hata fırlatmaz.
export async function saveConfigFile(path: string, content: string, version: string): Promise<ConfigSaveResult> {
    try {
        const res = await api.saveConfig(path, content, version);
        if (!res.ok) return { ok: false, error: await readApiError(res) };
        const body = (await res.json()) as Record<string, unknown>;
        if (body.success !== true || typeof body.version !== 'string' || !body.version) {
            return { ok: false, error: { message: '' } };
        }
        return {
            ok: true,
            saved: {
                version: body.version,
                unchanged: body.unchanged === true,
                backup: typeof body.backup === 'string' ? body.backup : '',
                applied: typeof body.applied === 'string' ? body.applied : '',
                daemonCheck: typeof body.daemon_check === 'string' ? body.daemon_check : '',
                restartRequired: Array.isArray(body.restart_required)
                    ? body.restart_required.filter((item): item is string => typeof item === 'string')
                    : [],
            },
        };
    } catch {
        return { ok: false, error: { message: '' } };
    }
}

// The server refused a write because the file is no longer the one this page
// loaded (or the page never loaded it). Nothing was saved.
// Sunucu yazıyı reddetti: dosya artık bu sayfanın yüklediği dosya değil.
export const isStaleConfigWrite = (error: ApiError) =>
    error.code === 'SETTINGS_CHANGED' || error.code === 'SETTINGS_VERSION_REQUIRED';

export interface ConfigFileHandle {
    remote: Remote<ConfigFile>;
    reading: boolean;
    saving: boolean;
    /** The save was refused because the file changed; what was typed stays. */
    stale: boolean;
    /** Any other refusal or failure of the last save. */
    refusal: ApiError | null;
    /** What the server did with the last accepted save. */
    saved: ConfigSaved | null;
    /** Reads the file again and forgets the notices of the last save. */
    reload: () => Promise<void>;
    /** Sends `content` with the version that was read. Resolves true when saved. */
    save: (content: string) => Promise<boolean>;
    /** Forgets a refusal once the person starts correcting what it named. */
    clearRefusal: () => void;
}

// useConfigFile is the state every configuration editor shares: the read in
// its three states, and a save that carries the version, keeps what was typed
// when it is refused and reads the file again when it is accepted.
// useConfigFile, her yapılandırma düzenleyicisinin paylaştığı durumdur.
export function useConfigFile(path: string): ConfigFileHandle {
    const { remote, reading, retry } = useRemote(configFileURL(path), decodeConfigFile);
    const [saving, setSaving] = useState(false);
    const [stale, setStale] = useState(false);
    const [refusal, setRefusal] = useState<ApiError | null>(null);
    const [saved, setSaved] = useState<ConfigSaved | null>(null);

    useEffect(() => {
        setStale(false);
        setRefusal(null);
        setSaved(null);
    }, [path]);

    const reload = useCallback(async () => {
        setStale(false);
        setRefusal(null);
        setSaved(null);
        await retry();
    }, [retry]);

    const save = useCallback(async (content: string) => {
        if (remote.state !== 'known') return false;
        setSaving(true);
        setRefusal(null);
        setSaved(null);
        const result = await saveConfigFile(path, content, remote.value.version);
        if (result.ok) {
            // The file is read again, so the editor stands on what the server
            // holds now and the next save carries its version.
            // Dosya yeniden okunur; düzenleyici sunucunun şimdi tuttuğu hâlin
            // üzerinde durur.
            await retry();
            setSaved(result.saved);
        } else if (isStaleConfigWrite(result.error)) {
            setStale(true);
        } else {
            setRefusal(result.error);
            // After a reload that failed the server put the previous file back,
            // or could not: either way what is on disk is read again.
            // Başarısız yeniden yüklemeden sonra diskteki dosya yeniden okunur.
            if (result.error.code === 'CONFIG_RELOAD_FAILED' && result.error.reason === 'not_restored') await retry();
        }
        setSaving(false);
        return result.ok;
    }, [path, remote, retry]);

    const clearRefusal = useCallback(() => setRefusal(null), []);

    return { remote, reading, saving, stale, refusal, saved, reload, save, clearRefusal };
}
