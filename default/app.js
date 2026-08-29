(() => {
  const state = { artifacts: [], filter: "", error: false };
  const root = document.querySelector("#root");
  if (!root) return;

  root.innerHTML = `
    <main class="library-shell">
      <nav class="topbar" aria-label="Artifactd home">
        <a class="brand" href="/">
          <span class="brand-mark" aria-hidden="true"><i></i><i></i><i></i></span>
          <span>artifactd</span>
        </a>
        <div class="local-status"><span aria-hidden="true"></span> Local runtime</div>
      </nav>
      <header class="hero">
        <div class="hero-copy">
          <span class="hero-badge">Your local collection</span>
          <h1>Things you make<br>shouldn’t disappear.</h1>
          <p>Dashboards, explorations, and useful little tools—kept close and ready to open.</p>
        </div>
        <div class="hero-stats" aria-label="Library summary">
          <div><strong data-stat="artifacts">0</strong><span>Artifacts</span></div>
          <div><strong data-stat="versions">0</strong><span>Versions kept</span></div>
          <div><strong>Local</strong><span>Private by default</span></div>
        </div>
      </header>
      <section class="collection" aria-labelledby="collection-heading">
        <div class="collection-bar">
          <div>
            <span class="section-kicker">Library</span>
            <h2 id="collection-heading">Your artifacts</h2>
          </div>
          <label class="library-search">
            <span class="sr-only">Search artifacts</span>
            <span aria-hidden="true">⌕</span>
            <input type="search" placeholder="Search your library…" autocomplete="off">
          </label>
        </div>
        <div class="collection-content"></div>
      </section>
      <footer class="library-footer">
        <p>Ready to make something?</p>
        <code>artifact create --id my-tool</code>
      </footer>
    </main>`;

  const content = root.querySelector(".collection-content");
  const search = root.querySelector(".library-search input");
  const artifactCount = root.querySelector('[data-stat="artifacts"]');
  const versionCount = root.querySelector('[data-stat="versions"]');
  const baseHost = window.location.host.replace(/^artifactd-home\./, "");

  function relativeDate(value) {
    const timestamp = new Date(value).getTime();
    if (Number.isNaN(timestamp)) return "Recently updated";
    const days = Math.max(0, Math.floor((Date.now() - timestamp) / 86400000));
    if (days === 0) return "Updated today";
    if (days === 1) return "Updated yesterday";
    if (days < 14) return `Updated ${days} days ago`;
    return `Updated ${new Date(value).toLocaleDateString(undefined, { month: "short", day: "numeric" })}`;
  }

  function element(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function preview(artifact) {
    const id = artifact.id.toLowerCase();
    if (id.includes("vault") || id.includes("graph")) {
      const node = element("div", "preview preview-graph");
      ["line-a", "line-b", "line-c"].forEach((name) => node.append(element("span", `graph-line ${name}`)));
      ["node-a", "node-b", "node-c", "node-d"].forEach((name) => node.append(element("span", `graph-node ${name}`)));
      node.append(element("span", "preview-label", "Knowledge map"));
      return node;
    }
    if (id.includes("top") || id.includes("monitor")) {
      const node = element("div", "preview preview-monitor");
      const topline = element("div", "monitor-topline");
      topline.append(element("span", "", "CPU"), element("strong", "", "42%"));
      const chart = element("div", "monitor-chart");
      for (let i = 0; i < 10; i += 1) chart.append(element("span"));
      const rows = element("div", "monitor-rows");
      for (let i = 0; i < 3; i += 1) rows.append(element("span"));
      node.append(topline, chart, rows);
      return node;
    }
    if (id.includes("github") || id.includes("issue")) {
      const node = element("div", "preview preview-github");
      node.append(element("div", "github-mark", "⌁"));
      const copy = element("div", "github-copy");
      copy.append(element("span", "github-kicker", "OPEN PULL REQUESTS"), element("strong", "", "12"));
      const bars = element("div");
      bars.append(element("i"), element("i"), element("i"));
      copy.append(bars);
      node.append(copy);
      return node;
    }
    const node = element("div", "preview preview-default");
    node.append(element("span", "orb orb-a"), element("span", "orb orb-b"), element("strong", "", (artifact.name || "A").slice(0, 1).toUpperCase()));
    return node;
  }

  function renderMessage(title, description, error) {
    const message = element("div", `message-card${error ? " error-card" : " empty-card"}`);
    if (!error) message.append(element("span", "", "✦"));
    message.append(element("h3", "", title), element("p", "", description));
    content.replaceChildren(message);
  }

  function render() {
    artifactCount.textContent = String(state.artifacts.length);
    versionCount.textContent = String(state.artifacts.reduce((total, artifact) => total + (Number(artifact.current_version) || 0), 0));
    if (state.error) {
      renderMessage("The artifact library is unavailable.", "Try again in a moment.", true);
      return;
    }
    const query = state.filter.trim().toLowerCase();
    const visible = [...state.artifacts]
      .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
      .filter((artifact) => !query || `${artifact.id} ${artifact.name} ${artifact.description}`.toLowerCase().includes(query));
    if (!visible.length) {
      renderMessage(query ? "No matching artifacts" : "Your library is empty", query ? "Try a different search." : "Publish an artifact and it will appear here.", false);
      return;
    }
    const grid = element("div", "artifact-grid");
    visible.forEach((artifact) => {
      const link = element("a", "artifact-card");
      link.href = `${window.location.protocol}//${artifact.id}.${baseHost}/`;
      link.append(preview(artifact));
      const body = element("div", "card-body");
      const heading = element("div", "card-heading");
      const title = element("div");
      title.append(element("span", "card-meta", artifact.workspace_id || "Static artifact"), element("h3", "", artifact.name));
      heading.append(title, element("span", "open-arrow", "↗"));
      body.append(heading, element("p", "", artifact.description || "No description"));
      const footer = element("div", "card-footer");
      footer.append(element("span", "", relativeDate(artifact.updated_at)), element("span", "", `Version ${artifact.current_version}`));
      body.append(footer);
      link.append(body);
      grid.append(link);
    });
    content.replaceChildren(grid);
  }

  async function load() {
    try {
      const response = await fetch("/_artifactd/library", { cache: "no-store" });
      if (!response.ok) throw new Error(`library returned ${response.status}`);
      const result = await response.json();
      if (!result || !Array.isArray(result.artifacts)) throw new Error("invalid library response");
      state.artifacts = result.artifacts.filter((artifact) => artifact && typeof artifact.id === "string" && artifact.id !== "artifactd-home");
      state.error = false;
    } catch {
      state.error = true;
    }
    render();
  }

  search.addEventListener("input", () => {
    state.filter = search.value;
    render();
  });
  void load();
  window.setInterval(load, 5000);
})();
