import { Alert, Button, Center, Stack, Text, Title, VisuallyHidden } from '@mantine/core';
import { useSearchParams } from 'react-router';
import { BEVEL, BORDER, HEADER, PANEL } from '../theme/theme';
import { googleStartPath, loginErrorMessage } from './login';

/** The only page a logged-out visitor sees. */
export function LoginPage() {
  const [params] = useSearchParams();
  const message = loginErrorMessage(params.get('error'));

  return (
    <Center mt={48} px="md">
      <Stack
        w="100%"
        maw={380}
        gap="md"
        p="lg"
        style={{ background: PANEL, border: `1px solid ${BORDER}`, boxShadow: 'inset 0 0 0 1px #ffffff' }}
      >
        <VisuallyHidden>
          <Title order={1}>Sign in</Title>
        </VisuallyHidden>
        <Text fw={700} c={HEADER} fz={44} lh={1.1} style={{ letterSpacing: -2 }}>
          [arc]
        </Text>
        <Text size="lg">
          Async Random Coffee <span aria-hidden="true">☕</span>
        </Text>
        <Text size="lg" fs="italic">
          Meet your teammates when you have time
        </Text>
        {message ? (
          <Alert color="red" variant="light">
            {message}
          </Alert>
        ) : null}
        <Button component="a" href={googleStartPath(params.get('next'))} size="sm" fullWidth fw={700} style={BEVEL}>
          Sign in with Google
        </Button>
      </Stack>
    </Center>
  );
}
