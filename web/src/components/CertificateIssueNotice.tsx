import { XCircle } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import type { ApiError } from '../lib/apiError';
import { Button } from './ui';

// A certificate request that certbot ran and did not fulfil (11 Oct 2026;
// D-024, cmd/panel/certificate_issue_failure.go). The Panel answers
// CERTIFICATE_ISSUE_FAILED with the kind of failure as `reason`: the
// certificate authority could not be reached, it refused the validation, one
// of its limits was reached, certbot ran out of time, or certbot ended with an
// error that is none of these. Each has its own sentence: what happened, what
// is in place now, what to do, and that nothing asks again by itself.
//
// This used to be "internal server error" in a toast that left after five
// seconds. A sentence with something to correct stays until the person closes
// it or requests again. An administrator also gets one line of certbot's own
// words, in the face used for what a program printed.
//
// certbot'un çalışıp yerine getirmediği bir sertifika isteği. Panel, hatanın
// türünü `reason` olarak verir; her türün kendi cümlesi vardır: ne oldu, şu an
// ne yerinde, ne yapılmalı ve hiçbir şeyin kendiliğinden yeniden istemediği.
// Beş saniyede kaybolan bir bildirim değil: kişi kapatana ya da yeniden
// isteyene dek yerinde durur.
export function isCertificateIssueFailure(error: ApiError): boolean {
    return error.code === 'CERTIFICATE_ISSUE_FAILED';
}

// The sentences live with this screen's own copy, not in the boot copy: they
// are read only here. A kind this page has no words for gets the general one.
// Cümleler bu ekranın kendi metinleriyle durur. Bu sayfanın sözü olmayan bir
// tür, genel cümleyi alır.
const sentences: Record<string, TranslationKey> = {
    authority_unreachable: 'ssl.issueFailure.authority_unreachable',
    validation: 'ssl.issueFailure.validation',
    rate_limited: 'ssl.issueFailure.rate_limited',
    timeout: 'ssl.issueFailure.timeout',
    tool: 'ssl.issueFailure.tool',
};

export function CertificateIssueNotice({
    failure,
    onClose,
    className,
}: {
    failure: ApiError | null;
    onClose: () => void;
    className?: string;
}) {
    const { t } = useI18n();
    const box = useRef<HTMLDivElement>(null);
    useEffect(() => {
        if (failure) box.current?.scrollIntoView?.({ block: 'nearest' });
    }, [failure]);
    if (!failure) return null;
    const detail = failure.vars?.detail;
    const [before, after] = t('ssl.issueFailure.said', { detail: '\u0000' }).split('\u0000');
    return (
        <div
            ref={box}
            role="alert"
            data-certificate-issue={failure.reason ?? 'failed'}
            className={`flex items-start gap-2 rounded-lg border border-danger/30 bg-danger/10 p-3 text-sm leading-relaxed text-fg ${className ?? ''}`}
        >
            <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-danger" aria-hidden="true" />
            <div className="min-w-0">
                <p className="max-w-[75ch] break-words">
                    {t(sentences[failure.reason ?? ''] ?? 'ssl.issueFailure.tool', { domain: failure.vars?.domain ?? '' })}
                </p>
                {detail && (
                    <p className="mt-1.5 max-w-[75ch] break-words text-fg-muted">
                        {before}<span className="font-mono text-fg">{detail}</span>{after}
                    </p>
                )}
                <div className="mt-2">
                    <Button type="button" onClick={onClose}>{t('common.close')}</Button>
                </div>
            </div>
        </div>
    );
}
