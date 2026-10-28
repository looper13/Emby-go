import { describe, expect, it, beforeEach } from 'vitest';
import { useSidebar } from '../src/composables/useSidebar';
import { useCardSize } from '../src/composables/useCardSize';

describe('responsive layout composables', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  describe('useSidebar', () => {
    it('initializes and toggles collapsed state', () => {
      const { isCollapsed, toggle, setCollapsed } = useSidebar();
      setCollapsed(false);
      expect(isCollapsed.value).toBe(false);

      toggle();
      expect(isCollapsed.value).toBe(true);
      expect(localStorage.getItem('emby_sidebar_collapsed')).toBe('true');

      toggle();
      expect(isCollapsed.value).toBe(false);
      expect(localStorage.getItem('emby_sidebar_collapsed')).toBe('false');
    });
  });

  describe('useCardSize', () => {
    it('sets and persists card size and updates CSS custom property', () => {
      const { cardSize, setSize, sizes } = useCardSize();
      setSize('normal');
      expect(cardSize.value).toBe('normal');
      expect(document.documentElement.style.getPropertyValue('--wall-card-min-width')).toBe(sizes.normal);

      setSize('compact');
      expect(cardSize.value).toBe('compact');
      expect(localStorage.getItem('emby_wall_card_size')).toBe('compact');
      expect(document.documentElement.style.getPropertyValue('--wall-card-min-width')).toBe(sizes.compact);

      setSize('large');
      expect(cardSize.value).toBe('large');
      expect(localStorage.getItem('emby_wall_card_size')).toBe('large');
      expect(document.documentElement.style.getPropertyValue('--wall-card-min-width')).toBe(sizes.large);
    });
  });
});
