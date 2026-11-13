import { Stack, Text, Title } from '@mantine/core';

export function NotFoundPage() {
  return (
    <Stack gap="sm">
      <Title order={1}>404 · Page not found</Title>
      <Text>We couldn’t find that page.</Text>
    </Stack>
  );
}
