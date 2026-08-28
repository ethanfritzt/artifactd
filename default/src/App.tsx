import { useEffect, useMemo, useState } from "react";
import { Badge, TextInput } from "@mantine/core";

type Artifact = {
  id: string;
  name: string;
  description?: string;
  current_version: number;
  workspace_id?: string;
  updated_at: string;
};

function previewKind(id: string) {
  if (id.includes("vault") || id.includes("graph")) return "graph";
  if (id.includes("top") || id.includes("monitor")) return "monitor";
  if (id.includes("github") || id.includes("issue")) return "github";
  return "default";
}

function ArtifactPreview({ artifact }: { artifact: Artifact }) {
  const kind = previewKind(artifact.id);

  if (kind === "graph") {
    return (
      <div className="preview preview-graph" aria-hidden="true">
        <span className="graph-line line-a" />
        <span className="graph-line line-b" />
        <span className="graph-line line-c" />
        <span className="graph-node node-a" />
        <span className="graph-node node-b" />
        <span className="graph-node node-c" />
        <span className="graph-node node-d" />
        <span className="preview-label">Knowledge map</span>
      </div>
    );
  }

  if (kind === "monitor") {
    return (
      <div className="preview preview-monitor" aria-hidden="true">
        <div className="monitor-topline"><span>CPU</span><strong>42%</strong></div>
        <div className="monitor-chart">
          <span /><span /><span /><span /><span /><span /><span /><span /><span /><span />
        </div>
        <div className="monitor-rows"><span /><span /><span /></div>
      </div>
    );
  }

  if (kind === "github") {
    return (
      <div className="preview preview-github" aria-hidden="true">
        <div className="github-mark">⌁</div>
        <div className="github-copy">
          <span className="github-kicker">OPEN PULL REQUESTS</span>
          <strong>12</strong>
          <div><i /><i /><i /></div>
        </div>
      </div>
    );
  }

  return (
    <div className="preview preview-default" aria-hidden="true">
      <span className="orb orb-a" /><span className="orb orb-b" />
      <strong>{artifact.name.slice(0, 1).toUpperCase()}</strong>
    </div>
  );
}

function relativeDate(value: string) {
  const timestamp = new Date(value).getTime();
  if (Number.isNaN(timestamp)) return "Recently updated";
  const days = Math.max(0, Math.floor((Date.now() - timestamp) / 86_400_000));
  if (days === 0) return "Updated today";
  if (days === 1) return "Updated yesterday";
  if (days < 14) return `Updated ${days} days ago`;
  return `Updated ${new Date(value).toLocaleDateString(undefined, { month: "short", day: "numeric" })}`;
}

export function App() {
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [filter, setFilter] = useState("");
  const [error, setError] = useState(false);

  useEffect(() => {
    let active = true;
    const load = async () => {
      try {
        const response = await fetch("/_artifactd/library", { cache: "no-store" });
        if (!response.ok) throw new Error(`library returned ${response.status}`);
        const result = (await response.json()) as { artifacts?: Artifact[] };
        if (active) {
          setArtifacts((result.artifacts ?? []).filter((artifact) => artifact.id !== "artifactd-home"));
          setError(false);
        }
      } catch {
        if (active) setError(true);
      }
    };
    void load();
    const interval = window.setInterval(load, 5000);
    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, []);

  const visible = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const sorted = [...artifacts].sort((a, b) => b.updated_at.localeCompare(a.updated_at));
    if (!query) return sorted;
    return sorted.filter((artifact) =>
      `${artifact.id} ${artifact.name} ${artifact.description ?? ""}`.toLowerCase().includes(query),
    );
  }, [artifacts, filter]);

  const versionCount = artifacts.reduce((total, artifact) => total + artifact.current_version, 0);
  const baseHost = window.location.host.replace(/^artifactd-home\./, "");

  return (
    <main className="library-shell">
      <nav className="topbar" aria-label="Artifactd home">
        <a className="brand" href="/">
          <span className="brand-mark"><i /><i /><i /></span>
          <span>artifactd</span>
        </a>
        <div className="local-status"><span /> Local runtime</div>
      </nav>

      <header className="hero">
        <div className="hero-copy">
          <Badge className="hero-badge" variant="light">Your local collection</Badge>
          <h1>Things you make<br />shouldn’t disappear.</h1>
          <p>Dashboards, explorations, and useful little tools—kept close and ready to open.</p>
        </div>
        <div className="hero-stats" aria-label="Library summary">
          <div><strong>{artifacts.length}</strong><span>Artifacts</span></div>
          <div><strong>{versionCount}</strong><span>Versions kept</span></div>
          <div><strong>Local</strong><span>Private by default</span></div>
        </div>
      </header>

      <section className="collection" aria-labelledby="collection-heading">
        <div className="collection-bar">
          <div>
            <span className="section-kicker">Library</span>
            <h2 id="collection-heading">Your artifacts</h2>
          </div>
          <TextInput
            className="library-search"
            aria-label="Search artifacts"
            placeholder="Search your library…"
            value={filter}
            onChange={(event) => setFilter(event.currentTarget.value)}
            leftSection={<span aria-hidden="true">⌕</span>}
          />
        </div>

        {error ? (
          <div className="message-card error-card">The artifact library is unavailable.</div>
        ) : visible.length === 0 ? (
          <div className="message-card empty-card">
            <span>✦</span>
            <h3>{filter ? "No matching artifacts" : "Your library is empty"}</h3>
            <p>{filter ? "Try a different search." : "Publish an artifact and it will appear here."}</p>
          </div>
        ) : (
          <div className="artifact-grid">
            {visible.map((artifact) => (
              <a
                className="artifact-card"
                key={artifact.id}
                href={`${window.location.protocol}//${artifact.id}.${baseHost}/`}
              >
                <ArtifactPreview artifact={artifact} />
                <div className="card-body">
                  <div className="card-heading">
                    <div>
                      <span className="card-meta">{artifact.workspace_id || "Static artifact"}</span>
                      <h3>{artifact.name}</h3>
                    </div>
                    <span className="open-arrow" aria-hidden="true">↗</span>
                  </div>
                  <p>{artifact.description || "No description"}</p>
                  <div className="card-footer">
                    <span>{relativeDate(artifact.updated_at)}</span>
                    <span>Version {artifact.current_version}</span>
                  </div>
                </div>
              </a>
            ))}
          </div>
        )}
      </section>

      <footer className="library-footer">
        <p>Ready to make something?</p>
        <code>artifact create ./my-tool --template react</code>
      </footer>
    </main>
  );
}
