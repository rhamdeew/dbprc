import { Paper, type PaperProps } from '@mantine/core';
import type { ReactNode } from 'react';
import { BORDER } from '../theme/theme';

type PanelProps = PaperProps & { children?: ReactNode };

export function Panel({ style, children, ...props }: PanelProps) {
  return (
    <Paper withBorder shadow="none" p="md" radius={2} bg="white" {...props} style={{ borderColor: BORDER, ...style }}>
      {children}
    </Paper>
  );
}
