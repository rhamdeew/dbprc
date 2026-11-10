import { Text, Title } from '@mantine/core';
import { Navigate } from 'react-router';
import { useMe } from '../api/me';
import { Panel } from '../components/Panel';

export function AdminPage() {
  const { data: me } = useMe();
  if (me && !me.isAdmin) return <Navigate to="/" replace />;

  return (
    <Panel>
      <Title order={1}>Admin</Title>
      <Text mt="xs">Aggregate stats will live here.</Text>
    </Panel>
  );
}
