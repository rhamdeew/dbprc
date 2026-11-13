import { Button, Group, Stack, Text, TextInput, Title } from '@mantine/core';
import { useState } from 'react';
import { interestFields, useSaveInterests, type InterestKey, type Profile } from '../../api/profile';
import { Panel } from '../../components/Panel';

type InformationBoxProps = {
  profile: Profile;
};

export function InformationBox({ profile }: InformationBoxProps) {
  const save = useSaveInterests(profile.id);
  const [editing, setEditing] = useState<InterestKey | null>(null);
  const [draft, setDraft] = useState('');

  function start(key: InterestKey) {
    setEditing(key);
    setDraft(profile.interests[key]);
  }

  function cancel() {
    setEditing(null);
    setDraft('');
  }

  function submit() {
    if (!editing) return;
    // PUT replaces every interest. Send the other three unchanged.
    const interests = { ...profile.interests, [editing]: draft.trim() };
    save.mutate(interests, { onSuccess: () => setEditing(null) });
  }

  return (
    <Panel>
      <Stack gap="sm">
        <Title order={2}>Information</Title>
        {interestFields.map((field) => {
          const value = profile.interests[field.key];
          if (editing === field.key) {
            return (
              <Stack key={field.key} gap={6}>
                <TextInput
                  label={field.label}
                  value={draft}
                  onChange={(event) => setDraft(Array.from(event.currentTarget.value).slice(0, 500).join(''))}
                />
                <Group gap="xs">
                  <Button type="button" size="compact-sm" loading={save.isPending} onClick={submit}>
                    Save
                  </Button>
                  <Button type="button" size="compact-sm" variant="default" disabled={save.isPending} onClick={cancel}>
                    Cancel
                  </Button>
                </Group>
              </Stack>
            );
          }
          return (
            <Group key={field.key} justify="space-between" wrap="nowrap" gap="sm">
              <Text fw={700} w={64} style={{ flexShrink: 0 }}>
                {field.label}
              </Text>
              <Text style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere' }} c={value === '' ? 'dimmed' : undefined}>
                {value === '' ? 'Not set' : value}
              </Text>
              {profile.isMe ? (
                <Button type="button" variant="subtle" size="compact-sm" aria-label={`Edit ${field.label}`} onClick={() => start(field.key)}>
                  Edit
                </Button>
              ) : null}
            </Group>
          );
        })}
      </Stack>
    </Panel>
  );
}
