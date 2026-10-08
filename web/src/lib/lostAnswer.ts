import { useCallback, useEffect, useRef, useState } from 'react';
import type { Known, Remote } from './remote';

// A change whose answer did not arrive (D-024: an unknown result is neither a
// failure nor a success). Most changes the panel sends carry no identity the
// server keeps: adding a DNS record, a team member, a VPN peer or a backup a
// second time makes a second one. So when the connection drops, or a gateway
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
export interface LostAnswer {
    /** When the answer was lost. */
    at: number;
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

// A gateway in front of the Panel answers 408, 502, 503 or 504 with its own
// page when it lost the Panel's answer. The Panel's own refusals are JSON, so a
// refusal with one of these codes and no JSON body is not the Panel speaking.
// Panelin önündeki geçit, Panelin yanıtını yitirdiğinde kendi sayfasıyla 408,
// 502, 503 ya da 504 yanıtlar; Panelin kendi retleri JSON'dur.
function answeredByGateway(res: Response): boolean {
    if (![408, 502, 503, 504].includes(res.status)) return false;
    return !(res.headers.get('Content-Type') ?? '').toLowerCase().includes('json');
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
    lose: (question?: Question) => void;
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

    const raise = useCallback((asked?: Question) => {
        if (!mounted.current) return;
        question.current = asked;
        closed.current = false;
        setLost({ at: Date.now(), readAt: null, shows: null });
        void check();
    }, [check]);

    const send = useCallback(async (url: string, init: RequestInit, asked?: Question): Promise<Sent> => {
        let res: Response;
        try {
            res = await fetch(url, init);
        } catch {
            raise(asked);
            return null;
        }
        if (answeredByGateway(res)) {
            raise(asked);
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
