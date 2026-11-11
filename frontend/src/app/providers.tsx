import { MantineProvider } from '@mantine/core';
import { Notifications } from '@mantine/notifications';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';
import { cssVariablesResolver, theme } from '../theme/theme';

export type ProvidersProps = {
  children: ReactNode;
  /** Test seam. Production omits it and keeps a client for the lifetime of the tree. */
  queryClient?: QueryClient;
};

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  });
}

export function Providers({ children, queryClient }: ProvidersProps) {
  const [owned] = useState(makeQueryClient);
  const client = queryClient ?? owned;
  return (
    <MantineProvider theme={theme} cssVariablesResolver={cssVariablesResolver} forceColorScheme="light">
      <Notifications position="top-right" transitionDuration={0} />
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </MantineProvider>
  );
}
