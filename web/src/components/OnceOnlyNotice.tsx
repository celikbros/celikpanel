import { AlertTriangle } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { useI18n } from '../i18n';
import { Button } from './ui';

// A change that was made, whose one-time result did not reach this page
// (D-029: `409 REQUEST_COMPLETED_RESULT_NOT_RETAINED`). The server keeps only
// that it was done: a VPN device's configuration and a database user's
// password are shown once and stored nowhere, so asking again cannot bring
// them back. That is not a failure (the device or the database exists) and is
// not drawn as one; it is something the person has to act on, so it stands in
// place on the attention surface with the sentence that says what to do, until
// they close it.
//
// Yapılmış, ama tek seferlik sonucu bu sayfaya ulaşmamış değişiklik (D-029).
// Sunucu yalnız yapıldığını saklar: VPN cihazının yapılandırması ve veritabanı
// kullanıcısının parolası bir kez gösterilir, hiçbir yerde saklanmaz. Bu bir
// hata değildir ve hata gibi çizilmez; kişinin yapması gereken bir şeydir, bu
// yüzden ne yapılacağını söyleyen cümleyle, kapatılana dek yerinde durur.
export function OnceOnlyNotice({
    text,
    onClose,
    className,
}: {
    /** What was made, that its result cannot be shown again, and what to do. */
    text: string | null;
    onClose: () => void;
    className?: string;
}) {
    const { t } = useI18n();
    const box = useRef<HTMLDivElement>(null);
    useEffect(() => {
        if (text) box.current?.scrollIntoView?.({ block: 'nearest' });
    }, [text]);
    if (!text) return null;
    return (
        <div
            ref={box}
            role="alert"
            data-once-only
            className={`flex items-start gap-2 rounded-lg border border-warning-mark/50 bg-warning-mark/20 p-3 text-sm leading-relaxed text-fg ${className ?? ''}`}
        >
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden="true" />
            <div className="min-w-0">
                <p className="max-w-[75ch] break-words">{text}</p>
                <div className="mt-2">
                    <Button type="button" onClick={onClose}>{t('common.close')}</Button>
                </div>
            </div>
        </div>
    );
}

/** The answer that says a change was made and its one-time result is not kept. */
export function resultNotKept(problem: { code?: string; reason?: string }): boolean {
    return problem.code === 'REQUEST_COMPLETED_RESULT_NOT_RETAINED' && !problem.reason;
}
