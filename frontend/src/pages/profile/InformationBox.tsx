import { Button, Group, Stack, Text, Textarea, TextInput } from '@mantine/core';
import { useState } from 'react';
import { interestFields, useSaveInterests, useSaveProfileFields, type InterestKey, type Profile } from '../../api/profile';
import { Panel } from '../../components/Panel';
import { HEADER } from '../../theme/theme';

type InformationBoxProps = {
  profile: Profile;
};

const profileFields = [
  { key: 'nickname', label: 'Nickname', limit: 40 },
  { key: 'hometown', label: 'Where are you from?', limit: 100 },
  { key: 'about', label: 'Tell something about you', limit: 1000 },
] as const;

type FieldKey = InterestKey | (typeof profileFields)[number]['key'];

export function InformationBox({ profile }: InformationBoxProps) {
  const saveInterests = useSaveInterests(profile.id);
  const saveProfile = useSaveProfileFields(profile.id);
  const pending = saveInterests.isPending || saveProfile.isPending;
  const values = { ...profile.interests, nickname: profile.nickname, hometown: profile.hometown, about: profile.about };
  const [editing, setEditing] = useState<FieldKey | null>(null);
  const [draft, setDraft] = useState('');

  function start(key: FieldKey) {
    setEditing(key);
    setDraft(values[key]);
  }

  function cancel() {
    setEditing(null);
    setDraft('');
  }

  function submit() {
    if (!editing || pending) return;
    const options = { onSuccess: cancel };
    const value = draft.trim();
    switch (editing) {
      case 'nickname':
      case 'hometown':
      case 'about':
        saveProfile.mutate(
          { nickname: profile.nickname, hometown: profile.hometown, about: profile.about, [editing]: value },
          options,
        );
        break;
      default:
        saveInterests.mutate({ ...profile.interests, [editing]: value }, options);
    }
  }

  return (
    <Panel title="Information">
      <Stack gap={6}>
        {[...profileFields, ...interestFields].map((field) => {
          const isProfileField = 'limit' in field;
          const value = values[field.key];
          if (isProfileField && !profile.isMe && (value === '' || field.key === 'nickname')) return null;
          if (editing === field.key) {
            const Input = field.key === 'about' ? Textarea : TextInput;
            return (
              <Stack key={field.key} gap={6}>
                <Input
                  label={field.label}
                  value={draft}
                  disabled={pending}
                  onChange={(event) => {
                    const limit = isProfileField ? field.limit : 500;
                    setDraft(Array.from(event.currentTarget.value).slice(0, limit).join(''));
                  }}
                />
                <Group gap="xs">
                  <Button type="button" size="compact-xs" loading={pending} onClick={submit}>
                    Save
                  </Button>
                  <Button type="button" size="compact-xs" variant="default" disabled={pending} onClick={cancel}>
                    Cancel
                  </Button>
                </Group>
              </Stack>
            );
          }
          return (
            <Group key={field.key} justify="space-between" wrap="nowrap" gap="sm" align="flex-start">
              <Text fw={700} c="dimmed" w={isProfileField ? { base: 100, sm: 144 } : 64} style={{ flexShrink: 0 }}>
                {field.label}{field.key === 'hometown' ? '' : ':'}
              </Text>
              <Text style={{ flex: 1, minWidth: 0, overflowWrap: 'anywhere', whiteSpace: field.key === 'about' ? 'pre-wrap' : undefined }} c={value === '' ? 'dimmed' : undefined}>
                {value === '' ? 'Not set' : value}
              </Text>
              {profile.isMe ? (
                <Button type="button" variant="subtle" size="compact-xs" c={HEADER} aria-label={`Edit ${field.label}`} disabled={pending} onClick={() => start(field.key)}>
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
