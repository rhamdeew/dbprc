import { Avatar, Stack } from '@mantine/core';
import { BORDER } from '../../theme/theme';
import { useUploadAvatar, type Profile } from '../../api/profile';
import { FilePicker } from './FilePicker';
import { UploadProgress } from './UploadProgress';

type AvatarColumnProps = {
  profile: Profile;
};

export function AvatarColumn({ profile }: AvatarColumnProps) {
  const { mutation, progress } = useUploadAvatar(profile.id);
  return (
    <Stack gap={4} align="flex-start" w={{ base: '100%', sm: 200 }} style={{ flexShrink: 0 }}>
        <Avatar
          src={profile.avatarUrl}
          name={profile.name}
          alt={`${profile.name}, profile photo`}
          size={200}
          radius={2}
          color="arcBlue"
          style={{ border: `1px solid ${BORDER}` }}
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
  );
}
