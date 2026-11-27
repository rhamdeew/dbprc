import { ActionIcon, Avatar, Box, Button, Group, Stack, Text, Textarea } from '@mantine/core';
import { useState } from 'react';
import { Link } from 'react-router';
import { ApiError } from '../../api/client';
import { usePhotoCommentActions, usePhotoComments, usePhotoLike } from '../../api/photos';
import type { ProfilePhoto } from '../../api/profile';

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback;
}

export function PhotoInteractions({ photo, userId }: { photo: ProfilePhoto; userId: number }) {
  const { mutation: like, pending: likePending } = usePhotoLike(userId, photo.id);
  const comments = usePhotoComments(photo);
  const { add, remove } = usePhotoCommentActions(userId, photo);
  const [body, setBody] = useState('');

  return (
    <section className="arc-photo-interactions" aria-label="Photo reactions and comments">
      <Stack gap="sm">
        <Group gap="xs">
          <ActionIcon
            type="button"
            variant="light"
            size="md"
            aria-label={photo.likedByMe ? 'Unlike' : 'Like'}
            disabled={likePending}
            onClick={() => like.mutate(!photo.likedByMe)}
          >
            <span aria-hidden="true">{photo.likedByMe ? '♥' : '♡'}</span>
          </ActionIcon>
          <Text size="sm" aria-label={`${photo.likeCount} ${photo.likeCount === 1 ? 'like' : 'likes'}`}>{photo.likeCount}</Text>
        </Group>
        {like.isError && <Text role="alert">{errorMessage(like.error, 'Could not update like.')}</Text>}
        <Text component="h2" size="sm" fw={700} m={0}>Comments ({photo.commentCount})</Text>
        {comments.isPending && <Text role="status">Loading comments…</Text>}
        {comments.isError && <Text role="alert">{errorMessage(comments.error, 'Could not load comments.')}</Text>}
        {comments.data?.comments.map((comment) => (
          <Group key={comment.id} align="flex-start" gap="xs" wrap="nowrap">
            <Avatar src={comment.author.avatarUrl} size="sm" aria-hidden="true" />
            <Box style={{ minWidth: 0, flex: 1, overflowWrap: 'anywhere' }}>
              <Group gap="xs" align="baseline">
                <Text component={Link} to={`/u/${comment.author.id}`} size="sm" fw={600}>
                  {comment.author.name}
                </Text>
                <Text component="time" dateTime={comment.createdAt} size="xs" c="dimmed">
                  {new Date(comment.createdAt).toLocaleString()}
                </Text>
              </Group>
              <Text size="sm" style={{ whiteSpace: 'pre-wrap' }}>{comment.body}</Text>
              {comment.canDelete && (
                <Button
                  type="button"
                  variant="subtle"
                  aria-label={`Delete comment by ${comment.author.name}`}
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(comment.id)}
                >
                  Delete
                </Button>
              )}
            </Box>
          </Group>
        ))}
        {remove.isError && <Text role="alert">{errorMessage(remove.error, 'Could not delete comment.')}</Text>}
        <form onSubmit={(event) => {
          event.preventDefault();
          if (!add.isPending && comments.data) add.mutate(body, { onSuccess: () => setBody('') });
        }}>
          <Stack gap="xs" align="flex-start">
            <Textarea
              w="100%"
              label="Write a comment"
              value={body}
              disabled={add.isPending}
              onChange={(event) => setBody(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') event.stopPropagation();
              }}
            />
            {add.isError && <Text role="alert">{errorMessage(add.error, 'Could not post comment.')}</Text>}
            <Button type="submit" size="xs" loading={add.isPending} disabled={!comments.data}>Comment</Button>
          </Stack>
        </form>
      </Stack>
    </section>
  );
}
