import { useContext, useEffect, useLayoutEffect, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { DesktopPageHeaderTargetContext } from './pageHeaderSlot';

export function PageHeader({
    title,
    subtitle,
    breadcrumb,
    actions,
}: {
    title: string;
    subtitle?: string;
    breadcrumb?: string[];
    actions?: ReactNode;
}) {
    const desktopSlot = useContext(DesktopPageHeaderTargetContext);
    const desktopTarget = desktopSlot?.target;
    const [isDesktop, setIsDesktop] = useState(
        () => typeof window !== 'undefined' && window.matchMedia('(min-width: 1280px)').matches,
    );

    useEffect(() => {
        const media = window.matchMedia('(min-width: 1280px)');
        const update = () => setIsDesktop(media.matches);
        update();
        media.addEventListener('change', update);
        return () => media.removeEventListener('change', update);
    }, []);

    useLayoutEffect(() => desktopSlot?.register(), [desktopSlot?.register]);

    const headerContent = (
        <div className="flex w-full min-w-0 flex-wrap items-center justify-between gap-3">
            <div className="min-w-0 flex-1">
                {breadcrumb && breadcrumb.length > 0 && (
                    <nav className="mb-1 flex items-center gap-1.5 text-xs text-fg-subtle">
                        {breadcrumb.map((crumb, i) => (
                            <span key={i} className="flex items-center gap-1.5">
                                {i > 0 && <span>/</span>}
                                <span>{crumb}</span>
                            </span>
                        ))}
                    </nav>
                )}
                <h1 className="break-words text-2xl font-bold tracking-tight text-fg">{title}</h1>
                {subtitle && <p className="mt-0.5 break-words text-sm text-fg-muted">{subtitle}</p>}
            </div>
            {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
        </div>
    );

    if (isDesktop && desktopSlot && desktopTarget === null) return null;
    if (isDesktop && desktopTarget) return createPortal(headerContent, desktopTarget);

    return <div className="mb-6">{headerContent}</div>;
}
