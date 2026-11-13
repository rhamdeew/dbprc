import type { Me } from '../api/me';
import type { Profile } from '../api/profile';

export const member: Me = {
  id: 7,
  email: 'ada@corp.test',
  name: 'Ada Lovelace',
  avatarUrl: null,
  available: true,
  isAdmin: false,
};

export const admin: Me = {
  ...member,
  id: 1,
  email: 'grace@corp.test',
  name: 'Grace Hopper',
  isAdmin: true,
};

export function profileFor(person: Me, overrides: Partial<Profile> = {}): Profile {
  const { interests, ...rest } = overrides;
  return {
    id: person.id,
    name: person.name,
    email: person.email,
    avatarUrl: person.avatarUrl,
    available: person.available,
    photos: [],
    isMe: true,
    ...rest,
    interests: { hobby: '', music: '', movies: '', other: '', ...interests },
  };
}
