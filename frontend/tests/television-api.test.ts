import { describe, expect, it } from 'vitest'
import { parseLibraries } from '../src/api/contracts'
import { parseTelevision } from '../src/api/television'

describe('television API contracts', () => {
  it('accepts supported library types and rejects unsupported values', () => {
    for (const Type of ['movies', 'tvshows', 'mixed', '']) {
      expect(parseLibraries({ items: [{ Id: 1, Name: 'Library', Path: '/media', Type }], total: 1 }).items?.[0]?.Type).toBe(Type)
    }
    expect(() => parseLibraries({ items: [{ Id: 1, Name: 'Library', Path: '/media', Type: 'music' }], total: 1 })).toThrow()
  })

  it('rejects invalid item types, malformed folder IDs, unsafe episode IDs and non-string image tags', () => {
    const series = { Id: 'tv-series:2:L3Nob3c', Name: 'Drama', Type: 'Series', ImageTags: { Primary: 'cover' } }
    const invalid = [
      { ...series, Type: 'Movie' },
      { ...series, Id: '5' },
      { ...series, Id: 'tv-series:2:' },
      { ...series, ImageTags: { Primary: 5 } },
      { Id: '9007199254740992', Name: 'Episode', Type: 'Episode', ImageTags: {} },
    ]
    for (const item of invalid) expect(() => parseTelevision({ Items: [item], TotalRecordCount: 1 })).toThrow()
  })
})
