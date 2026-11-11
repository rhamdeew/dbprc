import { Text, Title } from '@mantine/core';
import { Panel } from '../../components/Panel';

/** ARC-23 fills this in. Keep the box so the profile layout already has a wall. */
export function WallPlaceholder() {
  return (
    <Panel>
      <Title order={2}>Wall</Title>
      <Text mt="xs">Posts will show up here.</Text>
    </Panel>
  );
}
