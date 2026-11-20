import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'
import { ArtworkGallery } from '../src/react/media/ArtworkGallery'

const sources = ['/api/r18/libraries/8/videos/film/images/0', '/api/r18/libraries/8/videos/film/images/1']

afterEach(() => {
  cleanup()
  document.body.style.overflow = ''
})

describe('React artwork gallery', () => {
  it('opens, preloads neighbors, wraps keyboard navigation, traps focus and restores focus on close', async () => {
    const user = userEvent.setup()
    document.body.style.overflow = 'auto'
    const view = render(<ArtworkGallery sources={sources} />)
    const trigger = screen.getByRole('button', { name: '查看剧照 1' })
    await user.click(trigger)

    const dialog = screen.getByRole('dialog', { name: '剧照 1 / 2' })
    expect(dialog).toBeInTheDocument()
    expect(document.body.style.overflow).toBe('hidden')
    expect(view.container.querySelector('.react-artwork-image')).toHaveAttribute('src', sources[0])
    expect(view.container.querySelectorAll('.react-artwork-preload')).toHaveLength(2)
    expect(document.activeElement).toBe(screen.getByRole('button', { name: '关闭剧照' }))

    fireEvent.keyDown(window, { key: 'ArrowLeft' })
    expect(await screen.findByRole('dialog', { name: '剧照 2 / 2' })).toBeInTheDocument()
    expect(view.container.querySelector('.react-artwork-image')).toHaveAttribute('src', sources[1])
    fireEvent.keyDown(window, { key: 'ArrowRight' })
    expect(await screen.findByRole('dialog', { name: '剧照 1 / 2' })).toBeInTheDocument()

    const close = screen.getByRole('button', { name: '关闭剧照' })
    fireEvent.keyDown(close, { key: 'Tab', shiftKey: true })
    expect(screen.getByRole('button', { name: '下一张剧照' })).toHaveFocus()
    fireEvent.keyDown(screen.getByRole('button', { name: '下一张剧照' }), { key: 'Tab' })
    expect(close).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(document.body.style.overflow).toBe('auto')
    expect(trigger).toHaveFocus()
    document.body.style.overflow = ''
  })

  it('closes without reopening when images are hidden or the movie changes', async () => {
    const user = userEvent.setup()
    const view = render(<ArtworkGallery sources={sources} />)
    await user.click(screen.getByRole('button', { name: '查看剧照 1' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    view.rerender(<ArtworkGallery sources={sources} hidden />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(document.body.style.overflow).toBe('')
    view.rerender(<ArtworkGallery sources={sources} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '查看剧照 1' }))
    view.rerender(<ArtworkGallery sources={['/other-film/images/0']} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(view.container.querySelector('.react-detail-artwork-thumb img')).toHaveAttribute('src', '/other-film/images/0')
  })
})
