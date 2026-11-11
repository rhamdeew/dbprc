import { Box } from '@mantine/core';

/** Right side of the top bar. ARC-24 replaces this with colleague search. */
export function SearchSlot() {
  return <Box role="search" aria-label="Search" data-testid="search-slot" miw={180} />;
}
