import { MantineProvider } from '@mantine/core';
import { render as rtlRender } from '@testing-library/react';
import type { ReactElement } from 'react';
import { theme } from '../theme/theme';

export function render(ui: ReactElement) {
  return rtlRender(<MantineProvider theme={theme}>{ui}</MantineProvider>);
}
