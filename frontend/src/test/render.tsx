/* eslint-disable react-refresh/only-export-components -- test helper, not a fast-refresh module */
import { render as rtlRender, type RenderOptions } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { Providers } from '../app/providers';

function Wrapper({ children }: { children: ReactNode }) {
  return <Providers>{children}</Providers>;
}

export function render(ui: ReactElement, options?: Omit<RenderOptions, 'wrapper'>) {
  return rtlRender(ui, { wrapper: Wrapper, ...options });
}
