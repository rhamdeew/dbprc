import { useQuery } from '@tanstack/react-query';
import { api } from './client';

/** One row of `GET /api/users/search` (ARC-24). */
export type Colleague = {
  id: number;
  name: string;
  email: string;
  avatarUrl: string | null;
};

/** The API returns nobody until the query is this many characters. */
export const minSearchCharacters = 4;

export function colleagueSearchKey(q: string) {
  return ['users', 'search', q] as const;
}

/** `Array.from` counts Unicode code points, matching the API's rune check. */
export function searchQueryReady(q: string): boolean {
  return Array.from(q).length >= minSearchCharacters;
}

export function useColleagueSearch(q: string) {
  return useQuery({
    queryKey: colleagueSearchKey(q),
    queryFn: () => api<Colleague[]>(`/api/users/search?q=${encodeURIComponent(q)}`),
    enabled: searchQueryReady(q),
    retry: false,
  });
}
