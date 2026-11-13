import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { MemoryRouter, useLocation } from 'react-router';
import { describe, expect, it } from 'vitest';
import { render } from '../test/render';
import { server } from '../test/server';
import { SearchSlot } from './SearchSlot';

function LocationEcho() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

const ada = {
  id: 7,
  name: 'Ada Lovelace',
  email: 'ada@corp.test',
  avatarUrl: '/uploads/avatar/ada.jpg',
};

const adam = {
  id: 8,
  name: 'Adam West',
  email: 'adam@corp.test',
  avatarUrl: null,
};

describe('colleague search', () => {
  it('waits for 4 characters, then opens the profile that was picked', async () => {
    const user = userEvent.setup();
    const calls: string[] = [];
    server.use(
      http.get('/api/users/search', ({ request }) => {
        const q = new URL(request.url).searchParams.get('q') ?? '';
        calls.push(q);
        if (q === 'adam') return HttpResponse.json([ada, adam]);
        return HttpResponse.json([]);
      }),
    );
    render(
      <MemoryRouter initialEntries={['/messenger']}>
        <LocationEcho />
        <SearchSlot />
      </MemoryRouter>,
    );

    const input = screen.getByRole('combobox', { name: 'Search colleagues' });
    await user.type(input, 'ada');
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 400));
    });
    expect(calls).toEqual([]);
    expect(screen.queryByRole('option', { hidden: true })).not.toBeInTheDocument();

    await user.type(input, 'm');
    // jsdom gives the input no box, so Floating UI hides the dropdown. The options are still there.
    const option = await screen.findByRole('option', { name: /Adam West/, hidden: true });
    expect(calls).toEqual(['adam']);
    expect(screen.getByRole('option', { name: /Ada Lovelace/, hidden: true }).querySelector('img')).toHaveAttribute(
      'src',
      '/uploads/avatar/ada.jpg',
    );
    expect(option).toHaveTextContent('adam@corp.test');

    fireEvent.click(option);
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/u/8'));
    expect(input).toHaveValue('');
  });
});
