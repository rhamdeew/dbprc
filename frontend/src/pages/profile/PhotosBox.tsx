import { ActionIcon, Box, Button, Group, Modal, SimpleGrid, Stack, Text, UnstyledButton } from '@mantine/core';
import { useState } from 'react';
import Lightbox from 'yet-another-react-lightbox';
import 'yet-another-react-lightbox/styles.css';
import { maxPhotos, useAddPhoto, useDeletePhoto, type Profile, type ProfilePhoto } from '../../api/profile';
import { Panel } from '../../components/Panel';
import { BORDER } from '../../theme/theme';
import { FilePicker } from './FilePicker';
import { UploadProgress } from './UploadProgress';

type PhotosBoxProps = {
  profile: Profile;
};

export function PhotosBox({ profile }: PhotosBoxProps) {
  const { mutation: add, progress } = useAddPhoto(profile.id);
  const remove = useDeletePhoto(profile.id);
  const [index, setIndex] = useState(-1);
  const [confirm, setConfirm] = useState<ProfilePhoto | null>(null);
  const atLimit = profile.photos.length >= maxPhotos;

  function askDelete(photo: ProfilePhoto) {
    setConfirm(photo);
  }

  function closeConfirm() {
    if (!remove.isPending) setConfirm(null);
  }

  return (
    <Panel
      title={`Photos (${profile.photos.length}/${maxPhotos})`}
      action={
        profile.isMe && !atLimit ? (
          <FilePicker label="Add photo" inputLabel="Add a photo" disabled={add.isPending} onPick={(file) => add.mutate(file)} />
        ) : null
      }
    >
      <Stack gap="sm">
        <UploadProgress value={progress} label="Photo upload" />
        {profile.photos.length === 0 ? (
          <Text c="dimmed">No photos yet.</Text>
        ) : (
          <SimpleGrid cols={{ base: 3, sm: 5 }} spacing={6}>
            {profile.photos.map((photo, photoIndex) => (
              <Box key={photo.id} pos="relative">
                <UnstyledButton
                  type="button"
                  aria-label={`Open photo ${photoIndex + 1}`}
                  onClick={() => setIndex(photoIndex)}
                  display="block"
                  w="100%"
                  h={100}
                  style={{ border: `1px solid ${BORDER}`, overflow: 'hidden' }}
                >
                  <img src={photo.url} alt="" style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }} />
                </UnstyledButton>
                {profile.isMe ? (
                  <ActionIcon
                    type="button"
                    aria-label={`Delete photo ${photo.id}`}
                    variant="filled"
                    color="dark"
                    size="sm"
                    radius={2}
                    pos="absolute"
                    top={2}
                    right={2}
                    onClick={() => askDelete(photo)}
                  >
                    ×
                  </ActionIcon>
                ) : null}
              </Box>
            ))}
          </SimpleGrid>
        )}
      </Stack>
      <Modal
        opened={confirm !== null}
        onClose={closeConfirm}
        title="Delete this photo?"
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
              remove.mutate(confirm.id, {
                onSuccess: () => {
                  setConfirm(null);
                  setIndex(-1);
                },
              });
            }}
          >
            Delete
          </Button>
        </Group>
      </Modal>
      {index >= 0 ? (
        <Lightbox
          open
          close={() => setIndex(-1)}
          index={index}
          slides={profile.photos.map((photo, photoIndex) => ({
            src: photo.url,
            alt: `Photo ${photoIndex + 1} of ${profile.photos.length}`,
          }))}
          animation={{ fade: 0, swipe: 0 }}
          carousel={{ finite: true }}
        />
      ) : null}
    </Panel>
  );
}
