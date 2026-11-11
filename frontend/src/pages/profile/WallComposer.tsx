import { Box, Button, Checkbox, Group, Stack, Text, Textarea } from '@mantine/core';
import { useState, type FormEvent } from 'react';
import { useCreateWallPost } from '../../api/wall';
import { DrawingPad } from '../../components/DrawingPad';

const anonymousHint = "The owner of this wall will still see it's you";

type WallComposerProps = {
  userId: number;
  /** Set for a reply. Always the root id, matching the wall API. */
  parentId?: number;
  onPosted?: () => void;
};

export function WallComposer({ userId, parentId, onPosted }: WallComposerProps) {
  const create = useCreateWallPost(userId);
  const [text, setText] = useState('');
  const [draw, setDraw] = useState(false);
  const [drawing, setDrawing] = useState<Blob | null>(null);
  const [anonymous, setAnonymous] = useState(false);
  const reply = parentId !== undefined;
  const picture = draw ? drawing : null;
  const canPost = text.trim() !== '' || picture !== null;
  const hintId = reply ? `wall-anonymous-hint-${parentId}` : 'wall-anonymous-hint';

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (create.isPending || !canPost) return;
    create.mutate(
      { body: text, anonymous, drawing: picture, parentId },
      {
        onSuccess: () => {
          setText('');
          setAnonymous(false);
          setDraw(false);
          setDrawing(null);
          onPosted?.();
        },
      },
    );
  }

  return (
    <form aria-label={reply ? 'Reply composer' : 'Wall composer'} onSubmit={onSubmit}>
      <Stack gap="xs">
        <Textarea
          label={reply ? 'Write a reply' : 'Write on this wall'}
          value={text}
          onChange={(event) => setText(event.currentTarget.value)}
          maxLength={2000}
          rows={reply ? 2 : 3}
        />
        <Checkbox
          label="Draw"
          checked={draw}
          onChange={(event) => {
            const next = event.currentTarget.checked;
            setDraw(next);
            if (!next) setDrawing(null);
          }}
        />
        {draw ? (
          <Box style={{ maxWidth: '100%', overflowX: 'auto' }}>
            <DrawingPad onChange={setDrawing} />
          </Box>
        ) : null}
        <Checkbox
          label="Post anonymously"
          checked={anonymous}
          aria-describedby={hintId}
          onChange={(event) => setAnonymous(event.currentTarget.checked)}
        />
        <Text id={hintId} size="xs" c="dimmed">
          {anonymousHint}
        </Text>
        <Group justify="flex-end">
          <Button type="submit" loading={create.isPending} disabled={!canPost}>
            Post
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
