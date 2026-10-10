// These are consumed fields of the raw DTOs, not a global key-renaming layer.
export interface Credentials {
  Username: string;
  Pw: string;
}

export interface AuthStatusDto { initialized: boolean }

export interface UserDto {
  Id: string;
  Name: string;
  ServerId: string;
  ServerName: string;
  Policy: { IsAdministrator: boolean };
}

export interface LoginDto {
  AccessToken: string;
  ServerId: string;
  User: UserDto;
}

export interface LibraryDto { Id: number; Name: string; Path: string }
export interface LibrariesDto { items: LibraryDto[] | null; total: number }
export interface StatusDto { success: number; manual: number; pending: number; incompatible: number }

export interface MovieDto {
  id: number; library_id: number; source_path: string; Status: string;
  Title: string; OriginalTitle: string; Number: string; Plot: string;
  Year: number; Rating: number; RuntimeSeconds: number;
  PosterPath: string; LandscapePath: string; BackdropPath: string; cover_url: string;
  source_protocol?: string; source_container?: string; trailer_url?: string; collection?: string;
  Genres?: string[] | null; Tags?: string[] | null; Studios?: string[] | null; AdditionalParts?: string[] | null;
  Maker?: string; Label?: string; Director?: string; official_rating?: string; NFOPath?: string;
  created_at?: string; last_scrape_at?: string; last_scrape_error?: string;
}
export interface WallUserData { position_ticks: number; played: boolean; favorite: boolean }
export interface WallDto {
  items: MovieDto[]; total: number;
  image_tags: Record<string, Record<string, string>>;
  userdata?: Record<string, WallUserData>;
}
export interface ImageDto { ImageType: string; ImageTag?: string; ImageIndex?: number }
export interface ActorDto { name: string; has_image: boolean; image_tag?: string }
export interface StreamDto { Index?: number; Type?: string; Codec?: string; Width?: number; Height?: number; BitRate?: number; Profile?: string; BitDepth?: number; ChannelLayout?: string; Channels?: number; Language?: string; SampleRate?: number; IsDefault?: boolean; IsForced?: boolean; IsExternal?: boolean }
export interface FileDto { index: number; role: string; name: string; path: string; size: number; probed: boolean; streams: StreamDto[] }
export interface SimilarDto { Id: string; Name: string; ImageTags?: Record<string, string>; ProductionYear?: number; RunTimeTicks?: number; CommunityRating?: number }
export interface DetailDto { movie: MovieDto; library_name: string; playable: boolean; images?: ImageDto[]; actors?: ActorDto[]; files?: FileDto[]; modified_at?: string }

export function parseMovie(value: unknown): MovieDto {
  if (!isRecord(value) || !isCount(value.id) || value.id === 0 || !isCount(value.library_id)
    || typeof value.Title !== 'string' || typeof value.Status !== 'string'
    || typeof value.source_path !== 'string') return invalidDto();
  const strings = ['OriginalTitle', 'Number', 'Plot', 'PosterPath', 'LandscapePath', 'BackdropPath', 'cover_url'] as const;
  const numbers = ['Year', 'Rating', 'RuntimeSeconds'] as const;
  for (const key of strings) if (typeof value[key] !== 'string') return invalidDto();
  for (const key of numbers) if (typeof value[key] !== 'number' || !Number.isFinite(value[key])) return invalidDto();
  const movie = Object.fromEntries(['id', 'library_id', 'source_path', 'Status', 'Title', ...strings, ...numbers]
    .map(key => [key, value[key]])) as unknown as MovieDto;
  for (const key of ['source_protocol', 'source_container', 'trailer_url', 'collection', 'Maker', 'Label', 'Director', 'official_rating', 'NFOPath', 'created_at', 'last_scrape_at', 'last_scrape_error'] as const) {
    if (value[key] != null && typeof value[key] !== 'string') return invalidDto(); movie[key] = (value[key] ?? '') as string;
  }
  for (const key of ['Genres', 'Tags', 'Studios', 'AdditionalParts'] as const) {
    const values = array(value[key]); if (values.some(v => typeof v !== 'string')) return invalidDto(); movie[key] = values as string[];
  }
  return movie;
}

export function parseWall(value: unknown): WallDto {
  if (!isRecord(value) || !isCount(value.total) || (value.items !== null && !Array.isArray(value.items))) return invalidDto();
  const image_tags: WallDto['image_tags'] = {};
  if (value.image_tags != null) {
    if (!isRecord(value.image_tags)) return invalidDto();
    for (const [id, tags] of Object.entries(value.image_tags)) {
      if (!/^[1-9]\d*$/u.test(id) || !isRecord(tags) || Object.values(tags).some(tag => typeof tag !== 'string')) return invalidDto();
      image_tags[id] = tags as Record<string, string>;
    }
  }
  const userdata: Record<string, WallUserData> = {};
  if (value.userdata != null) {
    if (!isRecord(value.userdata)) return invalidDto();
    for (const [id, ud] of Object.entries(value.userdata)) {
      if (!/^[1-9]\d*$/u.test(id) || !isRecord(ud) || !isCount(ud.position_ticks) || typeof ud.played !== 'boolean' || typeof ud.favorite !== 'boolean') return invalidDto();
      userdata[id] = { position_ticks: ud.position_ticks, played: ud.played, favorite: ud.favorite };
    }
  }
  return { items: (value.items ?? []).map(parseMovie), total: value.total, image_tags, userdata };
}

export function parseDetail(value: unknown): DetailDto {
  if (!isRecord(value) || typeof value.library_name !== 'string' || typeof value.playable !== 'boolean') return invalidDto();
  return { movie: parseMovie(value.movie), library_name: value.library_name, playable: value.playable,
    images: array(value.images).map(v => { const r = obj(v); return { ImageType: str(r.ImageType), ImageTag: optionalString(r.ImageTag), ImageIndex: optionalNumber(r.ImageIndex) }; }),
    actors: array(value.actors).map(v => { const r = obj(v); if (typeof r.has_image !== 'boolean') return invalidDto(); return { name: str(r.name), has_image: r.has_image, image_tag: optionalString(r.image_tag) }; }),
    files: array(value.files).map(v => { const r = obj(v); if (!isCount(r.index) || !isCount(r.size) || typeof r.probed !== 'boolean') return invalidDto();
      return { index: r.index, size: r.size, probed: r.probed, name: str(r.name), path: str(r.path), role: str(r.role), streams: array(r.streams).map(parseStream) }; }),
    modified_at: optionalString(value.modified_at) };
}
function obj(v: unknown) { return isRecord(v) ? v : invalidDto(); }
function str(v: unknown) { return typeof v === 'string' ? v : invalidDto(); }
function optionalString(v: unknown) { return v == null ? undefined : str(v); }
function optionalNumber(v: unknown) { return v == null ? undefined : typeof v === 'number' && Number.isFinite(v) ? v : invalidDto(); }
function array(v: unknown): unknown[] { return v == null ? [] : Array.isArray(v) ? v : invalidDto(); }
function parseStream(v: unknown): StreamDto {
  const r = obj(v); const stream: StreamDto = {};
  for (const key of ['Type', 'Codec', 'Profile', 'ChannelLayout', 'Language'] as const) stream[key] = optionalString(r[key]);
  for (const key of ['Index', 'Width', 'Height', 'BitRate', 'BitDepth', 'Channels', 'SampleRate'] as const) stream[key] = optionalNumber(r[key]);
  for (const key of ['IsDefault', 'IsForced', 'IsExternal'] as const) { if (r[key] != null && typeof r[key] !== 'boolean') return invalidDto(); stream[key] = r[key] as boolean | undefined; }
  return stream;
}
export function parseSimilar(v: unknown): SimilarDto[] {
  return array(obj(v).Items).map(value => { const r = obj(value); const tags = r.ImageTags == null ? {} : obj(r.ImageTags);
    if (Object.values(tags).some(v => typeof v !== 'string') || !/^[1-9]\d*$/u.test(str(r.Id))) return invalidDto();
    return { Id: str(r.Id), Name: str(r.Name), ImageTags: tags as Record<string, string>, ProductionYear: optionalNumber(r.ProductionYear), RunTimeTicks: optionalNumber(r.RunTimeTicks), CommunityRating: optionalNumber(r.CommunityRating) };
  });
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function invalidDto(): never {
  throw new Error('服务响应格式不正确');
}

function isCount(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
}

export function parseAuthStatus(value: unknown): AuthStatusDto {
  if (!isRecord(value) || typeof value.initialized !== 'boolean') return invalidDto();
  return { initialized: value.initialized };
}

export function parseUser(value: unknown): UserDto {
  if (!isRecord(value) || typeof value.Id !== 'string' || !value.Id
    || typeof value.Name !== 'string' || typeof value.ServerId !== 'string'
    || typeof value.ServerName !== 'string' || !isRecord(value.Policy)
    || typeof value.Policy.IsAdministrator !== 'boolean') return invalidDto();
  return {
    Id: value.Id, Name: value.Name, ServerId: value.ServerId, ServerName: value.ServerName,
    Policy: { IsAdministrator: value.Policy.IsAdministrator },
  };
}

export function parseLogin(value: unknown): LoginDto {
  if (!isRecord(value) || typeof value.AccessToken !== 'string' || !value.AccessToken
    || typeof value.ServerId !== 'string') return invalidDto();
  return { AccessToken: value.AccessToken, ServerId: value.ServerId, User: parseUser(value.User) };
}

export function parseLibraries(value: unknown): LibrariesDto {
  if (!isRecord(value) || (value.items !== null && !Array.isArray(value.items)) || !isCount(value.total)) return invalidDto();
  // Go marshals an empty, nil library slice as null, not [].
  if (value.items === null) return { items: null, total: value.total };
  const items = value.items.map((item: unknown): LibraryDto => {
    if (!isRecord(item) || !isCount(item.Id) || item.Id === 0
      || typeof item.Name !== 'string' || typeof item.Path !== 'string') return invalidDto();
    return { Id: item.Id, Name: item.Name, Path: item.Path };
  });
  return { items, total: value.total };
}

export function parseStatus(value: unknown): StatusDto {
  if (!isRecord(value) || !isCount(value.success) || !isCount(value.manual)
    || !isCount(value.pending) || !isCount(value.incompatible)) return invalidDto();
  return { success: value.success, manual: value.manual, pending: value.pending, incompatible: value.incompatible };
}

export function parseItemsTotal(value: unknown): number {
  if (!isRecord(value) || (value.items !== null && !Array.isArray(value.items)) || !isCount(value.total)) return invalidDto();
  return value.total;
}
