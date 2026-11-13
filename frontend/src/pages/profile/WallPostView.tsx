import { Anchor, Avatar, Button, Group, Stack, Text } from '@mantine/core';
import { Link } from 'react-router';
import { wallAuthorLabel, type WallPost } from '../../api/wall';
import { BORDER } from '../../theme/theme';
import { relativeTime } from './relativeTime';

type WallPostViewProps = {
  post: WallPost;
  onDelete?: () => void;
};

export function WallPostView({ post, onDelete }: WallPostViewProps) {
  const label = wallAuthorLabel(post);
  // An anonymous post never links to a profile, even for the wall owner who can see the name.
  const profile = !post.isAnonymous && post.author ? post.author : null;
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
          {profile ? (
            <Anchor component={Link} to={`/u/${profile.id}`} fw={700} inherit>
              {label}
            </Anchor>
          ) : (
            <Text fw={700}>{label}</Text>
          )}
          <Text component="time" dateTime={post.createdAt} c="dimmed" size="xs">
            {relativeTime(post.createdAt)}
          </Text>
        </Group>
        {post.body !== '' ? <Text style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{post.body}</Text> : null}
        {post.drawingUrl ? (
          <img
            src={post.drawingUrl}
            alt="Drawing"
            width={400}
            height={200}
            // Drawn at 400x200 CSS pixels; the stored bitmap is larger on a high-density screen.
            style={{
              display: 'block',
              width: 400,
              maxWidth: '100%',
              height: 'auto',
              aspectRatio: '2 / 1',
              background: '#ffffff',
              border: `1px solid ${BORDER}`,
            }}
          />
        ) : null}
        {onDelete ? (
          <Group justify="flex-start">
            <Button type="button" variant="subtle" color="red" size="compact-xs" onClick={onDelete}>
              Delete
            </Button>
          </Group>
        ) : null}
      </Stack>
    </Group>
  );
}
