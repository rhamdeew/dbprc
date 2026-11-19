import { Button, Group, Text, TextInput, UnstyledButton } from '@mantine/core';
import { useState } from 'react';
import { useSaveStatus, type Profile } from '../../api/profile';

const statusStyle = { overflowWrap: 'anywhere' } as const;

export function ProfileStatus({ profile }: { profile: Profile }) {
  const save = useSaveStatus(profile.id);
  const [draft, setDraft] = useState<string | null>(null);

  if (!profile.isMe) {
    return profile.status ? <Text style={statusStyle}>{profile.status}</Text> : null;
  }
  if (draft === null) {
    return (
      <UnstyledButton
        type="button"
        c={profile.status ? undefined : 'dimmed'}
        style={statusStyle}
        aria-label={profile.status ? `Edit status: ${profile.status}` : undefined}
        onClick={() => setDraft(profile.status)}
      >
        {profile.status || 'Set status'}
      </UnstyledButton>
    );
  }
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (!save.isPending) save.mutate(draft, { onSuccess: () => setDraft(null) });
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape' && !save.isPending) setDraft(null);
      }}
    >
      <Group gap="xs" align="end">
        <TextInput
          label="Status"
          value={draft}
          autoFocus
          disabled={save.isPending}
          style={{ flex: 1 }}
          onChange={(event) => setDraft(Array.from(event.currentTarget.value).slice(0, 140).join(''))}
        />
        <Button type="submit" size="xs" loading={save.isPending}>Save</Button>
      </Group>
    </form>
  );
}
