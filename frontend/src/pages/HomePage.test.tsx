import { screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { render } from '../test/render';
import { HomePage } from './HomePage';

afterEach(() => vi.unstubAllGlobals());

describe('HomePage', () => {
  it('shows env and version from the API', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ env: 'staging', version: 'abc123' }) }));
    render(<HomePage />);
    expect(await screen.findByText('staging · abc123')).toBeInTheDocument();
  });

  it('shows an error when the API fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502 }));
    render(<HomePage />);
    expect(await screen.findByText('API unavailable')).toBeInTheDocument();
  });
});
