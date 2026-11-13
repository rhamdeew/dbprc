import {
  Box,
  Button,
  Center,
  Group,
  Loader,
  Modal,
  SimpleGrid,
  Stack,
  Text,
  TextInput,
  Title,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { useState, type FormEvent } from 'react';
import {
  useAdminStats,
  usePreviewRemoval,
  useRemoveProfile,
  type AdminPreview,
  type AdminStats,
} from '../api/admin';
import { ApiError } from '../api/client';
import { useMe } from '../api/me';
import { Panel } from '../components/Panel';
import { NotFoundPage } from './NotFoundPage';

const statFields: { key: keyof AdminStats; label: string }[] = [
  { key: 'users', label: 'Users' },
  { key: 'availableUsers', label: 'Available for chat' },
  { key: 'chats', label: 'Chats' },
  { key: 'messages', label: 'Messages' },
  { key: 'wallPosts', label: 'Wall posts' },
  { key: 'photos', label: 'Photos' },
];

const previewFields: { key: keyof Omit<AdminPreview, 'exists'>; label: string }[] = [
  { key: 'wallPosts', label: 'Posts on their wall' },
  { key: 'authoredPosts', label: 'Posts they wrote' },
  { key: 'photos', label: 'Photos' },
  { key: 'chats', label: 'Chats' },
  { key: 'messages', label: 'Messages' },
];

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.message !== '') return error.message;
  return fallback;
}

function StatCards({ stats }: { stats: AdminStats }) {
  return (
    <SimpleGrid cols={{ base: 2, sm: 3 }} spacing="sm">
      {statFields.map((field) => (
        <Box key={field.key} role="group" aria-label={field.label}>
          <Panel p="sm">
            <Text fw={700} fz={18}>
              {stats[field.key]}
            </Text>
            <Text>{field.label}</Text>
          </Panel>
        </Box>
      ))}
    </SimpleGrid>
  );
}

function StatsSection() {
  const stats = useAdminStats();
  return (
    <Box component="section" aria-label="Stats">
      {stats.isLoading ? (
        <Center mih={80}>
          <Loader color="arcBlue" aria-label="Loading stats" />
        </Center>
      ) : stats.data ? (
        <StatCards stats={stats.data} />
      ) : (
        <Text>{errorMessage(stats.error, 'Could not load stats.')}</Text>
      )}
    </Box>
  );
}

function CountList({ preview }: { preview: AdminPreview }) {
  return (
    <Stack gap={4} component="dl" my={0}>
      {previewFields.map((field) => (
        <Group key={field.key} justify="space-between" gap="sm" wrap="nowrap" role="group" aria-label={field.label}>
          <Text component="dt">{field.label}</Text>
          <Text component="dd" fw={700} my={0}>
            {preview[field.key]}
          </Text>
        </Group>
      ))}
    </Stack>
  );
}

function RemoveProfile() {
  const previewRemoval = usePreviewRemoval();
  const remove = useRemoveProfile();
  const [email, setEmail] = useState('');
  const [previewEmail, setPreviewEmail] = useState<string | null>(null);
  const [preview, setPreview] = useState<AdminPreview | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmEmail, setConfirmEmail] = useState('');

  const typed = email.trim();
  const confirmed = confirmEmail.trim();
  const matches = previewEmail !== null && confirmed === previewEmail;

  function onEmailChange(value: string) {
    setEmail(value);
    setPreview(null);
    setPreviewEmail(null);
    setConfirmOpen(false);
    setConfirmEmail('');
  }

  function onPreview(event: FormEvent) {
    event.preventDefault();
    if (typed === '' || previewRemoval.isPending) return;
    previewRemoval.mutate(typed, {
      onSuccess: (data) => {
        setPreviewEmail(typed);
        setPreview(data);
      },
    });
  }

  function closeConfirm() {
    if (remove.isPending) return;
    setConfirmOpen(false);
    setConfirmEmail('');
  }

  function onDelete(event: FormEvent) {
    event.preventDefault();
    if (!matches || previewEmail === null || !preview?.exists || remove.isPending) return;
    const target = previewEmail;
    remove.mutate(target, {
      onSuccess: () => {
        notifications.show({ color: 'green', message: 'Profile removed.' });
        setEmail('');
        setPreview(null);
        setPreviewEmail(null);
        setConfirmOpen(false);
        setConfirmEmail('');
      },
    });
  }

  return (
    <Box component="section" aria-label="Remove profile">
      <Panel>
        <Stack gap="sm">
          <Title order={2}>Remove profile</Title>
          <Text>Deletes that profile, their posts, and their chats. This cannot be undone.</Text>
          <form onSubmit={onPreview} noValidate>
            <Stack gap="xs">
              <TextInput
                label="Email"
                type="email"
                autoComplete="off"
                value={email}
                onChange={(event) => onEmailChange(event.currentTarget.value)}
              />
              <Group justify="flex-start">
                <Button type="submit" loading={previewRemoval.isPending} disabled={typed === ''}>
                  Preview
                </Button>
              </Group>
            </Stack>
          </form>
          {preview && previewEmail ? (
            <Stack gap="sm">
              {!preview.exists ? <Text>No profile with that email.</Text> : null}
              <CountList preview={preview} />
              {preview.exists ? (
                <Group justify="flex-start">
                  <Button type="button" color="red" onClick={() => setConfirmOpen(true)}>
                    Remove profile
                  </Button>
                </Group>
              ) : null}
            </Stack>
          ) : null}
        </Stack>
      </Panel>
      <Modal
        opened={confirmOpen}
        onClose={closeConfirm}
        title="Remove this profile?"
        centered
        transitionProps={{ duration: 0 }}
      >
        <form onSubmit={onDelete} noValidate>
          <Stack gap="sm">
            <Text>Type the email again to confirm.</Text>
            <TextInput
              label="Confirm email"
              type="email"
              autoComplete="off"
              value={confirmEmail}
              onChange={(event) => setConfirmEmail(event.currentTarget.value)}
            />
            {confirmEmail !== '' && !matches ? <Text c="red">Email does not match.</Text> : null}
            <Group justify="flex-end" gap="xs">
              <Button type="button" variant="default" disabled={remove.isPending} onClick={closeConfirm}>
                Cancel
              </Button>
              <Button type="submit" color="red" loading={remove.isPending} disabled={!matches}>
                Delete
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>
    </Box>
  );
}

export function AdminPage() {
  const me = useMe();
  if (me.isPending) {
    return (
      <Center mih={120}>
        <Loader color="arcBlue" aria-label="Loading" />
      </Center>
    );
  }
  if (!me.data?.isAdmin) return <NotFoundPage />;

  return (
    <Stack gap="md">
      <Title order={1}>Admin</Title>
      <StatsSection />
      <RemoveProfile />
    </Stack>
  );
}
