import { Fragment } from 'react';

// A web address shown as a link (browser inspection, 2026-10-08: "break-all" cut
// the Panel's secure address inside a word, "panel.examp / le.com", and on a
// phone inside the scheme). The link is one piece that moves to the next line
// whole when it does not fit beside the sentence. Only an address wider than its
// own line is broken, and then where an address may be broken: before a dot or
// the port, or at a hyphen, which the browser already treats as a break. The
// scheme is never broken. A single label with no such place, wider than the
// line, is the last resort and wraps where it must instead of overflowing.
//
// Bağlantı olarak gösterilen web adresi. Satıra sığmazsa bütün olarak alt satıra
// geçer; yalnızca kendi satırından geniş adres bölünür: noktadan ya da porttan
// önce ya da tireden sonra. Şema hiç bölünmez.
export function AddressLink({ href, address }: { href: string; address: string }) {
    const scheme = address.slice(0, address.indexOf('://') + 3);
    const parts = address.slice(scheme.length).split(/(?=[.:/])/);
    return <a href={href} className="inline-block max-w-full font-semibold text-primary underline underline-offset-4 [overflow-wrap:anywhere]">
        <span className="whitespace-nowrap">{scheme}</span>{parts.map((part, index) => <Fragment key={index}>{index > 0 && <wbr />}{part}</Fragment>)}
    </a>;
}
