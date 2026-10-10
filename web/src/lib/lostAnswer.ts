import { useCallback, useEffect, useRef, useState } from 'react';
import type { Known, Remote } from './remote';
import { answerWasLost, isReaskRoute } from './requestIdentity';

// A change whose answer did not arrive (D-024: an unknown result is neither a
// failure nor a success). Most changes the panel sends carry no identity the
// server keeps: adding a DNS record, an alias or a team member a second time
// makes a second one. So when the connection drops, or a gateway
// answers in place of the Panel, the screen does not know what happened and
// must not behave as if it did:
//
//   - nothing is sent a second time;
//   - what the change acts on is read again, and only read;
//   - every control that changes or removes stays off until that read has
//     answered (`holding`), so the person repeats nothing blind;
//   - the notice stays on screen after the read, saying when the list was read
//     again, until the person closes it or a later change is answered.
//
// This is not idempotency and does not pretend to be: whether the change was
// made is decided by the person looking at the re-read state.
//
// A form may add one thing (10 Oct 2026): a `Question` that looks at the state
// that was read again, and only at it. When that state shows the change, the
// form that sent it is closed or cleared and the notice says it was saved, so
// the same record is not one press away from being saved twice. When it does
// not show the change, what was typed stays and the notice says so, without
// calling the change failed: a server that was still working when the
// connection dropped can finish later, and "Check again" asks the question
// again. A state that cannot show the change answers nothing, and the notice
// stays as it was.
//
// Yanıtı gelmeyen değişiklik (D-024: bilinmeyen sonuç ne hatadır ne başarı).
// Panelin gönderdiği değişikliklerin çoğu, sunucunun sakladığı bir kimlik
// taşımaz; ikinci kez gönderilirse ikinci kez yapılır. Bağlantı koptuğunda
// ekran ne olduğunu bilmez: hiçbir şey ikinci kez gönderilmez, değişikliğin
// etkilediği şey yalnızca yeniden okunur, o okuma yanıtlanana dek değiştiren
// ya da kaldıran her denetim kapalı kalır (`holding`) ve bildirim, kişi
// kapatana ya da sonraki bir değişiklik yanıtlanana dek ekranda durur.
// Bir form buna bir `Question` ekleyebilir: yalnız yeniden okunan duruma bakar.
// O durum değişikliği gösteriyorsa form kapatılır ya da temizlenir ve bildirim
// kaydedildiğini söyler; göstermiyorsa yazılan kalır ve bildirim bunu söyler.
//
// Eight routes do carry an identity the server keeps (D-029, 10 Oct 2026; see
// lib/requestIdentity.ts). On those the one fetch interceptor has already asked
// once more for the same answer, under the same identity, before this hook
// hears that none arrived. The change still ran at most once, and the notice
// says what happened: the cause is `asked`, not `dropped`. On those routes the
// Panel itself can also answer that the result is not known: it restarted
// while the change ran (`interrupted`), or the first arrival is still running
// (`running`). Each is the same unknown result with another first sentence;
// what follows is the same: read again, hold the controls, let the person look.
//
// Sekiz rota sunucunun sakladığı bir kimlik taşır (D-029). Onlarda yanıt, bu
// kanca haberdar olmadan önce aynı kimlikle bir kez daha sorulmuştur; bildirim
// bunu söyler (`asked`). Panel sonucun bilinmediğini kendisi de söyleyebilir:
// değişiklik sürerken yeniden başlamıştır (`interrupted`) ya da ilk geliş hâlâ
// sürüyordur (`running`). Sonrası aynıdır: yeniden oku, denetimleri tut.
/** Why a change has no result of its own. */
export type LostCause = 'dropped' | 'asked' | 'interrupted' | 'running';

export interface LostAnswer {
    /** When the answer was lost. */
    at: number;
    /**
     * `dropped`: no answer, and nothing was sent again. `asked`: no answer, and
     * asking once more under the same identity brought none either.
     * `interrupted`: the Panel says it restarted or failed while the change
     * ran. `running`: the Panel says the first arrival is still running.
     */
    cause: LostCause;
    /** When the state was read again after it; null until that read answers. */
    readAt: number | null;
    /**
     * What the state that was read again shows about the change: true, it is
     * there; false, it is not there; null, not read yet or this state cannot
     * show it.
     */
    shows: boolean | null;
}

/**
 * What a form asks of the state that was read again after its answer was lost.
 * Every part only looks; none of them sends anything.
 */
export interface Question {
    /**
     * `read` is what the re-read returned, all of it known. true: it shows the
     * change. false: it does not. null: this state cannot show it.
     */
    shows: (read: Known<unknown>[]) => boolean | null;
    /** The state shows the change: close or clear the form that sent it. */
    made?: (read: Known<unknown>[]) => void;
    /** The state does not show it: put back what was typed, if a newer answer rebuilt the form. */
    notMade?: (read: Known<unknown>[]) => void;
}

/** What `send` hands back: the Panel's own answer, or null when none arrived. */
export type Sent = Response | null;

// What the Panel answers on the eight identified routes when the change has no
// result to give: its own word that the result is not known (yet). Any other
// 409 is a result and is the caller's to show.
// Panel'in kimlikli sekiz rotada, verecek sonucu olmadığında yanıtladığı
// kodlar. Başka her 409 bir sonuçtur.
const OPEN_OUTCOMES: Record<string, LostCause> = {
    REQUEST_OUTCOME_UNKNOWN: 'interrupted',
    REQUEST_IN_PROGRESS: 'running',
};

async function openOutcome(res: Response): Promise<LostCause | null> {
    if (res.status !== 409) return null;
    try {
        const body: unknown = await res.clone().json();
        const code = body && typeof body === 'object' ? (body as { code?: unknown }).code : null;
        return typeof code === 'string' ? OPEN_OUTCOMES[code] ?? null : null;
    } catch {
        return null;
    }
}

/**
 * How one sent change ended without a result of its own, or null when the
 * answer is one (a success or a refusal). `res` is null when no answer
 * arrived. What counts as a lost answer is defined once, in
 * lib/requestIdentity.ts. It only looks at the answer.
 */
export async function unansweredCause(method: string, url: string, res: Response | null): Promise<LostCause | null> {
    const identified = isReaskRoute(method, url);
    if (res === null || answerWasLost(res)) return identified ? 'asked' : 'dropped';
    return identified ? openOutcome(res) : null;
}

export interface LostAnswerHandle {
    lost: LostAnswer | null;
    /** A change's answer was lost and the state has not been read again yet. */
    holding: boolean;
    /** The re-read is in flight. */
    checking: boolean;
    /**
     * Sends one change. Returns the Panel's answer (a success or its refusal),
     * or null when no answer arrived; in that case the notice is raised and
     * the state is read again. It never sends twice and never throws.
     * `question`, when the form has one, is asked of that re-read state.
     */
    send: (url: string, init: RequestInit, question?: Question) => Promise<Sent>;
    /** The answer arrived but could not be read as an answer: the result is as unknown. */
    lose: (question?: Question, cause?: LostCause) => void;
    /** Reads the state again. It only reads. */
    check: () => Promise<void>;
    /** The person has looked; the notice leaves. */
    dismiss: () => void;
    /** A later change was answered, so the earlier question is settled. */
    settle: () => void;
}

/**
 * `reread` reads what the screen's changes act on and reports what it now
 * knows; it must only read. One remote or several.
 */
export function useLostAnswer(reread: () => Promise<Remote<unknown> | Remote<unknown>[]>): LostAnswerHandle {
    const [lost, setLost] = useState<LostAnswer | null>(null);
    const [checking, setChecking] = useState(false);
    const rereadRef = useRef(reread);
    rereadRef.current = reread;
    // The question of the change whose answer is lost; none for a change that
    // has no form to ask it.
    // Yanıtı yiten değişikliğin sorusu.
    const question = useRef<Question | undefined>(undefined);
    // The form is closed once, by the first read that shows the change: a
    // later "Check again" must not close a form the person has opened since.
    // Form, değişikliği gösteren ilk okumayla bir kez kapatılır.
    const closed = useRef(false);
    const mounted = useRef(true);
    useEffect(() => {
        mounted.current = true;
        return () => {
            mounted.current = false;
        };
    }, []);

    const check = useCallback(async () => {
        setChecking(true);
        try {
            const result = await rereadRef.current();
            const all = Array.isArray(result) ? result : [result];
            if (!mounted.current) return;
            if (all.every((remote) => remote.state === 'known')) {
                const read = all as Known<unknown>[];
                const asked = question.current;
                const shows = asked ? asked.shows(read) : null;
                if (shows === true && !closed.current) {
                    closed.current = true;
                    asked?.made?.(read);
                }
                if (shows === false) asked?.notMade?.(read);
                setLost((current) => (current ? { ...current, readAt: Date.now(), shows } : current));
            }
        } finally {
            if (mounted.current) setChecking(false);
        }
    }, []);

    const raise = useCallback((asked?: Question, cause: LostCause = 'dropped') => {
        if (!mounted.current) return;
        question.current = asked;
        closed.current = false;
        setLost({ at: Date.now(), cause, readAt: null, shows: null });
        void check();
    }, [check]);

    const send = useCallback(async (url: string, init: RequestInit, asked?: Question): Promise<Sent> => {
        // `fetch` is the one intercepted fetch: on the eight identified routes
        // it has already asked once more before it fails here or hands back a
        // gateway's answer.
        // `fetch`, araya girilen tek fetch'tir: kimlikli sekiz rotada, burada
        // başarısız olmadan ya da geçidin yanıtını vermeden önce bir kez daha
        // sormuştur.
        let res: Response | null = null;
        try {
            res = await fetch(url, init);
        } catch {
            res = null;
        }
        const cause = await unansweredCause(init.method ?? 'GET', url, res);
        if (cause) {
            raise(asked, cause);
            return null;
        }
        return res;
    }, [raise]);

    const dismiss = useCallback(() => {
        question.current = undefined;
        setLost(null);
    }, []);

    return {
        lost,
        holding: lost !== null && lost.readAt === null,
        checking,
        send,
        lose: raise,
        check,
        dismiss,
        settle: dismiss,
    };
}
