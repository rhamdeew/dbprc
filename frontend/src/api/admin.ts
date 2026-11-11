import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';

/** `GET /api/admin/stats` (ARC-34). Six totals; available users are `available != 0`. */
export type AdminStats = {
  users: number;
  availableUsers: number;
  chats: number;
  messages: number;
  wallPosts: number;
  photos: number;
};

/**
 * `GET /api/admin/users/preview?email=` (ARC-34).
 * An unknown email is `{exists:false}` and zeros, not an error.
 * A post can be counted in both `wallPosts` and `authoredPosts`.
 */
export type AdminPreview = {
  exists: boolean;
  wallPosts: number;
  authoredPosts: number;
  photos: number;
  chats: number;
  messages: number;
};

export const adminStatsKey = ['admin-stats'] as const;

function previewPath(email: string): string {
  return `/api/admin/users/preview?email=${encodeURIComponent(email)}`;
}

export function useAdminStats() {
  return useQuery({
    queryKey: adminStatsKey,
    queryFn: () => api<AdminStats>('/api/admin/stats'),
    retry: false,
  });
}

export function usePreviewRemoval() {
  return useMutation({
    mutationFn: (email: string) => api<AdminPreview>(previewPath(email)),
  });
}

export function useRemoveProfile() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (email: string) => api<void>('/api/admin/users', { method: 'DELETE', body: { email } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminStatsKey }),
  });
}
