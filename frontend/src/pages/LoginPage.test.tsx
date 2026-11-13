import { screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';
import { render } from '../test/render';
import { LoginPage } from './LoginPage';

function renderLogin(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <LoginPage />
    </MemoryRouter>,
  );
}

describe('login page', () => {
  it('shows the wordmark, tagline, and a Google link with no next', () => {
    renderLogin('/login');

    expect(screen.getByText('[arc]')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.getByText(/^Async Random Coffee/)).toBeInTheDocument();
    expect(screen.getByText('Meet your teammates when you have time')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in with Google' })).toHaveAttribute('href', '/auth/google/start');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'ARC' })).not.toBeInTheDocument();
  });

  it.each([
    ['domain', 'Use your company Google account'],
    ['unverified', 'Your Google email is not verified.'],
    ['state', 'Sign-in expired. Try again.'],
    ['provider', 'Google sign-in is unavailable. Try again.'],
    ['nope', 'Sign-in failed. Try again.'],
  ])('maps ?error=%s to a friendly message', (code, message) => {
    renderLogin(`/login?error=${code}`);
    expect(screen.getByRole('alert')).toHaveTextContent(message);
  });

  it('passes next through to the Google start URL', () => {
    renderLogin('/login?error=domain&next=%2Fmessenger%3Fx%3D1');

    expect(screen.getByRole('link', { name: 'Sign in with Google' })).toHaveAttribute(
      'href',
      '/auth/google/start?next=%2Fmessenger%3Fx%3D1',
    );
    expect(screen.getByRole('alert')).toHaveTextContent('Use your company Google account');
  });

  it('passes an unsafe next through unchanged', () => {
    renderLogin('/login?next=%2F%2Fevil.example');
    expect(screen.getByRole('link', { name: 'Sign in with Google' })).toHaveAttribute(
      'href',
      '/auth/google/start?next=%2F%2Fevil.example',
    );
  });

  it('omits an empty next', () => {
    renderLogin('/login?next=');
    expect(screen.getByRole('link', { name: 'Sign in with Google' })).toHaveAttribute('href', '/auth/google/start');
  });
});
