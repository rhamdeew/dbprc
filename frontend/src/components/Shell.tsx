import { AppShell, Box, Group, UnstyledButton } from '@mantine/core';
import { NavLink, Outlet } from 'react-router';
import { useLogout } from '../api/auth';
import { useMe } from '../api/me';
import { CONTENT_WIDTH, HEADER, PANEL } from '../theme/theme';
import { SearchSlot } from './SearchSlot';

const linkStyle = ({ isActive }: { isActive: boolean }) => ({
  color: '#ffffff',
  fontWeight: isActive ? 700 : 400,
  textDecoration: isActive ? 'underline' : 'none',
  fontSize: 12,
});

function TopLink({ to, children, end = false }: { to: string; children: string; end?: boolean }) {
  return (
    <NavLink to={to} end={end} style={linkStyle}>
      {children}
    </NavLink>
  );
}

function LogOutButton() {
  const logout = useLogout();
  return (
    <UnstyledButton
      type="button"
      fw={600}
      fz={12}
      style={{ color: '#ffffff' }}
      onClick={() => logout.mutate()}
    >
      Log out
    </UnstyledButton>
  );
}

export function Shell() {
  const { data: me } = useMe();

  return (
    <AppShell header={{ height: 42 }} padding={0} withBorder={false} offsetScrollbars={false}>
      <AppShell.Header
        style={{
          backgroundColor: HEADER,
          borderBottom: '1px solid #2f4779',
          boxShadow: 'none',
        }}
      >
        <Group h="100%" px="md" justify="space-between" wrap="nowrap" maw={CONTENT_WIDTH} mx="auto" w="100%">
          <Group gap="md" wrap="nowrap">
            <NavLink to="/" end style={{ color: '#ffffff', fontWeight: 700, fontSize: 18, textDecoration: 'none' }}>
              ARC
            </NavLink>
            <Group component="nav" aria-label="Primary" gap="md" wrap="nowrap">
              {me ? (
                <TopLink to={`/u/${me.id}`} end>
                  My profile
                </TopLink>
              ) : null}
              <TopLink to="/messenger">Messenger</TopLink>
              {me?.isAdmin ? (
                <TopLink to="/admin" end>
                  Admin
                </TopLink>
              ) : null}
              <LogOutButton />
            </Group>
          </Group>
          <SearchSlot />
        </Group>
      </AppShell.Header>
      <AppShell.Main style={{ backgroundColor: PANEL, minHeight: '100vh' }}>
        <Box data-testid="content" mx="auto" w="100%" px="md" py="md" style={{ maxWidth: CONTENT_WIDTH }}>
          <Outlet />
        </Box>
      </AppShell.Main>
    </AppShell>
  );
}
