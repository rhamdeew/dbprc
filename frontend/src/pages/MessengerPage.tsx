import { Text, Title } from '@mantine/core';
import { Panel } from '../components/Panel';

export function MessengerPage() {
  return (
    <Panel>
      <Title order={1}>Messenger</Title>
      <Text mt="xs">Your chats will show up here.</Text>
    </Panel>
  );
}
