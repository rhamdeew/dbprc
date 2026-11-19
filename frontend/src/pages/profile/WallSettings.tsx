import { Checkbox } from '@mantine/core';
import { useSaveWallSettings, type Profile } from '../../api/profile';

export function WallSettings({ profile }: { profile: Profile }) {
  const save = useSaveWallSettings(profile.id);

  return (
    <Checkbox
      label="Allow anonymous messages on my wall"
      checked={profile.allowAnonymous}
      disabled={save.isPending}
      onChange={(event) => save.mutate(event.currentTarget.checked)}
    />
  );
}
