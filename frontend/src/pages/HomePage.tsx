import { Anchor, Container, Paper, Text, Title } from '@mantine/core';
import { useEffect, useState } from 'react';

type VersionInfo = { version: string; env: string };

export function HomePage() {
  const [info, setInfo] = useState<VersionInfo | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    fetch('/api/version')
      .then((r) => (r.ok ? (r.json() as Promise<VersionInfo>) : Promise.reject(new Error(String(r.status)))))
      .then(setInfo)
      .catch(() => setFailed(true));
  }, []);

  return (
    <Container size={980} py="xl">
      <Paper withBorder p="lg">
        <Title order={1} c="arcBlue.6">
          ARC — Async Random Coffee
        </Title>
        <Text mt="sm">Meet your teammates when you have time. Coming soon.</Text>
        <Text mt="md" size="sm" c="dimmed" data-testid="version">
          {failed ? 'API unavailable' : info ? `${info.env} · ${info.version}` : 'Loading…'}
        </Text>
        <Anchor href="/healthz" size="xs">
          health
        </Anchor>
      </Paper>
    </Container>
  );
}
