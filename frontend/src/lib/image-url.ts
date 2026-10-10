export function imageURL(id: number | string, type: string, width?: number, tag?: string, index?: number) {
  const query = new URLSearchParams();
  if (tag) query.set('tag', tag);
  if (width) query.set('maxWidth', String(width));
  return `/Items/${id}/Images/${type}${index == null ? '' : '/' + index}${query.size ? `?${query}` : ''}`;
}
