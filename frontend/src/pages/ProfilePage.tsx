import { Center, Flex, Loader, Stack, Text, Title } from '@mantine/core';
import { useParams } from 'react-router';
import { ApiError } from '../api/client';
import { useProfile, type Profile } from '../api/profile';
import { Panel } from '../components/Panel';
import { AvatarColumn } from './profile/AvatarColumn';
import { InformationBox } from './profile/InformationBox';
import { PhotosBox } from './profile/PhotosBox';
import { WallPlaceholder } from './profile/WallPlaceholder';

function parseProfileId(raw: string | undefined): number | null {
  if (!raw || !/^[1-9]\d*$/.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) ? id : null;
}

function profileErrorMessage(error: unknown): string {
  if (error instanceof ApiError && error.message !== '') return error.message;
  return 'Could not load this profile.';
}

function ProfileMissing({ message }: { message: string }) {
  return (
    <Panel>
      <Title order={1}>Profile</Title>
      <Text mt="xs">{message}</Text>
    </Panel>
  );
}

function ProfileView({ profile }: { profile: Profile }) {
  return (
    <Stack gap="md">
      <Flex direction={{ base: 'column', sm: 'row' }} gap="md" align="flex-start">
        <AvatarColumn profile={profile} />
        <Stack gap="md" style={{ flex: 1, minWidth: 0 }} w="100%">
          <Title order={1}>{profile.name}</Title>
          <InformationBox profile={profile} />
          <PhotosBox profile={profile} />
        </Stack>
      </Flex>
      <WallPlaceholder />
    </Stack>
  );
}

export function ProfilePage() {
  const params = useParams();
  const id = parseProfileId(params.id);
  const profile = useProfile(id);

  if (id === null) return <ProfileMissing message="Profile not found." />;
  if (profile.isLoading) {
    return (
      <Center mih={240}>
        <Loader color="arcBlue" aria-label="Loading profile" />
      </Center>
    );
  }
  if (!profile.data) return <ProfileMissing message={profileErrorMessage(profile.error)} />;
  return <ProfileView key={profile.data.id} profile={profile.data} />;
}
