import { Text, Title } from '@mantine/core';
import { useParams } from 'react-router';
import { Panel } from '../components/Panel';

export function ProfilePage() {
  const { id = '' } = useParams();
  return (
    <Panel>
      <Title order={1}>Profile</Title>
      <Text mt="xs">User {id}</Text>
    </Panel>
  );
}
