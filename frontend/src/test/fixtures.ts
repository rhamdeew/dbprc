import type { Me } from '../api/me';

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
