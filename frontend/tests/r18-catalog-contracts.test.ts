import { describe, expect, it } from 'vitest'
import { readR18CatalogQuery, serializeR18CatalogQuery } from '../src/composables/r18-catalog'

describe('AVdb catalog query contract', () => {
  it('normalizes default mode, page bounds, and supported sorts', () => {
    expect(serializeR18CatalogQuery(readR18CatalogQuery({ library_id: '2' }))).toEqual({
      library_id: '2', type: 'all', filter: 'magnets', sort: 'release', order: 'desc', page: '1', page_size: '24',
    })
    expect(readR18CatalogQuery({ page: '10000', page_size: '999' })).toMatchObject({ page: 1, page_size: '24' })
    expect(readR18CatalogQuery({ type: '4', sort: 'update' })).toMatchObject({ mode: 'latest', type: '4', sort: 'update' })
    expect(readR18CatalogQuery({ mode: 'categories', sort: 'update' }).sort).toBe('release')
    for (const page of [0, -1, 10000, 1.5, NaN]) expect(() => serializeR18CatalogQuery(readR18CatalogQuery({}), page)).toThrow()
  })

  it('deduplicates and validates category tags independently from basic and date filters', () => {
    const query = readR18CatalogQuery({
      library_id: '2', mode: 'categories', type: '0', tag_ids: '0:23,0:1,0:23', main_filter: 'm,p,p,c',
      year: '2024', month: '6', duration: '90-120', sort: 'score', order: 'asc', page: '4', page_size: '50',
    })
    expect(serializeR18CatalogQuery(query)).toEqual({
      library_id: '2', mode: 'categories', type: '0', filter: 'magnets', sort: 'score', order: 'asc', page: '4', page_size: '50',
      tag_ids: '0:1,0:23', main_filter: 'c,m,p', year: '2024', month: '6', duration: '90-120',
    })
    const full = readR18CatalogQuery({ mode: 'categories', tag_ids: '0:1,0:2,0:3,0:4,0:5', main_filter: 'p,m,c,s,i,v', year: '2024', month: '12', duration: 'gt-120' })
    expect(serializeR18CatalogQuery(full)).toMatchObject({ tag_ids: '0:1,0:2,0:3,0:4,0:5', main_filter: 'c,i,m,p,s,v' })
    for (const overrides of [
      { tag_ids: ['0:1', '0:2', '0:3', '0:4', '0:5', '0:6'] }, { tag_ids: ['0:../'] }, { type: '1', tag_ids: ['0:1'] },
      { main_filter: ['unsafe'] }, { month: '13' }, { duration: 'guessed' }, { year: '-1' }, { sort: 'update' },
    ]) expect(() => serializeR18CatalogQuery({ ...query, ...overrides })).toThrow()
  })

  it('keeps chart modes separate from catalog filters and enforces the TOP250 limit', () => {
    for (const mode of ['top250', 'ranking', 'playback', 'popular'] as const) {
      const query = serializeR18CatalogQuery(readR18CatalogQuery({ mode, type: 'all', filter: 'magnets', tag_ids: '0:23', main_filter: 'p', month: '6', duration: 'lt-45', sort: 'score', order: 'asc' }))
      expect(query).toMatchObject({ mode, type: mode === 'ranking' ? '0' : 'all', filter: 'all' })
      for (const key of ['sort', 'order', 'tag_ids', 'main_filter', 'year', 'month', 'duration']) expect(query).not.toHaveProperty(key)
      if (mode !== 'top250') expect(query.period).toBe('daily')
    }
    expect(serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'top250', year: '2024', ignore_watched: 'false' }))).toMatchObject({ type: 'all', year: '2024', ignore_watched: 'false' })
    expect(() => serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'top250', type: '0', year: '2024' }))).toThrow()
    for (const size of ['24', '40', '50']) {
      const state = readR18CatalogQuery({ mode: 'top250', page_size: size })
      const max = Math.ceil(250 / Number(size))
      expect(serializeR18CatalogQuery(state, max).page).toBe(String(max))
      expect(() => serializeR18CatalogQuery(state, max + 1)).toThrow(/TOP250/u)
    }
  })

  it('treats search as upstream relevance and drops incompatible ranking filters', () => {
    expect(serializeR18CatalogQuery(readR18CatalogQuery({
      mode: 'top250', search: ' TEST ', type: '3', year: '2024', tag_ids: '0:23', period: 'monthly', ignore_watched: 'true', sort: 'score', order: 'asc',
    }))).toEqual({ library_id: '', type: '3', filter: 'magnets', page: '1', page_size: '24', search: 'TEST' })
    expect(() => serializeR18CatalogQuery(readR18CatalogQuery({ mode: 'latest', sort: 'update', year: '2024' }))).toThrow(/更新排序不能叠加年份/u)
  })
})
