(() => {
  const state = {
    snapshot: null,
    history: [],
    paused: false,
    filter: "",
    sort: "cpu_percent",
    descending: true,
    error: false,
  };
  const root = document.querySelector("#root");
  if (!root) return;

  root.innerHTML = `
    <main class="system-shell">
      <nav class="topbar">
        <a class="brand" href="/" aria-label="Top Lite home">
          <span class="brand-signal" aria-hidden="true"><i></i><i></i><i></i><i></i></span>
          <span>top-lite</span>
        </a>
        <div class="live-status"><span class="live-dot" aria-hidden="true"></span><span class="live-label">Live telemetry</span></div>
      </nav>
      <header class="hero">
        <div>
          <span class="hero-badge">Linux system monitor</span>
          <h1>Your system,<br>at a glance.</h1>
        </div>
        <div class="hero-actions">
          <p>A calm, live reading of the processes and resources shaping this machine right now.</p>
          <button class="pause-button" type="button">Pause updates</button>
        </div>
      </header>
      <section class="metrics" aria-label="System overview">
        <article class="metric metric-cpu">
          <div class="metric-top"><span>CPU activity</span><span class="cpu-period">Last 40 seconds</span></div>
          <div class="cpu-reading"><strong class="cpu-value">0.0%</strong><span class="cpu-breakdown">user 0.0%<br>system 0.0%</span></div>
          <svg class="signal-chart" viewBox="0 0 640 180" role="img" aria-label="Recent CPU usage"></svg>
        </article>
        <article class="metric metric-memory">
          <span class="metric-label">Memory</span>
          <strong class="memory-value">0.0%</strong>
          <progress class="memory-progress" max="100" value="0" aria-label="0.0% memory used"></progress>
          <p class="memory-used">0.0 GiB used</p>
          <span class="metric-note memory-total">of 0.0 GiB</span>
        </article>
        <article class="metric metric-load">
          <span class="metric-label">Load average</span>
          <strong class="load-one">—</strong>
          <div class="load-periods">
            <div><span>5 min</span><b class="load-five">—</b></div>
            <div><span>15 min</span><b class="load-fifteen">—</b></div>
          </div>
          <span class="metric-note process-count">0 processes sampled</span>
        </article>
      </section>
      <section class="process-section" aria-labelledby="process-heading">
        <div class="process-header">
          <div><span class="section-kicker">Live activity</span><h2 id="process-heading">Processes</h2></div>
          <label class="process-search">
            <span class="sr-only">Filter processes</span>
            <span aria-hidden="true">⌕</span>
            <input type="search" placeholder="Filter command, user, or PID…" autocomplete="off">
          </label>
        </div>
        <div class="process-content"></div>
        <footer class="process-footer"><span class="process-summary">Showing 0 of 0 processes</span><span class="process-refresh">Refreshing every second</span></footer>
      </section>
    </main>`;

  const query = (selector) => root.querySelector(selector);
  const pauseButton = query(".pause-button");
  const search = query(".process-search input");
  const content = query(".process-content");
  const liveStatus = query(".live-status");
  const liveLabel = query(".live-label");

  const number = (value, fallback = 0) => Number.isFinite(Number(value)) ? Number(value) : fallback;
  const formatPercent = (value) => `${number(value).toFixed(1)}%`;
  const formatBytes = (value) => `${(number(value) / 1024 ** 3).toFixed(1)} GiB`;
  const text = (tag, className, value) => {
    const node = document.createElement(tag);
    if (className) node.className = className;
    node.textContent = value;
    return node;
  };

  function validSnapshot(value) {
    return value && typeof value === "object"
      && Array.isArray(value.processes)
      && value.processes.every((process) => process && typeof process === "object")
      && value.cpu && typeof value.cpu === "object"
      && value.memory && typeof value.memory === "object"
      && value.load && typeof value.load === "object";
  }

  function renderChart(values) {
    const svg = query(".signal-chart");
    svg.replaceChildren();
    const width = 640;
    const height = 180;
    const points = values.length < 2
      ? `0,${height} ${width},${height}`
      : values.map((value, index) => {
          const x = (index / (values.length - 1)) * width;
          const y = height - (Math.min(100, Math.max(0, number(value))) / 100) * (height - 14) - 7;
          return `${x.toFixed(1)},${y.toFixed(1)}`;
        }).join(" ");
    const ns = "http://www.w3.org/2000/svg";
    const defs = document.createElementNS(ns, "defs");
    const gradient = document.createElementNS(ns, "linearGradient");
    gradient.id = "signal-fill";
    gradient.setAttribute("x1", "0"); gradient.setAttribute("y1", "0");
    gradient.setAttribute("x2", "0"); gradient.setAttribute("y2", "1");
    const top = document.createElementNS(ns, "stop");
    top.setAttribute("offset", "0"); top.setAttribute("stop-color", "#c5ff52"); top.setAttribute("stop-opacity", ".35");
    const bottom = document.createElementNS(ns, "stop");
    bottom.setAttribute("offset", "1"); bottom.setAttribute("stop-color", "#c5ff52"); bottom.setAttribute("stop-opacity", "0");
    gradient.append(top, bottom); defs.append(gradient); svg.append(defs);
    [45, 90, 135].forEach((y) => {
      const line = document.createElementNS(ns, "line");
      line.setAttribute("x1", "0"); line.setAttribute("y1", String(y)); line.setAttribute("x2", String(width)); line.setAttribute("y2", String(y));
      svg.append(line);
    });
    const fill = document.createElementNS(ns, "polygon");
    fill.setAttribute("points", `0,${height} ${points} ${width},${height}`);
    fill.setAttribute("fill", "url(#signal-fill)");
    const line = document.createElementNS(ns, "polyline");
    line.setAttribute("points", points);
    svg.append(fill, line);
  }

  function sortedProcesses() {
    if (!state.snapshot) return [];
    const normalized = state.filter.trim().toLowerCase();
    return state.snapshot.processes
      .filter((process) => `${process.command} ${process.user} ${process.pid}`.toLowerCase().includes(normalized))
      .sort((first, second) => {
        const a = first[state.sort];
        const b = second[state.sort];
        const result = typeof a === "number" && typeof b === "number" ? a - b : String(a).localeCompare(String(b));
        return state.descending ? -result : result;
      });
  }

  function changeSort(next) {
    if (next === state.sort) state.descending = !state.descending;
    else {
      state.sort = next;
      state.descending = !["command", "user", "state"].includes(next);
    }
    render();
  }

  function renderTable(processes) {
    const table = document.createElement("table");
    const headers = [["pid", "PID"], ["command", "Command"], ["user", "User"], ["cpu_percent", "CPU"], ["memory_percent", "Memory"], ["state", "State"]];
    const head = document.createElement("thead");
    const row = document.createElement("tr");
    headers.forEach(([key, label]) => {
      const cell = document.createElement("th");
      if (["cpu_percent", "memory_percent"].includes(key)) cell.className = "numeric";
      const button = text("button", "", label + (state.sort === key ? (state.descending ? " ↓" : " ↑") : ""));
      button.type = "button";
      button.addEventListener("click", () => changeSort(key));
      cell.append(button); row.append(cell);
    });
    head.append(row); table.append(head);
    const body = document.createElement("tbody");
    processes.slice(0, 100).forEach((process) => {
      const item = document.createElement("tr");
      const cells = [
        text("td", "pid", String(process.pid)),
        text("td", "command-cell", String(process.command)),
        text("td", "", String(process.user)),
        text("td", "numeric strong-value", formatPercent(process.cpu_percent)),
        text("td", "numeric", formatPercent(process.memory_percent)),
      ];
      cells[1].prepend(text("span", "process-glyph", String(process.command).slice(0, 1).toUpperCase()));
      const stateName = String(process.state || "unknown");
      const stateCell = text("td");
      stateCell.append(text("span", `state state-${["running", "zombie"].includes(stateName) ? stateName : "unknown"}`, stateName));
      cells.push(stateCell); cells.forEach((cell) => item.append(cell)); body.append(item);
    });
    table.append(body);
    const wrapper = document.createElement("div");
    wrapper.className = "table-wrap";
    wrapper.append(table);
    return wrapper;
  }

  function render() {
    const snapshot = state.snapshot;
    liveStatus.classList.toggle("is-error", state.error);
    liveStatus.classList.toggle("is-paused", state.paused && !state.error);
    liveLabel.textContent = state.error ? "Provider unavailable" : state.paused ? "Telemetry paused" : "Live telemetry";
    pauseButton.textContent = state.paused ? "Resume updates" : "Pause updates";
    query(".cpu-period").textContent = state.paused ? "Paused" : "Last 40 seconds";
    if (snapshot) {
      query(".cpu-value").textContent = formatPercent(snapshot.cpu.percent);
      query(".cpu-breakdown").innerHTML = `user ${formatPercent(snapshot.cpu.user_percent)}<br>system ${formatPercent(snapshot.cpu.system_percent)}`;
      query(".memory-value").textContent = formatPercent(snapshot.memory.percent);
      query(".memory-progress").value = number(snapshot.memory.percent);
      query(".memory-progress").setAttribute("aria-label", `${formatPercent(snapshot.memory.percent)} memory used`);
      query(".memory-used").textContent = `${formatBytes(snapshot.memory.used_bytes)} used`;
      query(".memory-total").textContent = `of ${formatBytes(snapshot.memory.total_bytes)}`;
      query(".load-one").textContent = number(snapshot.load.one).toFixed(2);
      query(".load-five").textContent = number(snapshot.load.five).toFixed(2);
      query(".load-fifteen").textContent = number(snapshot.load.fifteen).toFixed(2);
      query(".process-count").textContent = `${snapshot.processes.length} processes sampled`;
      renderChart(state.history);
    }
    const processes = sortedProcesses();
    query(".process-summary").textContent = `Showing ${Math.min(processes.length, 100)} of ${snapshot ? snapshot.processes.length : 0} processes`;
    query(".process-refresh").textContent = state.paused ? "Snapshot held" : "Refreshing every second";
    if (state.error && !snapshot) {
      const message = document.createElement("div");
      message.className = "table-message";
      message.append(text("strong", "", "System data is unavailable."), text("span", "", "Artifactd could not read the local provider."));
      content.replaceChildren(message);
    } else if (!snapshot) {
      const message = document.createElement("div");
      message.className = "table-message";
      message.append(text("strong", "", "Reading the system…"), text("span", "", "The first snapshot will appear shortly."));
      content.replaceChildren(message);
    } else if (!processes.length) {
      const message = document.createElement("div");
      message.className = "table-message";
      message.append(text("strong", "", "No matching processes."), text("span", "", "Try another filter."));
      content.replaceChildren(message);
    } else content.replaceChildren(renderTable(processes));
  }

  async function load() {
    try {
      const response = await fetch("/_artifactd/system", { cache: "no-store" });
      if (!response.ok) throw new Error(`system provider returned ${response.status}`);
      const next = await response.json();
      if (!validSnapshot(next)) throw new Error("invalid system response");
      state.snapshot = next;
      state.history = [...state.history.slice(-39), number(next.cpu.percent)];
      state.error = false;
    } catch {
      state.error = true;
    }
    render();
  }

  pauseButton.addEventListener("click", () => {
    state.paused = !state.paused;
    render();
  });
  search.addEventListener("input", () => {
    state.filter = search.value;
    render();
  });
  render();
  void load();
  window.setInterval(() => { if (!state.paused) void load(); }, 1000);
})();
