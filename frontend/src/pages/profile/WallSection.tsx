import { Box, Button, Divider, Group, Loader, Modal, Stack, Text } from '@mantine/core';
import { useState } from 'react';
import { ApiError } from '../../api/client';
import { useDeleteWallPost, useWall, type WallPost } from '../../api/wall';
import { Panel } from '../../components/Panel';
import { BORDER, HEADER, PANEL } from '../../theme/theme';
import { WallComposer } from './WallComposer';
import { WallPostView } from './WallPostView';

type WallSectionProps = {
  userId: number;
};

function wallErrorMessage(error: unknown): string {
  if (error instanceof ApiError && error.message !== '') return error.message;
  return 'Could not load this wall.';
}

export function WallSection({ userId }: WallSectionProps) {
  const wall = useWall(userId);
  const remove = useDeleteWallPost(userId);
  const [replyingTo, setReplyingTo] = useState<number | null>(null);
  const [confirm, setConfirm] = useState<WallPost | null>(null);
  const posts = wall.data?.pages.flatMap((page) => page.posts) ?? [];

  function closeConfirm() {
    if (!remove.isPending) setConfirm(null);
  }

  return (
    <Panel title="Wall">
      <Stack gap="sm">
        {wall.isPending ? (
          <Loader color="arcBlue" size="sm" aria-label="Loading wall" />
        ) : !wall.data ? (
          <Text>{wallErrorMessage(wall.error)}</Text>
        ) : (
          <>
            <WallComposer userId={userId} />
            <Divider color={BORDER} />
            {posts.length === 0 ? (
              <Text c="dimmed">No posts yet.</Text>
            ) : (
              <Stack gap="sm">
                {posts.map((root, index) => (
                  <Box
                    key={root.id}
                    className={index > 0 ? 'arc-wall-post' : undefined}
                    pt={index > 0 ? 'xs' : 0}
                    style={index > 0 ? { borderTop: `1px solid ${BORDER}` } : undefined}
                  >
                    <Stack gap="xs">
                      <WallPostView post={root} onDelete={root.canDelete ? () => setConfirm(root) : undefined} />
                      {root.replies.length > 0 ? (
                        <Stack
                          className="arc-wall-replies"
                          gap="xs"
                          ml={{ base: 0, sm: 44 }}
                          p={6}
                          style={{ borderLeft: `2px solid ${BORDER}`, background: PANEL }}
                        >
                          {root.replies.map((reply) => (
                            <WallPostView
                              key={reply.id}
                              post={reply}
                              onDelete={reply.canDelete ? () => setConfirm(reply) : undefined}
                            />
                          ))}
                        </Stack>
                      ) : null}
                      <Group justify="flex-start">
                        <Button
                          type="button"
                          variant="subtle"
                          size="compact-xs"
                          c={HEADER}
                          aria-expanded={replyingTo === root.id}
                          onClick={() => setReplyingTo((current) => (current === root.id ? null : root.id))}
                        >
                          Reply
                        </Button>
                      </Group>
                      {replyingTo === root.id ? (
                        <WallComposer
                          userId={userId}
                          parentId={root.id}
                          onPosted={() => setReplyingTo(null)}
                        />
                      ) : null}
                    </Stack>
                  </Box>
                ))}
              </Stack>
            )}
            {wall.hasNextPage ? (
              <Button
                type="button"
                variant="default"
                loading={wall.isFetchingNextPage}
                onClick={() => wall.fetchNextPage()}
              >
                Show more
              </Button>
            ) : null}
          </>
        )}
      </Stack>
      <Modal
        opened={confirm !== null}
        onClose={closeConfirm}
        title="Delete this post?"
        centered
        transitionProps={{ duration: 0 }}
      >
        <Group justify="flex-end" gap="xs">
          <Button type="button" variant="default" disabled={remove.isPending} onClick={closeConfirm}>
            Cancel
          </Button>
          <Button
            type="button"
            color="red"
            loading={remove.isPending}
            onClick={() => {
              if (!confirm) return;
              remove.mutate(confirm.id, { onSuccess: () => setConfirm(null) });
            }}
          >
            Delete
          </Button>
        </Group>
      </Modal>
    </Panel>
  );
}
