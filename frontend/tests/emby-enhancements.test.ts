import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createPinia, setActivePinia } from 'pinia';
import ItemHero from '../src/components/media/ItemHero.vue';
import GlobalSearchDialog from '../src/components/common/GlobalSearchDialog.vue';
import { catalogApi } from '../src/api/catalog';
import { actorsApi } from '../src/api/actors';
import { adminApi } from '../src/api/admin';
import type { MovieDto, FileDto } from '../src/api/contracts';

const sampleMovie: MovieDto = {
  id: 101,
  library_id: 1,
  source_path: '/media/test.mkv',
  Status: 'success',
  Title: '星际穿梭 4K HDR 测试片',
  OriginalTitle: 'Interstellar UHD',
  Plot: '太空探险与黑洞旅行。',
  PosterPath: '/posters/101.jpg',
  LandscapePath: '',
  BackdropPath: '',
  cover_url: '',
  Year: 2024,
  Rating: 9.3,
  RuntimeSeconds: 7200,
  Number: 'ABC-001',
};

const sampleFiles4K: FileDto[] = [
  {
    index: 0,
    role: 'main',
    name: 'test.mkv',
    path: '/media/test.mkv',
    size: 25000000000,
    probed: true,
    streams: [
      {
        Index: 0,
        Type: 'video',
        Codec: 'hevc',
        Width: 3840,
        Height: 2160,
        BitDepth: 10,
        Profile: 'Main 10',
      },
      {
        Index: 1,
        Type: 'audio',
        Codec: 'ac3',
        Channels: 6,
        ChannelLayout: '5.1',
      },
      {
        Index: 2,
        Type: 'subtitle',
        Codec: 'subrip',
        Language: 'chi',
      },
      {
        Index: 3,
        Type: 'subtitle',
        Codec: 'subrip',
        Language: 'eng',
      },
    ],
  },
];

describe('ItemHero Technical Spec Badges', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it('renders resolution, HDR, video codec, audio, and subtitle badges from media files', () => {
    const wrapper = mount(ItemHero, {
      props: {
        movie: sampleMovie,
        files: sampleFiles4K,
      },
    });

    const badges = wrapper.findAll('.tech-badge');
    expect(badges.length).toBe(5);

    const labels = badges.map(b => b.text());
    expect(labels).toContain('4K UHD');
    expect(labels).toContain('10-bit HDR');
    expect(labels).toContain('HEVC');
    expect(labels).toContain('Dolby 5.1');
    expect(labels).toContain('字幕 ×2');

    expect(wrapper.find('.tech-badge.res').text()).toBe('4K UHD');
    expect(wrapper.find('.tech-badge.hdr').text()).toBe('10-bit HDR');
    expect(wrapper.find('.tech-badge.codec').text()).toBe('HEVC');
    expect(wrapper.find('.tech-badge.audio').text()).toBe('Dolby 5.1');
    expect(wrapper.find('.tech-badge.sub').text()).toBe('字幕 ×2');
    wrapper.unmount();
  });

  it('handles 1080p AVC stereo media properly', () => {
    const files1080p: FileDto[] = [
      {
        index: 0,
        role: 'main',
        name: 'test1080.mp4',
        path: '/media/test1080.mp4',
        size: 5000000000,
        probed: true,
        streams: [
          {
            Index: 0,
            Type: 'video',
            Codec: 'h264',
            Width: 1920,
            Height: 1080,
            BitDepth: 8,
          },
          {
            Index: 1,
            Type: 'audio',
            Codec: 'aac',
            Channels: 2,
          },
        ],
      },
    ];

    const wrapper = mount(ItemHero, {
      props: {
        movie: sampleMovie,
        files: files1080p,
      },
    });

    const badges = wrapper.findAll('.tech-badge');
    const labels = badges.map(b => b.text());
    expect(labels).toContain('1080p FHD');
    expect(labels).toContain('AVC (H.264)');
    expect(labels).toContain('AAC · 立体声');
    wrapper.unmount();
  });
});

describe('GlobalSearchDialog Component', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.spyOn(adminApi, 'libraries').mockResolvedValue({
      items: [{ Id: 1, Name: '电影库', Path: '/movies' }],
      total: 1,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('does not render dialog modal when open is false', () => {
    const wrapper = mount(GlobalSearchDialog, {
      props: { open: false },
    });
    expect(document.body.querySelector('.search-backdrop')).toBeNull();
    wrapper.unmount();
  });

  it('renders quick navigation shortcuts when query is empty', async () => {
    const wrapper = mount(GlobalSearchDialog, {
      props: { open: true },
    });
    await flushPromises();

    const backdrop = document.body.querySelector('.search-backdrop');
    expect(backdrop).not.toBeNull();

    const items = document.body.querySelectorAll('.search-dialog-item');
    expect(items.length).toBeGreaterThan(0);
    wrapper.unmount();
  });

  it('searches movies and actors when query is typed', async () => {
    vi.useFakeTimers();

    vi.spyOn(catalogApi, 'items').mockResolvedValue({
      items: [sampleMovie],
      total: 1,
      image_tags: {},
    });

    vi.spyOn(actorsApi, 'list').mockResolvedValue({
      items: [{ name: '马修·麦康纳', movie_count: 5, has_image: false, image_tag: '' }],
      total: 1,
      stats: { total: 1, ready: 1, missing: 0 },
    });

    const wrapper = mount(GlobalSearchDialog, {
      props: { open: true },
    });
    await flushPromises();

    const input = document.body.querySelector('.search-input') as HTMLInputElement;
    input.value = '星际';
    input.dispatchEvent(new Event('input'));

    vi.advanceTimersByTime(200);
    await flushPromises();

    expect(catalogApi.items).toHaveBeenCalled();
    expect(actorsApi.list).toHaveBeenCalled();

    const text = document.body.textContent || '';
    expect(text).toContain('星际穿梭 4K HDR 测试片');
    expect(text).toContain('马修·麦康纳');

    vi.useRealTimers();
    wrapper.unmount();
  });

  it('emits close event when pressing Escape', async () => {
    const wrapper = mount(GlobalSearchDialog, {
      props: { open: true },
    });
    await flushPromises();

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(wrapper.emitted('close')).toBeTruthy();
    wrapper.unmount();
  });
});
