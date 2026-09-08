// The same monochrome mark used by the public site; color follows its surface.
export function BrandMark({ className = '' }: { className?: string }) {
    return <svg className={className} viewBox="0 0 32 32" aria-hidden="true" focusable="false">
        <path fill="currentColor" d="M11 3H29V10H14L10 14V18L14 22H29V29H11L3 21V11Z M20 13H29V19H20Z" />
    </svg>;
}
