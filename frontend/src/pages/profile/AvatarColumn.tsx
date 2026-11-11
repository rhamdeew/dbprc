import { Avatar, Stack } from '@mantine/core';
import { useUploadAvatar, type Profile } from '../../api/profile';
import { Panel } from '../../components/Panel';
import { FilePicker } from './FilePicker';
import { UploadProgress } from './UploadProgress';

type AvatarColumnProps = {
  profile: Profile;
};

export function AvatarColumn({ profile }: AvatarColumnProps) {
  const { mutation, progress } = useUploadAvatar(profile.id);
  return (
    <Panel w={{ base: '100%', sm: 220 }} style={{ flexShrink: 0 }}>
      <Stack gap="xs" align="center">
        <Avatar
          src={profile.avatarUrl}
          name={profile.name}
          alt={`${profile.name}, profile photo`}
          size={180}
          radius={2}
          color="arcBlue"
        />
        {profile.isMe ? (
          <FilePicker
            label="Change photo"
            inputLabel="Profile photo"
            disabled={mutation.isPending}
            onPick={(file) => mutation.mutate(file)}
          />
        ) : null}
        <UploadProgress value={progress} label="Avatar upload" />
      </Stack>
    </Panel>
  );
}
