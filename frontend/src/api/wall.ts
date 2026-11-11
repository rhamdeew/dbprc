import { useInfiniteQuery, useMutation, useQueryClient, type InfiniteData } from '@tanstack/react-query';
import { api } from './client';

/** One wall post from `GET`/`POST /api/users/{id}/wall` (ARC-21). */
export type WallAuthor = {
  id: number;
  name: string;
  avatarUrl: string | null;
};

export type WallPost = {
  id: number;
  body: string;
  drawingUrl: string | null;
  createdAt: string;
  isAnonymous: boolean;
  author: WallAuthor | null;
  canDelete: boolean;
};

export type WallRoot = WallPost & {
  replies: WallPost[];
};

export type WallPage = {
  posts: WallRoot[];
};

export type NewWallPost = {
  body: string;
  anonymous: boolean;
  drawing: Blob | null;
  /** Root post id. The API stores a reply to a reply on that root. */
  parentId?: number;
};

/** Page size sent as `limit`. A shorter page means there is nothing older. */
export const wallPageSize = 20;

type WallFetchPage = {
  posts: WallRoot[];
  hasMore: boolean;
};

type WallCache = InfiniteData<WallFetchPage, number>;

export function wallKey(userId: number) {
  return ['wall', userId] as const;
}

/** Owner view keeps `author` on an anonymous post. Everyone else gets a null author. */
export function wallAuthorLabel(post: Pick<WallPost, 'isAnonymous' | 'author'>): string {
  if (post.isAnonymous) {
    return post.author ? `Anonymous · ${post.author.name}` : 'Anonymous';
  }
  return post.author?.name ?? 'Anonymous';
}

export function useWall(userId: number) {
  return useInfiniteQuery({
    queryKey: wallKey(userId),
    queryFn: ({ pageParam }) => fetchWall(userId, pageParam),
    initialPageParam: 0,
    getNextPageParam: (last) => {
      if (!last.hasMore) return undefined;
      const cursor = last.posts[last.posts.length - 1];
      return cursor?.id;
    },
    retry: false,
  });
}

async function fetchWall(userId: number, before: number): Promise<WallFetchPage> {
  const params = new URLSearchParams({ limit: String(wallPageSize) });
  if (before > 0) params.set('before', String(before));
  const page = await api<WallPage>(`/api/users/${userId}/wall?${params.toString()}`);
  return { posts: page.posts, hasMore: page.posts.length === wallPageSize };
}

export function useCreateWallPost(userId: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: NewWallPost) =>
      api<WallPost>(`/api/users/${userId}/wall`, { method: 'POST', body: wallForm(input) }),
    onSuccess: (created, input) => {
      queryClient.setQueryData<WallCache>(wallKey(userId), (current) => insertPost(current, created, input.parentId));
    },
  });
}

export function useDeleteWallPost(userId: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (postId: number) => api<void>(`/api/wall/${postId}`, { method: 'DELETE' }),
    onSuccess: (_empty, postId) => {
      queryClient.setQueryData<WallCache>(wallKey(userId), (current) => removePost(current, postId));
    },
  });
}

function wallForm(input: NewWallPost): FormData {
  const form = new FormData();
  form.append('body', input.body.trim());
  form.append('anonymous', input.anonymous ? 'true' : 'false');
  if (input.drawing) form.append('drawing', input.drawing, 'drawing.png');
  if (input.parentId !== undefined) form.append('parentId', String(input.parentId));
  return form;
}

function insertPost(current: WallCache | undefined, created: WallPost, parentId: number | undefined): WallCache | undefined {
  if (!current) return current;
  if (parentId === undefined) {
    const [first, ...rest] = current.pages;
    if (!first) return current;
    const root: WallRoot = { ...created, replies: [] };
    return { ...current, pages: [{ ...first, posts: [root, ...first.posts] }, ...rest] };
  }
  return {
    ...current,
    pages: current.pages.map((page) => ({
      ...page,
      posts: page.posts.map((post) =>
        post.id === parentId ? { ...post, replies: [...post.replies, created] } : post,
      ),
    })),
  };
}

function removePost(current: WallCache | undefined, postId: number): WallCache | undefined {
  if (!current) return current;
  return {
    ...current,
    pages: current.pages.map((page) => ({
      ...page,
      posts: page.posts
        .filter((post) => post.id !== postId)
        .map((post) => ({ ...post, replies: post.replies.filter((reply) => reply.id !== postId) })),
    })),
  };
}
