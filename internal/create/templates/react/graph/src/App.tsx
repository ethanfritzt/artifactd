import { useState } from "react";
import { Badge, Card, Container, Group, Stack, Text, Title } from "@mantine/core";
import { GraphView } from "./GraphView";

type SelectedNode = {
  id: string;
  label: string;
};

export function App() {
  const [selected, setSelected] = useState<SelectedNode | null>(null);

  return (
    <Container size="xl" py={{ base: 24, sm: 48 }}>
      <Stack gap="lg">
        <Group justify="space-between" align="flex-end">
          <Stack gap={4}>
            <Badge variant="light" w="fit-content">
              Artifactd · React · Cytoscape
            </Badge>
            <Title order={1}>Explore connected ideas</Title>
            <Text c="dimmed" maw={680}>
              A graph is useful when relationships are the story. Replace graph.json with your own nodes and edges.
            </Text>
          </Stack>
          <Text c="dimmed" size="sm" visibleFrom="sm">
            Drag · zoom · select
          </Text>
        </Group>
        <div className="graph-layout">
          <Card withBorder radius="lg" padding={0} className="graph-card">
            <GraphView onSelect={setSelected} />
          </Card>
          <Card withBorder radius="lg" padding="lg" className="details-card">
            <Stack gap="xs">
              <Text size="xs" fw={700} tt="uppercase" c="dimmed" lts="0.08em">
                Selected node
              </Text>
              {selected ? (
                <>
                  <Title order={3}>{selected.label}</Title>
                  <Text c="dimmed" size="sm">
                    {selected.id}
                  </Text>
                </>
              ) : (
                <Text c="dimmed" size="sm">
                  Select a node to inspect it.
                </Text>
              )}
            </Stack>
          </Card>
        </div>
      </Stack>
    </Container>
  );
}
