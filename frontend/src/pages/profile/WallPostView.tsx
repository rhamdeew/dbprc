import { Avatar, Button, Group, Stack, Text } from '@mantine/core';
import { wallAuthorLabel, type WallPost } from '../../api/wall';
import { BORDER } from '../../theme/theme';
import { relativeTime } from './relativeTime';

type WallPostViewProps = {
  post: WallPost;
  onDelete?: () => void;
};

export function WallPostView({ post, onDelete }: WallPostViewProps) {
  const label = wallAuthorLabel(post);
  return (
    <Group align="flex-start" wrap="nowrap" gap="sm">
      <Avatar
        src={post.author?.avatarUrl}
        name={post.author?.name ?? 'Anonymous'}
        alt=""
        size={36}
        radius={2}
        color="arcBlue"
      />
      <Stack gap={4} style={{ flex: 1, minWidth: 0 }}>
        <Group gap={6} wrap="wrap">
          <Text fw={700}>{label}</Text>
          <Text component="time" dateTime={post.createdAt} c="dimmed" size="xs">
            {relativeTime(post.createdAt)}
          </Text>
        </Group>
        {post.body !== '' ? <Text style={{ whiteSpace: 'pre-wrap' }}>{post.body}</Text> : null}
        {post.drawingUrl ? (
          <img
            src={post.drawingUrl}
            alt="Drawing"
            style={{ maxWidth: '100%', height: 'auto', background: '#ffffff', border: `1px solid ${BORDER}` }}
          />
        ) : null}
        {onDelete ? (
          <Group justify="flex-start">
            <Button type="button" variant="subtle" color="red" size="compact-sm" onClick={onDelete}>
              Delete
            </Button>
          </Group>
        ) : null}
      </Stack>
    </Group>
  );
}
