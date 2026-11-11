import { Alert, Button, Center, Stack, Text, Title } from '@mantine/core';
import { useSearchParams } from 'react-router';
import { Panel } from '../components/Panel';
import { HEADER, PANEL } from '../theme/theme';
import { googleStartPath, loginErrorMessage } from './login';

/** The only page a logged-out visitor sees. */
export function LoginPage() {
  const [params] = useSearchParams();
  const message = loginErrorMessage(params.get('error'));

  return (
    <Center mih="100vh" bg={PANEL} p="md">
      <Panel maw={420} w="100%">
        <Stack gap="sm">
          <Text fw={700} fz={22} lh={1} c={HEADER}>
            ARC
          </Text>
          <Title order={1}>Sign in</Title>
          <Text>Async Random Coffee: meet your teammates when you have time</Text>
          {message ? (
            <Alert color="red" variant="light">
              {message}
            </Alert>
          ) : null}
          <Button component="a" href={googleStartPath(params.get('next'))} fullWidth>
            Sign in with Google
          </Button>
        </Stack>
      </Panel>
    </Center>
  );
}
