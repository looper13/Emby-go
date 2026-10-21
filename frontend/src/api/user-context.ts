import { useAuthStore } from '../stores/auth';
import { ApiError } from './client';

export function currentUserPath(suffix: string): string {
  const id = useAuthStore().user?.Id;
  if (!id) throw new ApiError('登录已失效', 401);
  return `/Users/${encodeURIComponent(id)}/${suffix}`;
}
