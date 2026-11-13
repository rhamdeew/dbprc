import { AppShell, Box, Flex, UnstyledButton } from '@mantine/core';
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
  whiteSpace: 'nowrap' as const,
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
      style={{ color: '#ffffff', whiteSpace: 'nowrap' }}
      onClick={() => logout.mutate()}
    >
      Log out
    </UnstyledButton>
  );
}

export function Shell() {
  const { data: me } = useMe();

  return (
    <AppShell header={{ height: { base: 84, sm: 42 } }} padding={0} withBorder={false} offsetScrollbars={false}>
      <AppShell.Header
        style={{
          backgroundColor: HEADER,
          borderBottom: '1px solid #2f4779',
          boxShadow: 'none',
        }}
      >
        <Flex
          h="100%" px="md" py={{ base: 8, sm: 0 }} gap={8}
          direction={{ base: 'column', sm: 'row' }}
          align={{ base: 'stretch', sm: 'center' }} justify="space-between"
          maw={CONTENT_WIDTH} mx="auto" w="100%"
        >
          <Flex align="center" gap={{ base: 6, sm: 'md' }} justify="space-between" wrap="nowrap">
            <NavLink to="/" end style={{ color: '#ffffff', fontWeight: 700, fontSize: 18, textDecoration: 'none' }}>
              ARC
            </NavLink>
            <Flex align="center" component="nav" aria-label="Primary" gap={{ base: 6, sm: 'md' }} wrap="nowrap">
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
            </Flex>
          </Flex>
          <SearchSlot />
        </Flex>
      </AppShell.Header>
      <AppShell.Main style={{ backgroundColor: PANEL, minHeight: '100vh' }}>
        <Box data-testid="content" mx="auto" w="100%" px="md" py="md" style={{ maxWidth: CONTENT_WIDTH }}>
          <Outlet />
        </Box>
      </AppShell.Main>
    </AppShell>
  );
}
