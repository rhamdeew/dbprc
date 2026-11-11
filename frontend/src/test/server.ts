import { ws } from 'msw';
import { setupServer } from 'msw/node';

/** Mock for `useRealtime`. A test can `client.send` / `client.close` on `chatSocket.clients`. */
export const chatSocket = ws.link('ws://localhost/ws');

export const server = setupServer(
  chatSocket.addEventListener('connection', () => {
    // Accept the socket so an unhandled upgrade is not an error. Tests drive `chatSocket.clients`.
  }),
);
