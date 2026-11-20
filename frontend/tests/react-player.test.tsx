import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useReactPlayerStore } from '../src/react/player-store'
import detailFixture from './fixtures/json/item-detail.json'

function PlayerState() {
  const current = useReactPlayerStore((state) => state.current)
  return current ? <output data-testid="current-player">{`${current.id}:${current.url}:${current.itemId}`}</output> : null
}

describe('React player store', () => {
  it('opens media, local sources and trailers with the existing stream contract', () => {
    const player = useReactPlayerStore.getState()
    player.media(detailFixture.movie as never, undefined, { id: 52, container: 'mkv' })
    expect(useReactPlayerStore.getState().current).toMatchObject({
      url: '/Videos/52/stream', proxyURL: '/Videos/52/proxy', itemId: '5', sourceItemId: '52',
    })
    player.local({ item_id: 'catalog:source 1', name: 'Local', container: 'mp4' } as never)
    expect(useReactPlayerStore.getState().current).toMatchObject({
      title: 'Local', url: '/Videos/catalog%3Asource%201/stream', itemId: 'catalog:source 1',
    })
    player.trailer(detailFixture.movie as never, 'https://media.example/trailer.m3u8')
    expect(useReactPlayerStore.getState().current).toMatchObject({
      title: '预告片 · ' + detailFixture.movie.Title, url: '/Videos/5/trailer', itemId: '5',
    })
    player.close()
  })

  it('notifies React subscribers and restores focus after closing', async () => {
    const user = userEvent.setup()
    render(
      <>
        <button type="button" onClick={() => useReactPlayerStore.getState().open({ title: 'movie', url: '/Videos/5/stream', type: 'mp4', itemId: '5' })}>
          Play
        </button>
        <PlayerState />
      </>,
    )
    const trigger = screen.getByRole('button', { name: 'Play' })
    await user.click(trigger)
    expect(screen.getByTestId('current-player')).toHaveTextContent('/Videos/5/stream')
    useReactPlayerStore.getState().close()
    await Promise.resolve()
    expect(trigger).toHaveFocus()
  })
})
