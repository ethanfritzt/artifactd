import { useEffect, useRef, useState } from "react";
import cytoscape, { type Core, type ElementDefinition } from "cytoscape";
import { Alert, Button, Center, Group, Loader, Select, Stack, Text, TextInput } from "@mantine/core";

type SelectedNode = {
  id: string;
  label: string;
};

type GraphViewProps = {
  onSelect: (node: SelectedNode | null) => void;
};

const layouts = [
  { value: "cose", label: "Force directed" },
  { value: "breadthfirst", label: "Hierarchy" },
  { value: "circle", label: "Circle" },
  { value: "grid", label: "Grid" },
];

export function GraphView({ onSelect }: GraphViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const graphRef = useRef<Core | null>(null);
  const [elements, setElements] = useState<ElementDefinition[] | null>(null);
  const [layout, setLayout] = useState("cose");
  const [query, setQuery] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch("/graph.json", { cache: "no-store" })
      .then((response) => {
        if (!response.ok) throw new Error(`graph data returned ${response.status}`);
        return response.json() as Promise<{ nodes?: ElementDefinition[]; edges?: ElementDefinition[] }>;
      })
      .then((graph) => setElements([...(graph.nodes ?? []), ...(graph.edges ?? [])]))
      .catch(() => setError("Graph data could not be loaded."));
  }, []);

  useEffect(() => {
    if (!containerRef.current || !elements?.length) return;
    const graph = cytoscape({
      container: containerRef.current,
      elements,
      layout: { name: layout },
      style: [
        {
          selector: "node",
          style: {
            "background-color": "#66d9e8",
            label: "data(label)",
            color: "#f8fafc",
            "font-size": 11,
            "text-outline-color": "#101827",
            "text-outline-width": 3,
            "text-valign": "bottom",
            "text-margin-y": 8,
            width: 22,
            height: 22,
          },
        },
        {
          selector: "edge",
          style: {
            width: 1.5,
            "line-color": "#64748b",
            "target-arrow-color": "#64748b",
            "target-arrow-shape": "triangle",
            "curve-style": "bezier",
          },
        },
        {
          selector: ".faded",
          style: { opacity: 0.18 },
        },
        {
          selector: ".search-match",
          style: { "background-color": "#d0bfff", "border-width": 3, "border-color": "#ffffff" },
        },
        {
          selector: ":selected",
          style: {
            "background-color": "#d0bfff",
            "line-color": "#d0bfff",
            "target-arrow-color": "#d0bfff",
            "border-width": 3,
            "border-color": "#ffffff",
          },
        },
      ],
    });
    graph.on("tap", "node", (event) => {
      const node = event.target;
      graph.elements().addClass("faded");
      node.closedNeighborhood().removeClass("faded");
      onSelect({ id: node.id(), label: String(node.data("label") ?? node.id()) });
    });
    graph.on("tap", (event) => {
      if (event.target === graph) {
        graph.elements().removeClass("faded");
        onSelect(null);
      }
    });
    graphRef.current = graph;
    return () => {
      graph.destroy();
      graphRef.current = null;
    };
  }, [elements, layout, onSelect]);

  useEffect(() => {
    const graph = graphRef.current;
    if (!graph) return;
    const normalized = query.trim().toLowerCase();
    graph.nodes().forEach((node) => {
      const label = String(node.data("label") ?? node.id()).toLowerCase();
      node.toggleClass("search-match", normalized !== "" && label.includes(normalized));
    });
  }, [query]);

  if (error) {
    return <Alert color="red" m="lg">{error}</Alert>;
  }
  if (!elements) {
    return <Center h="100%" mih={420}><Loader color="cyan" /></Center>;
  }
  if (!elements.length) {
    return <Center h="100%" mih={420}><Stack align="center" gap={4}><Text fw={600}>No relationships yet</Text><Text c="dimmed" size="sm">Add nodes and edges to graph.json.</Text></Stack></Center>;
  }
  return (
    <Stack gap={0} h="100%">
      <Group p="sm" gap="xs" className="graph-toolbar">
        <TextInput
          aria-label="Search graph"
          placeholder="Search nodes"
          value={query}
          onChange={(event) => setQuery(event.currentTarget.value)}
          flex={1}
        />
        <Select aria-label="Graph layout" data={layouts} value={layout} onChange={(value) => value && setLayout(value)} w={170} />
        <Button variant="subtle" onClick={() => graphRef.current?.fit(undefined, 36)}>Reset</Button>
      </Group>
      <div ref={containerRef} className="graph-stage" aria-label="Interactive relationship graph" />
    </Stack>
  );
}
