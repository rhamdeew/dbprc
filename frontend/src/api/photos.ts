import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { profileKey, writeProfile, type Profile, type ProfilePhoto } from './profile';

export type PhotoComment = {
  id: number;
  author: { id: number; name: string; avatarUrl: string | null };
  body: string;
  createdAt: string;
  canDelete: boolean;
};

type Comments = { comments: PhotoComment[] };
type Likes = Pick<ProfilePhoto, 'likeCount' | 'likedByMe'>;

const commentsKey = (id: number, url: string) => ['photo-comments', id, url] as const;

function updatePhoto(
  cache: ReturnType<typeof useQueryClient>, userId: number, photoId: number,
  update: (photo: ProfilePhoto) => ProfilePhoto,
) {
  writeProfile(cache, userId, (profile) => ({
    ...profile,
    photos: profile.photos.map((photo) => photo.id === photoId ? update(photo) : photo),
  }));
}

export function usePhotoLike(userId: number, photoId: number) {
  const cache = useQueryClient();
  const mutationKey = ['photo-like', photoId];
  const pending = useIsMutating({ mutationKey }) > 0;
  const mutation = useMutation({
    mutationKey,
    mutationFn: (liked: boolean) => api<Likes>(`/api/photos/${photoId}/like`, {
      method: liked ? 'PUT' : 'DELETE', silent: true,
    }),
    onMutate: async (liked) => {
      await cache.cancelQueries({ queryKey: profileKey(userId) });
      const photo = cache.getQueryData<Profile>(profileKey(userId))?.photos.find(({ id }) => id === photoId);
      if (!photo) return;
      const { likedByMe, likeCount } = photo;
      updatePhoto(cache, userId, photoId, (current) => ({
        ...current,
        likedByMe: liked,
        likeCount: liked === likedByMe ? likeCount : likeCount + (liked ? 1 : -1),
      }));
      return { likedByMe, likeCount };
    },
    onError: (_error, _liked, previous) => {
      if (previous) updatePhoto(cache, userId, photoId, (photo) => ({ ...photo, ...previous }));
    },
    onSuccess: (likes) => updatePhoto(cache, userId, photoId, (photo) => ({ ...photo, ...likes })),
  });
  return { mutation, pending };
}

export function usePhotoComments(photo: ProfilePhoto) {
  return useQuery({
    queryKey: commentsKey(photo.id, photo.url),
    queryFn: ({ signal }) => api<Comments>(`/api/photos/${photo.id}/comments`, { signal, silent: true }),
    refetchOnMount: 'always',
    retry: false,
  });
}

export function usePhotoCommentActions(userId: number, photo: ProfilePhoto) {
  const cache = useQueryClient();
  const queryKey = commentsKey(photo.id, photo.url);
  const path = `/api/photos/${photo.id}/comments`;
  const reconcile = () => cache.invalidateQueries({ queryKey });
  const cancelRead = () => cache.cancelQueries({ queryKey });
  const add = useMutation({
    mutationFn: (body: string) => api<PhotoComment>(path, { method: 'POST', body: { body }, silent: true }),
    onMutate: cancelRead,
    onSuccess: (comment) => {
      cache.setQueryData<Comments>(queryKey, (current) => current && ({
        comments: [...current.comments.filter(({ id }) => id !== comment.id), comment].slice(-200),
      }));
      updatePhoto(cache, userId, photo.id, (current) => ({ ...current, commentCount: current.commentCount + 1 }));
      void reconcile();
    },
  });
  const remove = useMutation({
    mutationFn: (id: number) => api<void>(`${path}/${id}`, { method: 'DELETE', silent: true }),
    onMutate: cancelRead,
    onSuccess: (_result, deletedId) => {
      cache.setQueryData<Comments>(queryKey, (current) => current && ({
        comments: current.comments.filter(({ id }) => id !== deletedId),
      }));
      updatePhoto(cache, userId, photo.id, (current) => ({ ...current, commentCount: Math.max(0, current.commentCount - 1) }));
      void reconcile();
    },
    onError: () => { void reconcile(); },
  });
  return { add, remove };
}
