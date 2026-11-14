// Tasklane's app. Tokens are kept in localStorage; queries are sent by hash.
const OPERATIONS = __OPERATIONS__;
const main = document.getElementById("main");
const store = {
  get access() { return localStorage.getItem("tasklane.accessToken"); },
  get refresh() { return localStorage.getItem("tasklane.refreshToken"); },
  get expiresAt() { return Number(localStorage.getItem("tasklane.expiresAt") || 0); },
  save(t) {
    localStorage.setItem("tasklane.accessToken", t.accessToken);
    localStorage.setItem("tasklane.refreshToken", t.refreshToken);
    localStorage.setItem("tasklane.expiresAt", String(Date.now() + t.expiresIn * 1000));
  },
  clear() { for (const k of ["accessToken", "refreshToken", "expiresAt"]) localStorage.removeItem("tasklane." + k); },
};
const el = (tag, text, cls) => Object.assign(document.createElement(tag), { textContent: text ?? "", className: cls || "" });

// ---- Signing in with Northwind ID (PKCE)

function random(n) {
  const b = crypto.getRandomValues(new Uint8Array(n));
  return btoa(String.fromCharCode(...b)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
async function challenge(verifier) {
  const sum = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return btoa(String.fromCharCode(...new Uint8Array(sum))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
async function signIn() {
  const config = await fetch("/api/auth/config").then((r) => r.json());
  const verifier = random(48), state = random(16);
  sessionStorage.setItem("tasklane.pkce", JSON.stringify({ verifier, state, returnTo: location.pathname + location.search }));
  location.href = config.authorizeUrl + "&state=" + state + "&code_challenge=" + (await challenge(verifier)) + "&code_challenge_method=S256";
}
async function callback() {
  const q = new URLSearchParams(location.search), pkce = JSON.parse(sessionStorage.getItem("tasklane.pkce") || "{}");
  sessionStorage.removeItem("tasklane.pkce");
  if (q.get("error")) return signedOut(q.get("error_description") || q.get("error"));
  if (!pkce.state || q.get("state") !== pkce.state) return signedOut("The sign-in did not match. Try again.");
  const r = await fetch("/api/auth/exchange", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ code: q.get("code"), codeVerifier: pkce.verifier }) });
  const body = await r.json();
  if (!r.ok) return signedOut(body.message);
  store.save(body);
  history.replaceState(null, "", pkce.returnTo === "/callback" ? "/" : pkce.returnTo || "/");
  start();
}
function signedOut(message) {
  store.clear();
  const box = el("div", "", "card narrow");
  box.append(el("h2", "Sign in to Tasklane"), el("p", message || "Use your Northwind account.", message ? "error" : "muted"));
  const b = el("button", "Sign in with Northwind ID", "primary");
  b.onclick = signIn;
  box.append(b);
  main.replaceChildren(box);
}

// ---- Tokens: refreshed once at a time, since each refresh token works once.

let refreshing = null;
function refresh() {
  refreshing ??= (async () => {
    const r = await fetch("/api/auth/refresh", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ refreshToken: store.refresh }) });
    const body = await r.json();
    if (!r.ok) { signedOut(body.message); throw new Error(body.message); }
    store.save(body);
  })().finally(() => { refreshing = null; });
  return refreshing;
}

async function gql(name, variables = {}) {
  if (store.expiresAt - Date.now() < 30_000) await refresh();
  for (let attempt = 0; ; attempt++) {
    const r = await fetch("/graphql", {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: "Bearer " + store.access },
      body: JSON.stringify({ operationName: name, variables, extensions: { persistedQuery: { version: 1, sha256Hash: OPERATIONS[name] } } }),
    });
    const body = await r.json();
    if (r.status === 401 && attempt === 0) { await refresh(); continue; }
    if (body.errors) throw new Error(body.errors.map((e) => e.message).join("; "));
    return body.data;
  }
}

// ---- Pages

let filters = { project: "", state: "", assignee: "" };
async function issues() {
  const { projects } = await gql("Projects");
  const bar = el("div", "", "filters");
  const select = (name, options) => {
    const s = el("select");
    s.setAttribute("aria-label", name);
    for (const [v, t] of options) s.append(new Option(t, v, false, filters[name] === v));
    s.onchange = () => { filters[name] = s.value; issues(); };
    return s;
  };
  bar.append(
    select("project", [["", "All projects"], ...projects.map((p) => [p.key, p.name])]),
    select("state", [["", "Any state"], ["TODO", "To do"], ["IN_PROGRESS", "In progress"], ["IN_REVIEW", "In review"], ["DONE", "Done"]]),
    select("assignee", [["", "Anyone"], [me.username, "Assigned to me"]]),
  );
  const table = el("table");
  table.insertRow().append(el("th", "Key"), el("th", "Title"), el("th", "State"), el("th", "Priority"), el("th", "Assignee"), el("th", "Updated"));
  const count = el("p", "", "muted"), more = el("button", "Load more");
  const card = el("div", "", "card");
  card.append(table, count, more);
  main.replaceChildren(el("h1", "Issues"), bar, card);
  let after = null;
  const load = async () => {
    const vars = { first: 20, after };
    for (const k of ["project", "state", "assignee"]) if (filters[k]) vars[k] = filters[k];
    const { issues } = await gql("Issues", vars);
    for (const { node: i } of issues.edges) {
      const tr = table.insertRow();
      tr.id = "issue-" + i.key;
      const a = el("a", i.key); a.href = "/issues/" + i.key; a.onclick = nav;
      tr.insertCell().append(a);
      tr.insertCell().textContent = i.title;
      tr.insertCell().append(el("span", i.state.replace("_", " ").toLowerCase(), "state"));
      tr.insertCell().textContent = i.priority.toLowerCase();
      tr.insertCell().textContent = i.assignee ? i.assignee.name : "";
      tr.insertCell().textContent = new Date(i.updated).toLocaleDateString();
    }
    after = issues.pageInfo.endCursor;
    more.hidden = !issues.pageInfo.hasNextPage;
    count.textContent = `${table.rows.length - 1} of ${issues.totalCount}`;
  };
  more.onclick = load;
  await load();
}

async function issue(key) {
  const { issue: i } = await gql("Issue", { key });
  if (!i) { main.replaceChildren(el("p", "No issue " + key, "error")); return; }
  const card = el("div", "", "card");
  card.append(el("p", `${i.project.name} · ${i.key}`, "muted"), el("h1", i.title), el("p", i.description));
  const state = el("select");
  state.setAttribute("aria-label", "State");
  for (const s of ["TODO", "IN_PROGRESS", "IN_REVIEW", "DONE"]) state.append(new Option(s.replace("_", " ").toLowerCase(), s, false, s === i.state));
  state.onchange = async () => { await gql("UpdateIssue", { key, input: { state: state.value } }); toast(`${key} is now ${state.value}`); };
  card.append(el("p", `Priority: ${i.priority.toLowerCase()} · Assignee: ${i.assignee ? i.assignee.name : "nobody"} · Labels: ${i.labels.join(", ") || "none"}`), state);
  main.replaceChildren(card);
}

async function newIssue() {
  const { projects } = await gql("Projects");
  const form = el("form", "", "card");
  form.innerHTML = `<h1>New issue</h1><label>Project <select name="project"></select></label><label>Title <input name="title" required></label>
    <label>Description <textarea name="description" rows="4"></textarea></label>
    <label>Priority <select name="priority"><option>LOW</option><option selected>MEDIUM</option><option>HIGH</option><option>URGENT</option></select></label>
    <button class="primary">Create issue</button>`;
  for (const p of projects) form.project.append(new Option(p.name, p.key));
  form.onsubmit = async (e) => {
    e.preventDefault();
    const f = Object.fromEntries(new FormData(form));
    const { createIssue } = await gql("CreateIssue", { input: f });
    history.pushState(null, "", "/issues/" + createIssue.key);
    route();
  };
  main.replaceChildren(form);
}

function nav(e) { e.preventDefault(); history.pushState(null, "", e.currentTarget.getAttribute("href")); route(); }
function toast(t) { const b = document.getElementById("toast"); b.textContent = t; b.style.display = "block"; setTimeout(() => (b.style.display = "none"), 3000); }

async function route() {
  try {
    const path = location.pathname;
    if (path.startsWith("/issues/")) await issue(decodeURIComponent(path.slice(8)));
    else if (path === "/new") await newIssue();
    else await issues();
  } catch (e) { main.replaceChildren(el("p", e.message, "error")); }
}

// Live updates from teammates.
function live() {
  const ws = new WebSocket(`wss://${location.host}/live?access_token=${encodeURIComponent(store.access)}`);
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (!msg.issue) return;
    toast(`${msg.issue.key} ${msg.type === "issue.created" ? "created" : "updated"}: ${msg.issue.title}`);
    const row = document.getElementById("issue-" + msg.issue.key);
    if (row) row.cells[2].firstChild.textContent = msg.issue.state.replace("_", " ").toLowerCase();
  };
  ws.onclose = () => setTimeout(() => store.access && live(), 5000);
}

let me;
async function start() {
  if (!store.refresh) return signedOut();
  try { me = (await gql("Viewer")).viewer; } catch { return; }
  document.getElementById("who").textContent = me.name;
  const out = document.getElementById("signout");
  out.hidden = false;
  out.onclick = async () => {
    const r = await fetch("/api/auth/logout", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ refreshToken: store.refresh }) }).then((r) => r.json());
    store.clear();
    location.href = r.logoutUrl;
  };
  for (const a of document.querySelectorAll("header nav a")) a.onclick = nav;
  window.onpopstate = route;
  live();
  route();
}

if (location.pathname === "/callback") callback(); else start();
