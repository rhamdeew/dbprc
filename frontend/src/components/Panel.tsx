import { Box, Group, Paper, Title, type MantineSpacing, type PaperProps, type StyleProp } from '@mantine/core';
import type { ReactNode } from 'react';
import { BORDER, HEADER } from '../theme/theme';

type PanelProps = Omit<PaperProps, 'p'> & {
  children?: ReactNode;
  /** Text of the light-blue header strip. It is the panel's level-2 heading. */
  title?: ReactNode;
  /** Control on the right of the header strip. */
  action?: ReactNode;
  /** Body padding. 0 for an edge-to-edge list. */
  p?: StyleProp<MantineSpacing>;
};

/** A bordered box in the 2006 style, with an optional titled header strip. */
export function Panel({ style, children, title, action, p = 8, ...props }: PanelProps) {
  return (
    <Paper withBorder shadow="none" radius={2} bg="white" {...props} p={0} style={{ borderColor: BORDER, ...style }}>
      {title !== undefined ? (
        <Group justify="space-between" wrap="nowrap" gap="xs" px={8} py={4} mih={26} style={{ background: BORDER }}>
          <Title order={2} fz={12} lh={1.45} c={HEADER} style={{ minWidth: 0 }}>
            {title}
          </Title>
          {action}
        </Group>
      ) : null}
      <Box p={p}>{children}</Box>
    </Paper>
  );
}
