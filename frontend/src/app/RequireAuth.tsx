import { Center, Loader } from '@mantine/core';
import { Navigate, Outlet, useLocation } from 'react-router';
import { useMe } from '../api/me';
import { PANEL } from '../theme/theme';

/** Every route except `/login` needs a session user. */
export function RequireAuth() {
  const location = useLocation();
  const me = useMe();

  if (me.isPending) {
    return (
      <Center mih="100vh" bg={PANEL}>
        <Loader color="arcBlue" aria-label="Loading" />
      </Center>
    );
  }

  if (!me.data) {
    const next = `${location.pathname}${location.search}`;
    return <Navigate to={`/login?next=${encodeURIComponent(next)}`} replace />;
  }

  return <Outlet />;
}
