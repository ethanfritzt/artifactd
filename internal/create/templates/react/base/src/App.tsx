import { Badge, Card, Container, Group, Stack, Text, ThemeIcon, Title } from "@mantine/core";

export function App() {
  return (
    <Container size="md" py={{ base: 48, sm: 80 }}>
      <Stack gap="xl">
        <Stack gap="xs">
          <Badge variant="light" w="fit-content">
            Artifactd · React
          </Badge>
          <Title order={1}>A beautiful starting point</Title>
          <Text c="dimmed" maw={620} size="lg">
            Shape this standalone artifact around the problem you want to solve. Keep the interface focused, responsive, and useful.
          </Text>
        </Stack>
        <Card withBorder radius="lg" padding="xl" shadow="sm">
          <Group align="flex-start" wrap="nowrap">
            <ThemeIcon size={44} radius="md" variant="light" color="cyan">
              ✦
            </ThemeIcon>
            <Stack gap={4}>
              <Text fw={700}>Ready to build</Text>
              <Text c="dimmed" size="sm">
                Edit src/App.tsx, then run npm run build and publish the dist directory.
              </Text>
            </Stack>
          </Group>
        </Card>
      </Stack>
    </Container>
  );
}
