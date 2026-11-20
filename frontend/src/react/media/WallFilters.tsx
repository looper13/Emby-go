import {
  CheckCircle2, Folder, Heart, RotateCcw, Search, SlidersHorizontal, Sparkles, X,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'
import type { LibraryDto } from '../../api/contracts'
import { entityParams, statusText } from '../../lib/media'
import type { CardSize } from '../preferences-store'
import './wall-filters.css'

interface Props {
  query: URLSearchParams
  libraries: LibraryDto[]
  search: string
  canManage?: boolean
  cardSize: CardSize
  onFilter: (key: string, value: string, toggle?: boolean) => void
  onSearch: (value: string) => void
  onCardSize: (size: CardSize) => void
  onClearEntity: () => void
  onClearAll: () => void
  onShuffle: () => void
  entityAction?: ReactNode
}

const statuses = [
  ['', '全部'], ['success', '已入库'], ['manual', '手动'], ['pending', '待补录'], ['incompatible', '不兼容'],
]
const scrapeStates = [['failed', '刮削失败'], ['confirm', '待人工确认']]
const sorts = [
  ['datecreated', '最近入库'], ['title', '标题'], ['year', '年份'],
  ['communityrating', '评分'], ['date', '首映日期'], ['random', '随机'],
]

export function WallFilters({
  query, libraries, search, canManage = false, cardSize, onFilter, onSearch,
  onCardSize, onClearEntity, onClearAll, onShuffle, entityAction,
}: Props) {
  const [expanded, setExpanded] = useState(true)
  const value = (key: string) => query.get(key) || ''
  const activeEntity = entityParams
    .map(([key, label]) => ({ key, label, value: value(key) }))
    .find((item) => item.value)
  const activeFilters = [
    value('library_id') ? {
      key: 'library_id',
      label: `媒体库：${libraries.find((library) => String(library.Id) === value('library_id'))?.Name || value('library_id')}`,
    } : null,
    value('status') ? { key: 'status', label: `入库：${statusText[value('status')] || value('status')}` } : null,
    value('scrape') ? { key: 'scrape', label: `刮削：${scrapeStates.find(([key]) => key === value('scrape'))?.[1] || value('scrape')}` } : null,
    value('favorite') === 'true' ? { key: 'favorite', label: '仅收藏' } : null,
  ].filter((filter): filter is { key: string; label: string } => filter !== null)
  const sort = value('sort') || 'datecreated'
  const order = value('order') || (sort === 'title' ? 'asc' : 'desc')
  const activeCount = Number(Boolean(value('library_id'))) + Number(Boolean(value('status')))
    + Number(Boolean(value('scrape'))) + Number(value('favorite') === 'true') + Number(Boolean(activeEntity))

  return (
    <section className="wall-filters-workspace" aria-label="媒体筛选">
      <div className={`wall-filters-card${expanded ? ' is-expanded' : ''}`}>
        <div className="wall-filters-bar">
          <label className="wall-search-field">
            <Search size={15} aria-hidden="true" />
            <input id="item-search" value={search} placeholder="搜索标题 / 番号 / 原名" aria-label="搜索标题 / 番号 / 原名"
              onChange={(event) => onSearch(event.currentTarget.value)} />
            {search && <button type="button" className="wall-search-clear" title="清空搜索" aria-label="清空搜索" onClick={() => onSearch('')}>
              <X size={13} aria-hidden="true" />
            </button>}
          </label>

          <button type="button" className={`wall-filter-toggle${expanded || activeCount ? ' is-active' : ''}`}
            aria-expanded={expanded} aria-controls="wall-filter-panel" title="切换筛选面板" onClick={() => setExpanded((open) => !open)}>
            <SlidersHorizontal size={14} aria-hidden="true" /><span>筛选</span>
            {activeCount > 0 && <span className="wall-filter-count">{activeCount}</span>}
          </button>

          <div className="wall-filter-tools">
            <select id="item-sort" aria-label="排序" value={sort}
              onChange={(event) => onFilter('sort', event.currentTarget.value)}>
              {sorts.map(([key, label]) => <option key={key} value={key}>{label}</option>)}
            </select>
            {sort !== 'random'
              ? <select id="item-order" aria-label="排序方向" value={order}
                onChange={(event) => onFilter('order', event.currentTarget.value)}>
                <option value="asc">正序</option><option value="desc">倒序</option>
              </select>
              : <button id="wall-shuffle" className="wall-shuffle-button" type="button" title="换一批随机内容" onClick={onShuffle}>
                <Sparkles size={14} aria-hidden="true" /><span>换一批</span>
              </button>}
            <label className="wall-favorite-filter" title="仅查看收藏的媒体">
              <input type="checkbox" checked={value('favorite') === 'true'} aria-label="仅收藏"
                onChange={(event) => onFilter('favorite', event.currentTarget.checked ? 'true' : '')} />
              <Heart size={13} aria-hidden="true" /><span>仅收藏</span>
            </label>
            <div className="wall-card-size" role="group" aria-label="卡片尺寸">
              {(['compact', 'normal', 'large'] as const).map((size, index) => (
                <button key={size} type="button" className={cardSize === size ? 'is-active' : ''}
                  aria-pressed={cardSize === size} aria-label={['紧凑尺寸', '标准尺寸', '大图尺寸'][index]}
                  title={['紧凑卡片', '标准卡片', '大图卡片'][index]} onClick={() => onCardSize(size)}>
                  {['小', '中', '大'][index]}
                </button>
              ))}
            </div>
          </div>
        </div>

        <div id="wall-filter-panel" className="wall-filter-panel" hidden={!expanded}>
          {libraries.length > 0 && <div className="wall-filter-group">
            <div className="wall-filter-label"><Folder size={13} aria-hidden="true" /><span>媒体库</span><span className="wall-group-count">{libraries.length}</span></div>
            <div className="wall-filter-options">
              {libraries.length <= 8 ? <>
                <button type="button" className="wall-pill" data-lib="" aria-pressed={!value('library_id')}
                  onClick={() => onFilter('library_id', '')}>全部库</button>
                {libraries.map((library) => <button key={library.Id} type="button" className="wall-pill"
                  data-lib={library.Id} aria-pressed={value('library_id') === String(library.Id)}
                  onClick={() => onFilter('library_id', String(library.Id))}>{library.Name}</button>)}
              </> : <select id="item-lib" aria-label="媒体库" value={value('library_id')}
                onChange={(event) => onFilter('library_id', event.currentTarget.value)}>
                <option value="">全部库</option>{libraries.map((library) => <option key={library.Id} value={library.Id}>{library.Name}</option>)}
              </select>}
            </div>
          </div>}

          {canManage && <div className="wall-filter-group">
            <div className="wall-filter-label"><CheckCircle2 size={13} aria-hidden="true" /><span>入库状态</span></div>
            <div className="wall-filter-options">
              {statuses.map(([key, label]) => <button key={key || 'all'} type="button" className="wall-pill"
                data-status={key} aria-pressed={value('status') === key} onClick={() => onFilter('status', key)}>{label}</button>)}
            </div>
          </div>}

          {canManage && <div className="wall-filter-group">
            <div className="wall-filter-label"><Sparkles size={13} aria-hidden="true" /><span>刮削状态</span></div>
            <div className="wall-filter-options">
              {scrapeStates.map(([key, label]) => <button key={key} type="button" className="wall-pill"
                data-scrape={key} aria-pressed={value('scrape') === key}
                onClick={() => onFilter('scrape', key, true)}>{label}</button>)}
            </div>
          </div>}

          {activeCount > 0 && <div className="wall-filter-footer">
            <button type="button" onClick={onClearAll}><RotateCcw size={12} aria-hidden="true" /><span>清除所有筛选</span></button>
          </div>}
        </div>
      </div>

      {activeFilters.length > 0 && <div className="wall-active-filters" aria-label="已选筛选条件">
        {activeFilters.map((filter) => <button key={filter.key} type="button" className="wall-active-filter"
          title={`清除${filter.label}`} aria-label={`清除${filter.label}`} onClick={() => onFilter(filter.key, '')}>
          <span>{filter.label}</span><X size={12} aria-hidden="true" />
        </button>)}
      </div>}
      {activeEntity && <div className="wall-entity-filter filters">
        <span className="filter-chip"><span>{activeEntity.label}：</span><strong>{activeEntity.value}</strong>
          <button id="entity-clear" type="button" title="清除该筛选" aria-label="清除该筛选" onClick={onClearEntity}><X size={13} /></button>
        </span>
        {entityAction}
      </div>}
      {canManage && <span className="wall-hidden-status" aria-hidden="true">{statusText[value('status')] || ''}</span>}
    </section>
  )
}
