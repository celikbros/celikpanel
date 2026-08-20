import { createContext } from 'react';

export type DesktopPageHeaderSlot = {
    target: HTMLElement | null;
    register: () => () => void;
};

export const DesktopPageHeaderTargetContext = createContext<DesktopPageHeaderSlot | undefined>(undefined);
