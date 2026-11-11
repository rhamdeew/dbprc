import { Navigate, Route, Routes } from 'react-router';
import { useMe } from '../api/me';
import { Shell } from '../components/Shell';
import { AdminPage } from '../pages/AdminPage';
import { LoginPage } from '../pages/LoginPage';
import { MessengerPage } from '../pages/MessengerPage';
import { ProfilePage } from '../pages/ProfilePage';
import { RequireAuth } from './RequireAuth';

function OwnProfile() {
  const { data: me } = useMe();
  if (!me) return null;
  return <Navigate to={`/u/${me.id}`} replace />;
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<RequireAuth />}>
        <Route element={<Shell />}>
          <Route path="/" element={<OwnProfile />} />
          <Route path="/u/:id" element={<ProfilePage />} />
          <Route path="/messenger/:chatId?" element={<MessengerPage />} />
          <Route path="/admin" element={<AdminPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Route>
    </Routes>
  );
}
