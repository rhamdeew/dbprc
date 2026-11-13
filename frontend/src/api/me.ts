import { useQuery } from '@tanstack/react-query';
import { api } from './client';

/**
 * `GET /api/me` (ARC-16). `isAdmin` is true when the email is in `ADMIN_EMAILS`.
 * Tests mock this route until that endpoint exists.
 */
export type Me = {
  id: number;
  email: string;
  name: string;
  avatarUrl: string | null;
  available: boolean;
  isAdmin: boolean;
};

export const meKey = ['me'] as const;

export function useMe() {
  return useQuery({
    queryKey: meKey,
    queryFn: () => api<Me>('/api/me'),
    retry: false,
    staleTime: 30_000,
  });
}
