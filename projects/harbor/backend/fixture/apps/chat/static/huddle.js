// Huddle. The token is kept in IndexedDB, as Firebase-style apps do.
const db = new Promise((resolve, reject) => {
  const open = indexedDB.open("huddle", 1);
  open.onupgradeneeded = () => open.result.createObjectStore("auth");
  open.onsuccess = () => resolve(open.result);
  open.onerror = () => reject(open.error);
});
async function idb(mode, fn) {
  const tx = (await db).transaction("auth", mode);
  const req = fn(tx.objectStore("auth"));
  return new Promise((resolve, reject) => { req.onsuccess = () => resolve(req.result); req.onerror = () => reject(req.error); });
}
const getToken = () => idb("readonly", (s) => s.get("session")).then((v) => v && v.token);
const setToken = (token) => idb("readwrite", (s) => s.put({ token, savedAt: Date.now() }, "session"));
const clearToken = () => idb("readwrite", (s) => s.delete("session"));

let token, channel, events;
const $ = (id) => document.getElementById(id);
const el = (tag, text, cls) => Object.assign(document.createElement(tag), { textContent: text ?? "", className: cls || "" });

async function api(path, options = {}) {
  const r = await fetch("/api" + path, { ...options, headers: { "Content-Type": "application/json", Authorization: "Bearer " + token } });
  const body = await r.json();
  if (r.status === 401) { await signedOut(body.message); throw new Error(body.message); }
  if (!r.ok) throw new Error(body.message);
  return body;
}

async function signedOut(message) {
  await clearToken();
  token = null;
  if (events) events.close();
  $("app").hidden = true;
  $("signin").hidden = false;
  $("signin-error").textContent = message || "";
}

// Sign-in happens in a popup, which posts the token back.
$("open-signin").onclick = () => window.open("/auth/popup", "huddle-signin", "width=440,height=560");
window.addEventListener("message", async (e) => {
  if (e.origin !== location.origin || e.data?.type !== "huddle-auth") return;
  await setToken(e.data.token);
  start();
});

function render(m) {
  const d = el("div", "", "msg" + (m.mentionsMe ? " mention" : ""));
  d.append(el("span", m.author.name, "who"), el("span", new Date(m.sent).toLocaleString(), "when"), el("div", m.text));
  return d;
}

async function showChannel(id) {
  channel = id;
  $("compose").hidden = false;
  for (const a of $("channels").children) a.classList.toggle("active", a.dataset.id === id);
  const { messages } = await api(`/channels/${id}/messages`);
  const ch = channels.find((c) => c.id === id);
  $("title").textContent = `#${ch.name} · ${ch.topic}`;
  $("messages").replaceChildren(...messages.map(render));
  $("messages").scrollTop = 1e9;
}

let channels = [];
async function start() {
  token = await getToken();
  if (!token) return signedOut();
  const me = await api("/me");
  $("signin").hidden = true;
  $("app").hidden = false;
  $("who").textContent = me.name;
  ({ channels } = await api("/channels"));
  $("channels").replaceChildren(...channels.map((c) => {
    const a = el("a", "# " + c.name);
    a.href = "#" + c.id;
    a.dataset.id = c.id;
    a.onclick = (e) => { e.preventDefault(); showChannel(c.id); };
    return a;
  }));
  const { mentions } = await api("/mentions?unread=1");
  $("mention-count").textContent = mentions.length ? `(${mentions.length})` : "";
  showChannel(channels[0].id);

  events = new EventSource("/api/events?access_token=" + encodeURIComponent(token));
  events.addEventListener("message", (e) => {
    const m = JSON.parse(e.data);
    if (m.channel === channel) { $("messages").append(render(m)); $("messages").scrollTop = 1e9; }
  });
  events.addEventListener("signed-out", () => signedOut("You were signed out on all devices."));
}

$("mentions").onclick = async () => {
  const { mentions } = await api("/mentions?unread=1");
  channel = null;
  $("compose").hidden = true;
  $("title").textContent = "Unread mentions";
  $("messages").replaceChildren(...(mentions.length ? mentions.map(render) : [el("p", "No unread mentions.")]));
  if (mentions.length) await api("/mentions/read", { method: "POST", body: JSON.stringify({ ids: mentions.map((m) => m.id) }) });
  $("mention-count").textContent = "";
};
$("compose").onsubmit = async (e) => {
  e.preventDefault();
  const text = e.target.text.value;
  e.target.text.value = "";
  await api(`/channels/${channel}/messages`, { method: "POST", body: JSON.stringify({ text }) });
};
$("search").onsubmit = async (e) => {
  e.preventDefault();
  channel = null;
  $("compose").hidden = true;
  try {
    const { query, results } = await api("/search?q=" + encodeURIComponent(e.target.q.value));
    $("title").textContent = `Search: ${query}`;
    $("messages").replaceChildren(...(results.length ? results.map(render) : [el("p", "Nothing found.")]));
  } catch (err) { $("title").textContent = err.message; }
};
$("signout").onclick = async () => { await api("/auth/logout-everywhere", { method: "POST" }); signedOut(); };
start();
