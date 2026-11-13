import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { api, uploadFile } from './client';
import { meKey, type Me } from './me';

/** `GET /api/users/{id}` (ARC-19). `isMe` is the server's own-profile flag. */
export type Interests = {
  hobby: string;
  music: string;
  movies: string;
  other: string;
};

export type ProfilePhoto = {
  id: number;
  url: string;
};

export type Profile = {
  id: number;
  name: string;
  email: string;
  avatarUrl: string | null;
  /** In the random-chat pool. */
  available: boolean;
  interests: Interests;
  photos: ProfilePhoto[];
  isMe: boolean;
};

export const interestFields = [
  { key: 'hobby', label: 'Hobby' },
  { key: 'music', label: 'Music' },
  { key: 'movies', label: 'Movies' },
  { key: 'other', label: 'Other' },
] as const;

export type InterestKey = (typeof interestFields)[number]['key'];

export const maxPhotos = 5;

export function profileKey(id: number) {
  return ['profile', id] as const;
}

export function useProfile(id: number | null) {
  return useQuery({
    queryKey: profileKey(id ?? 0),
    queryFn: () => api<Profile>(`/api/users/${id}`),
    enabled: id !== null,
    retry: false,
  });
}

function writeProfile(queryClient: ReturnType<typeof useQueryClient>, id: number, update: (profile: Profile) => Profile) {
  queryClient.setQueryData<Profile>(profileKey(id), (current) => (current ? update(current) : current));
}

export function useSaveInterests(userId: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (interests: Interests) => api<Interests>('/api/me/interests', { method: 'PUT', body: interests }),
    onSuccess: (interests) => writeProfile(queryClient, userId, (profile) => ({ ...profile, interests })),
  });
}

export function useUploadAvatar(userId: number) {
  const queryClient = useQueryClient();
  const [progress, setProgress] = useState<number | null>(null);
  const mutation = useMutation({
    mutationFn: (file: File) => uploadFile<{ avatarUrl: string }>('/api/me/avatar', file, setProgress),
    onMutate: () => setProgress(0),
    onSuccess: ({ avatarUrl }) => {
      writeProfile(queryClient, userId, (profile) => ({ ...profile, avatarUrl }));
      queryClient.setQueryData<Me>(meKey, (me) => (me && me.id === userId ? { ...me, avatarUrl } : me));
    },
    onSettled: () => setProgress(null),
  });
  return { mutation, progress };
}

export function useAddPhoto(userId: number) {
  const queryClient = useQueryClient();
  const [progress, setProgress] = useState<number | null>(null);
  const mutation = useMutation({
    mutationFn: (file: File) => uploadFile<ProfilePhoto>('/api/me/photos', file, setProgress),
    onMutate: () => setProgress(0),
    onSuccess: (photo) =>
      writeProfile(queryClient, userId, (profile) =>
        profile.photos.some((item) => item.id === photo.id)
          ? profile
          : { ...profile, photos: [...profile.photos, photo] },
      ),
    onSettled: () => setProgress(null),
  });
  return { mutation, progress };
}

export function useDeletePhoto(userId: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (photoId: number) => api<void>(`/api/me/photos/${photoId}`, { method: 'DELETE' }),
    onSuccess: (_empty, photoId) =>
      writeProfile(queryClient, userId, (profile) => ({
        ...profile,
        photos: profile.photos.filter((item) => item.id !== photoId),
      })),
  });
}
