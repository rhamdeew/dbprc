import { Button, Group, Text, TextInput, UnstyledButton } from '@mantine/core';
import { useState } from 'react';
import { useSaveStatus, type Profile } from '../../api/profile';

const statusStyle = { overflowWrap: 'anywhere' } as const;

// Longer than this (Unicode code points) the status creeps across the line like a TV ticker.
export const MARQUEE_MIN_LENGTH = 67;

function StatusText({ text }: { text: string }) {
  const length = Array.from(text).length;
  if (length <= MARQUEE_MIN_LENGTH) return <>{text}</>;
  // About 5 characters per second, plus the empty lead-in of one box width.
  const seconds = Math.max(8, Math.round(length / 5));
  return (
    <span className="arc-status-marquee">
      <span style={{ animationDuration: `${seconds}s` }}>{text}</span>
    </span>
  );
}

export function ProfileStatus({ profile }: { profile: Profile }) {
  const save = useSaveStatus(profile.id);
  const [draft, setDraft] = useState<string | null>(null);

  if (!profile.isMe) {
    return profile.status ? <Text style={statusStyle}><StatusText text={profile.status} /></Text> : null;
  }
  if (draft === null) {
    return (
      <UnstyledButton
        type="button"
        className={profile.status ? 'arc-status-edit' : 'arc-status-cta'}
        style={statusStyle}
        aria-label={profile.status ? `Edit status: ${profile.status}` : 'Set status'}
        onClick={() => setDraft(profile.status)}
      >
        {profile.status ? <StatusText text={profile.status} /> : <>set status</>}
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
