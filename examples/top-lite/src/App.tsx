import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge, Button, TextInput } from "@mantine/core";

type Process = {
  pid: number;
  command: string;
  user: string;
  cpu_percent: number;
  memory_percent: number;
  state: string;
};

type Snapshot = {
  updated_at: string;
  cpu: { percent: number; user_percent: number; system_percent: number };
  memory: { percent: number; used_bytes: number; total_bytes: number };
  load: { one: number; five: number; fifteen: number };
  processes: Process[];
};

type SortKey = "pid" | "command" | "user" | "cpu_percent" | "memory_percent" | "state";

function formatBytes(value: number) {
  return `${(value / 1024 ** 3).toFixed(1)} GiB`;
}

function formatPercent(value: number) {
  return `${value.toFixed(1)}%`;
}

function Sparkline({ values }: { values: number[] }) {
  const width = 640;
  const height = 180;
  const points = values.length < 2
    ? `0,${height} ${width},${height}`
    : values.map((value, index) => {
        const x = (index / (values.length - 1)) * width;
        const y = height - (Math.min(100, Math.max(0, value)) / 100) * (height - 14) - 7;
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      }).join(" ");

  return (
    <svg className="signal-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Recent CPU usage">
      <defs>
        <linearGradient id="signal-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#c5ff52" stopOpacity="0.35" />
          <stop offset="1" stopColor="#c5ff52" stopOpacity="0" />
        </linearGradient>
      </defs>
      <line x1="0" y1="45" x2={width} y2="45" />
      <line x1="0" y1="90" x2={width} y2="90" />
      <line x1="0" y1="135" x2={width} y2="135" />
      <polygon points={`0,${height} ${points} ${width},${height}`} fill="url(#signal-fill)" />
      <polyline points={points} />
    </svg>
  );
}

export function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [history, setHistory] = useState<number[]>([]);
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState<SortKey>("cpu_percent");
  const [descending, setDescending] = useState(true);
  const [error, setError] = useState(false);

  const load = useCallback(async () => {
    try {
      const response = await fetch("/_artifactd/system", { cache: "no-store" });
      if (!response.ok) throw new Error(`system provider returned ${response.status}`);
      const next = (await response.json()) as Snapshot;
      setSnapshot(next);
      setHistory((current) => [...current.slice(-39), next.cpu.percent]);
      setError(false);
    } catch {
      setError(true);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (paused) return;
    const interval = window.setInterval(load, 1000);
    return () => window.clearInterval(interval);
  }, [load, paused]);

  const processes = useMemo(() => {
    if (!snapshot) return [];
    const query = filter.trim().toLowerCase();
    const filtered = snapshot.processes.filter((process) =>
      `${process.command} ${process.user} ${process.pid}`.toLowerCase().includes(query),
    );
    return filtered.sort((a, b) => {
      const first = a[sort];
      const second = b[sort];
      const result = typeof first === "number" && typeof second === "number"
        ? first - second
        : String(first).localeCompare(String(second));
      return descending ? -result : result;
    });
  }, [snapshot, filter, sort, descending]);

  function changeSort(next: SortKey) {
    if (next === sort) setDescending((value) => !value);
    else {
      setSort(next);
      setDescending(next !== "command" && next !== "user" && next !== "state");
    }
  }

  const cpu = snapshot?.cpu.percent ?? 0;
  const memory = snapshot?.memory.percent ?? 0;

  return (
    <main className="system-shell">
      <nav className="topbar">
        <a className="brand" href="/"><span className="brand-signal"><i /><i /><i /><i /></span><span>top-lite</span></a>
        <div className={`live-status ${error ? "is-error" : paused ? "is-paused" : ""}`}>
          <span className="live-dot" />
          {error ? "Provider unavailable" : paused ? "Telemetry paused" : "Live telemetry"}
        </div>
      </nav>

      <header className="hero">
        <div>
          <Badge className="hero-badge" variant="light">Linux system monitor</Badge>
          <h1>Your system,<br />at a glance.</h1>
        </div>
        <div className="hero-actions">
          <p>A calm, live reading of the processes and resources shaping this machine right now.</p>
          <Button className="pause-button" variant="outline" onClick={() => setPaused((value) => !value)}>
            {paused ? "Resume updates" : "Pause updates"}
          </Button>
        </div>
      </header>

      <section className="metrics" aria-label="System overview">
        <article className="metric metric-cpu">
          <div className="metric-top"><span>CPU activity</span><span>{paused ? "Paused" : "Last 40 seconds"}</span></div>
          <div className="cpu-reading"><strong>{formatPercent(cpu)}</strong><span>user {formatPercent(snapshot?.cpu.user_percent ?? 0)}<br />system {formatPercent(snapshot?.cpu.system_percent ?? 0)}</span></div>
          <Sparkline values={history} />
        </article>

        <article className="metric metric-memory">
          <span className="metric-label">Memory</span>
          <strong>{formatPercent(memory)}</strong>
          <progress max="100" value={memory} aria-label={`${formatPercent(memory)} memory used`} />
          <p>{formatBytes(snapshot?.memory.used_bytes ?? 0)} used</p>
          <span className="metric-note">of {formatBytes(snapshot?.memory.total_bytes ?? 0)}</span>
        </article>

        <article className="metric metric-load">
          <span className="metric-label">Load average</span>
          <strong>{snapshot?.load.one.toFixed(2) ?? "—"}</strong>
          <div className="load-periods">
            <div><span>5 min</span><b>{snapshot?.load.five.toFixed(2) ?? "—"}</b></div>
            <div><span>15 min</span><b>{snapshot?.load.fifteen.toFixed(2) ?? "—"}</b></div>
          </div>
          <span className="metric-note">{snapshot?.processes.length ?? 0} processes sampled</span>
        </article>
      </section>

      <section className="process-section" aria-labelledby="process-heading">
        <div className="process-header">
          <div><span className="section-kicker">Live activity</span><h2 id="process-heading">Processes</h2></div>
          <TextInput
            className="process-search"
            aria-label="Filter processes"
            placeholder="Filter command, user, or PID…"
            value={filter}
            onChange={(event) => setFilter(event.currentTarget.value)}
            leftSection={<span aria-hidden="true">⌕</span>}
          />
        </div>

        {error && !snapshot ? (
          <div className="table-message"><strong>System data is unavailable.</strong><span>Artifactd could not read the local provider.</span></div>
        ) : !snapshot ? (
          <div className="table-message"><strong>Reading the system…</strong><span>The first snapshot will appear shortly.</span></div>
        ) : processes.length === 0 ? (
          <div className="table-message"><strong>No matching processes.</strong><span>Try another filter.</span></div>
        ) : (
          <div className="table-wrap">
            <table>
              <thead><tr>
                <th><button onClick={() => changeSort("pid")}>PID {sort === "pid" && (descending ? "↓" : "↑")}</button></th>
                <th><button onClick={() => changeSort("command")}>Command {sort === "command" && (descending ? "↓" : "↑")}</button></th>
                <th><button onClick={() => changeSort("user")}>User {sort === "user" && (descending ? "↓" : "↑")}</button></th>
                <th className="numeric"><button onClick={() => changeSort("cpu_percent")}>CPU {sort === "cpu_percent" && (descending ? "↓" : "↑")}</button></th>
                <th className="numeric"><button onClick={() => changeSort("memory_percent")}>Memory {sort === "memory_percent" && (descending ? "↓" : "↑")}</button></th>
                <th><button onClick={() => changeSort("state")}>State {sort === "state" && (descending ? "↓" : "↑")}</button></th>
              </tr></thead>
              <tbody>{processes.slice(0, 100).map((process) => (
                <tr key={process.pid}>
                  <td className="pid">{process.pid}</td>
                  <td className="command-cell"><span className="process-glyph">{process.command.slice(0, 1).toUpperCase()}</span>{process.command}</td>
                  <td>{process.user}</td>
                  <td className="numeric strong-value">{formatPercent(process.cpu_percent)}</td>
                  <td className="numeric">{formatPercent(process.memory_percent)}</td>
                  <td><span className={`state state-${process.state}`}>{process.state}</span></td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}

        <footer className="process-footer">
          <span>Showing {Math.min(processes.length, 100)} of {snapshot?.processes.length ?? 0} processes</span>
          <span>{paused ? "Snapshot held" : "Refreshing every second"}</span>
        </footer>
      </section>
    </main>
  );
}
