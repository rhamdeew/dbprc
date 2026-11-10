import { Center, Text, Title } from '@mantine/core';
import { HEADER, PANEL } from '../theme/theme';
import { Panel } from '../components/Panel';

/** Placeholder until ARC-17 builds the Google sign-in page. */
export function LoginPage() {
  return (
    <Center mih="100vh" bg={PANEL} p="md">
      <Panel maw={420} w="100%">
        <Text fw={700} fz={18} c={HEADER}>
          ARC
        </Text>
        <Title order={1} mt={4}>
          Sign in
        </Title>
        <Text mt="sm">Meet your teammates when you have time.</Text>
      </Panel>
    </Center>
  );
}
