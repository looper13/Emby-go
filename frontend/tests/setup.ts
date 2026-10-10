import { beforeEach, vi } from 'vitest';
// jsdom has no layout engine; scrolling and focus restoration are verified in E2E.
beforeEach(() => { vi.spyOn(window, 'scrollTo').mockImplementation(() => {}); });
