import { Button } from '@mantine/core';
import { useState } from 'react';

/** Shown only while the browser has not answered the permission prompt. */
export function EnableNotificationsButton() {
  if (typeof Notification === 'undefined') return null;
  return <EnableNotificationsPrompt />;
}

function EnableNotificationsPrompt() {
  const [permission, setPermission] = useState(Notification.permission);
  if (permission !== 'default') return null;
  return (
    <Button
      type="button"
      variant="default"
      onClick={() => {
        void Notification.requestPermission().then(setPermission);
      }}
    >
      Enable notifications
    </Button>
  );
}
