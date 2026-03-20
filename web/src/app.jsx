import { h, Fragment, render } from "preact";
import { useState, useEffect, useCallback } from "preact/hooks";

// API base URL — defaults to same origin, configurable via data attribute.
const API_BASE =
  document.getElementById("app")?.dataset?.api || window.location.origin;

function useFetch(url, interval = 10000) {
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  const fetchData = useCallback(() => {
    fetch(`${API_BASE}${url}`)
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((d) => {
        setData(d);
        setError(null);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [url]);

  useEffect(() => {
    fetchData();
    const id = setInterval(fetchData, interval);
    return () => clearInterval(id);
  }, [fetchData, interval]);

  return { data, error, loading };
}

function StatCard({ label, value }) {
  return (
    <div class="stat-card">
      <div class="label">{label}</div>
      <div class="value">{value ?? "—"}</div>
    </div>
  );
}

function Dashboard() {
  const { data, error, loading } = useFetch("/api/v1/stats", 5000);
  const health = useFetch("/health", 5000);

  if (loading) return <div class="loading">Loading...</div>;
  if (error) return <div class="error">Failed to load stats: {error}</div>;

  const healthData = health.data || {};
  const fed = data.federation;

  return (
    <>
      <div class="stats-grid">
        <StatCard label="Local Agents" value={data.registry?.local_count} />
        <StatCard
          label="Federated Agents"
          value={data.registry?.federated_count}
        />
        <StatCard label="Skills" value={data.registry?.skill_count} />
        <StatCard
          label="Federation Peers"
          value={fed ? `${fed.healthy_peers}/${fed.peers}` : "N/A"}
        />
        <StatCard
          label="Connected Peers"
          value={healthData.libp2p?.connected_peers}
        />
        <StatCard
          label="Uptime"
          value={
            healthData.uptime_seconds
              ? `${Math.floor(healthData.uptime_seconds / 3600)}h ${Math.floor((healthData.uptime_seconds % 3600) / 60)}m`
              : "—"
          }
        />
      </div>
    </>
  );
}

function AgentCard({ agent }) {
  return (
    <div class="agent-card">
      <div class="agent-header">
        <span class="agent-name">{agent.name || "Unnamed Agent"}</span>
        {agent.origin && <span class="agent-origin">via {agent.origin}</span>}
      </div>
      <div class="peer-id">{agent.peer_id}</div>
      <div class="skills-list">
        {agent.skills?.map((s) => (
          <span class="skill-badge" key={s.id}>
            {s.id}
          </span>
        ))}
      </div>
    </div>
  );
}

function Agents() {
  const [search, setSearch] = useState("");
  const { data, error, loading } = useFetch("/api/v1/agents");

  if (loading) return <div class="loading">Loading agents...</div>;
  if (error) return <div class="error">Failed to load agents: {error}</div>;

  const agents = (data.agents || []).filter((a) => {
    if (!search) return true;
    const q = search.toLowerCase();
    return (
      (a.name || "").toLowerCase().includes(q) ||
      a.peer_id.toLowerCase().includes(q) ||
      a.skills?.some((s) => s.id.toLowerCase().includes(q))
    );
  });

  return (
    <>
      <input
        class="search-box"
        type="text"
        placeholder="Search agents by name, peer ID, or skill..."
        value={search}
        onInput={(e) => setSearch(e.target.value)}
      />
      {agents.length === 0 ? (
        <div class="empty">No agents found</div>
      ) : (
        <div class="agent-list">
          {agents.map((a) => (
            <AgentCard key={a.peer_id} agent={a} />
          ))}
        </div>
      )}
    </>
  );
}

function Skills() {
  const { data, error, loading } = useFetch("/api/v1/skills");

  if (loading) return <div class="loading">Loading skills...</div>;
  if (error) return <div class="error">Failed to load skills: {error}</div>;

  const skills = data.skills || [];

  if (skills.length === 0) return <div class="empty">No skills registered</div>;

  return (
    <div class="agent-list">
      {skills.map((s) => (
        <div class="skill-card" key={s.id}>
          <span class="skill-name">{s.id}</span>
          <span class="agent-count">{s.agent_count}</span>
        </div>
      ))}
    </div>
  );
}

function Federation() {
  const { data, error, loading } = useFetch("/health", 5000);

  if (loading) return <div class="loading">Loading...</div>;
  if (error) return <div class="error">Failed to load health: {error}</div>;

  const fed = data.federation;
  if (!fed) return <div class="empty">Federation not configured</div>;

  return (
    <div class="agent-list">
      {(fed.peer_details || []).map((p) => (
        <div class="federation-peer" key={p.address}>
          <div>
            <span class={`status-dot ${p.healthy ? "healthy" : "unhealthy"}`} />
            <span class="addr">{p.address}</span>
          </div>
          <div style="color: var(--text-muted); font-size: 0.75rem">
            {p.consecutive_failures > 0
              ? `${p.consecutive_failures} failures`
              : "Healthy"}
          </div>
        </div>
      ))}
    </div>
  );
}

const PAGES = {
  dashboard: Dashboard,
  agents: Agents,
  skills: Skills,
  federation: Federation,
};

function App() {
  const [page, setPage] = useState("dashboard");
  const Page = PAGES[page];

  return (
    <>
      <header>
        <h1>
          Agent<span>Anycast</span>
        </h1>
        <nav>
          {Object.keys(PAGES).map((p) => (
            <button
              key={p}
              class={page === p ? "active" : ""}
              onClick={() => setPage(p)}
            >
              {p.charAt(0).toUpperCase() + p.slice(1)}
            </button>
          ))}
        </nav>
      </header>
      <Page />
    </>
  );
}

render(<App />, document.getElementById("app"));
