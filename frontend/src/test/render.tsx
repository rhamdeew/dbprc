import { QueryClient } from '@tanstack/react-query';
import { render as rtlRender, type RenderOptions } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { Providers } from '../app/providers';

type RenderArgs = Omit<RenderOptions, 'wrapper'> & { queryClient?: QueryClient };

export function render(ui: ReactElement, options?: RenderArgs) {
  const { queryClient, ...rest } = options ?? {};
  function Wrapper({ children }: { children: ReactNode }) {
    return <Providers queryClient={queryClient}>{children}</Providers>;
  }
  return rtlRender(ui, { wrapper: Wrapper, ...rest });
}
