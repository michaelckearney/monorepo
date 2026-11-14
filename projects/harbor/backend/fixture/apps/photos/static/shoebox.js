// Shoebox. Writes are signed with the session's signing key.
const main = document.getElementById("main");
const el = (tag, text, cls) => Object.assign(document.createElement(tag), { textContent: text ?? "", className: cls || "" });
let session;

async function api(path, options = {}) {
  const r = await fetch("/api" + path, options);
  const body = await r.json();
  if (r.status === 401 && body.error === "unauthenticated") { location.href = "/login"; throw new Error(body.message); }
  if (!r.ok) throw new Error(body.message);
  return body;
}

// sign adds X-Shoebox-Date and X-Shoebox-Signature:
// hex(HMAC-SHA256(signingKey, method + "\n" + path + "\n" + date)).
async function sign(method, path) {
  const date = new Date().toISOString().replace(/\.\d+Z$/, "Z");
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(session.signingKey), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const mac = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(`${method}\n${path}\n${date}`));
  const hex = [...new Uint8Array(mac)].map((b) => b.toString(16).padStart(2, "0")).join("");
  return { "X-Shoebox-Date": date, "X-Shoebox-Signature": hex };
}

const thumbs = new Map();
async function thumb(img, id) {
  // Each photo is fetched through a short-lived signed URL.
  if (!thumbs.has(id)) thumbs.set(id, api(`/photos/${id}/download`).then((d) => d.url));
  img.src = await thumbs.get(id);
}

async function albums() {
  const { albums } = await api("/albums");
  const grid = el("div", "", "grid");
  for (const a of albums) {
    const t = el("div", "", "tile");
    const img = el("img");
    img.alt = a.title;
    if (a.coverPhoto) thumb(img, a.coverPhoto);
    t.append(img, el("p", a.title), el("p", `${a.count} photos`, "muted"));
    t.onclick = () => { history.pushState(null, "", "#" + a.id); album(a.id); };
    grid.append(t);
  }
  main.replaceChildren(el("h1", "Albums"), grid);
}

async function album(id) {
  const { album, photos } = await api("/albums/" + id);
  const bar = el("div", "", "toolbar");
  const back = el("button", "← Albums");
  back.onclick = () => { history.pushState(null, "", "/"); albums(); };
  const upload = el("input");
  upload.type = "file";
  upload.accept = "image/*";
  upload.setAttribute("aria-label", "Upload a photo");
  upload.onchange = async () => {
    const form = new FormData();
    form.append("file", upload.files[0]);
    const path = `/api/albums/${id}/photos`;
    await api(`/albums/${id}/photos`, { method: "POST", body: form, headers: await sign("POST", path) });
    album(id);
  };
  bar.append(back, el("strong", album.title), upload);
  const grid = el("div", "", "grid");
  for (const p of photos) {
    const t = el("div", "", "tile");
    const img = el("img");
    img.alt = p.name;
    thumb(img, p.id);
    const share = el("button", "Share");
    share.onclick = async (e) => {
      e.stopPropagation();
      const path = `/api/photos/${p.id}/share`;
      const { url } = await api(`/photos/${p.id}/share`, { method: "POST", headers: await sign("POST", path) });
      prompt("Anyone with this link can see the photo:", url);
    };
    t.onclick = async () => window.open((await api(`/photos/${p.id}/download`)).url);
    t.append(img, el("p", p.name), el("p", new Date(p.taken).toLocaleDateString(), "muted"), share);
    grid.append(t);
  }
  main.replaceChildren(bar, grid);
}

document.getElementById("add-passkey").onclick = async () => {
  const options = await api("/passkeys/register/begin", { method: "POST" });
  const credential = await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey) });
  await api("/passkeys/register/finish", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(credential.toJSON()) });
  start();
};
document.getElementById("signout").onclick = async () => { await api("/logout", { method: "POST" }); location.href = "/login"; };

async function start() {
  session = await api("/session");
  document.getElementById("who").textContent = session.name;
  const notice = document.getElementById("notice");
  notice.hidden = session.passkeys > 0;
  notice.textContent = "Sign in faster next time: add a passkey.";
  document.getElementById("add-passkey").hidden = session.passkeys > 0;
  const id = location.hash.slice(1);
  id ? album(id) : albums();
}
window.onpopstate = () => (location.hash ? album(location.hash.slice(1)) : albums());
start();
