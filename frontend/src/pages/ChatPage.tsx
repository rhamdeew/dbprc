import { Text, Title } from '@mantine/core';
import { useParams } from 'react-router';
import { Panel } from '../components/Panel';

export function ChatPage() {
  const { chatId = '' } = useParams();
  return (
    <Panel>
      <Title order={1}>Chat</Title>
      <Text mt="xs">Conversation {chatId}</Text>
    </Panel>
  );
}
