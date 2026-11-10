import { vi } from 'vitest';

export const locationAssign = vi.fn();

/** jsdom's Location.assign throws. Tests assert against this stand-in. */
export function installTestLocation() {
  const fake = {
    assign: locationAssign,
    replace: vi.fn(),
    reload: vi.fn(),
    pathname: '/',
    search: '',
    hash: '',
    href: 'http://localhost/',
    origin: 'http://localhost',
    protocol: 'http:',
    host: 'localhost',
    hostname: 'localhost',
    port: '',
    ancestorOrigins: { length: 0 },
    toString() {
      return 'http://localhost/';
    },
  };
  Object.defineProperty(window, 'location', {
    configurable: true,
    writable: true,
    value: fake,
  });
}

export function setTestPath(pathname: string, search = '') {
  window.location.pathname = pathname;
  window.location.search = search;
}
