import { http, HttpResponse, ws } from 'msw';
import { setupServer } from 'msw/node';

/** Mock for `useRealtime`. A test can `client.send` / `client.close` on `chatSocket.clients`. */
export const chatSocket = ws.link('ws://localhost/ws');

export const server = setupServer(
  // The messenger polls this on every render of the page. Zero keeps the line hidden.
  http.get('/api/chats/available', () => HttpResponse.json({ count: 0 })),
  // The shell loads the chat list on every signed-in page, for the unread badge.
  http.get('/api/chats', () => HttpResponse.json([])),
  chatSocket.addEventListener('connection', () => {
    // Accept the socket so an unhandled upgrade is not an error. Tests drive `chatSocket.clients`.
  }),
);
