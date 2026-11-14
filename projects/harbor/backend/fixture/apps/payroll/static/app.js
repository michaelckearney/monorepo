// Payday's app. The access token lives only in this variable: a reload gets
// a new one from /api/auth/refresh, which uses an HttpOnly cookie.
let token = null;
const main = document.getElementById("main");
const el = (tag, text, cls) => Object.assign(document.createElement(tag), { textContent: text ?? "", className: cls || "" });
const money = (c) => (c / 100).toLocaleString("en-US", { style: "currency", currency: "USD" });

async function refresh() {
  const r = await fetch("/api/auth/refresh", { method: "POST" });
  const body = await r.json();
  if (!r.ok) throw Object.assign(new Error(body.message), { signedOut: true });
  token = body.accessToken;
}

// Replies are JSON, gzipped, in base64.
async function unpack(r) {
  const bytes = Uint8Array.from(atob(await r.text()), (c) => c.charCodeAt(0));
  const text = await new Response(new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"))).text();
  return JSON.parse(text);
}

let nextId = 1;
// rpc sends calls in one batch: [[method, params], ...]. It returns each
// result, or throws the first error.
async function rpc(...calls) {
  const batch = calls.map(([method, params]) => ({ id: nextId++, method, params: params || {} }));
  for (let attempt = 0; ; attempt++) {
    const r = await fetch("/rpc?batch=1", { method: "POST", headers: { "Content-Type": "application/json", Authorization: "Bearer " + token }, body: JSON.stringify(batch) });
    if (r.status === 401 && attempt === 0) { await refresh(); continue; }
    if (r.status === 401) throw Object.assign(new Error("Signed out"), { signedOut: true });
    const results = await unpack(r);
    const failed = results.find((x) => x.error);
    if (failed) throw Object.assign(new Error(failed.error.message), failed.error);
    return results.map((x) => x.result);
  }
}

function signedOut(message) {
  const box = el("div", "", "card narrow");
  const a = el("a", "Sign in with Northwind ID", "primary");
  a.href = "/auth/login";
  box.append(el("h2", "Payday"), el("p", message || "Payslips, time off, and your details.", "muted"), a);
  main.replaceChildren(box);
}

async function render() {
  const [me, payslips, balances, requests, bank] = await rpc(["me.get"], ["payslips.list"], ["timeoff.balances"], ["timeoff.list"], ["bank.get"]);
  document.getElementById("who").textContent = me.name;
  const card = (title, ...children) => { const c = el("section", "", "card"); c.append(el("h2", title), ...children); return c; };

  const profile = el("p", `${me.title}, ${me.department} · Manager: ${me.manager} · Employee ${me.id} · Paid ${me.payFrequency.toLowerCase()}`);

  const slips = el("table");
  slips.insertRow().append(el("th", "Pay date"), el("th", "Period"), el("th", "Gross", "num"), el("th", "Net", "num"), el("th", ""));
  for (const s of payslips) {
    const tr = slips.insertRow();
    tr.insertCell().textContent = s.payDate;
    tr.insertCell().textContent = `${s.periodStart} – ${s.periodEnd}`;
    Object.assign(tr.insertCell(), { textContent: money(s.grossCents), className: "num" });
    Object.assign(tr.insertCell(), { textContent: money(s.netCents), className: "num" });
    const b = el("button", "PDF");
    b.onclick = async () => {
      // The PDF needs the token, so it is fetched, then opened as a blob.
      const r = await fetch(s.pdfUrl, { headers: { Authorization: "Bearer " + token } });
      window.open(URL.createObjectURL(await r.blob()));
    };
    tr.insertCell().append(b);
  }

  const bal = el("p", balances.map((b) => `${b.label}: ${b.hours} hours`).join(" · "));
  const reqs = el("table");
  reqs.insertRow().append(el("th", "Request"), el("th", "Dates"), el("th", "Hours", "num"), el("th", "Status"));
  for (const t of requests) reqs.insertRow().append(el("td", `${t.id} · ${t.type}${t.note ? " · " + t.note : ""}`), el("td", `${t.start} – ${t.end}`), el("td", String(t.hours), "num"), el("td", t.status));
  const form = el("form", "", "row");
  form.innerHTML = `<label>Type <select name="type">${balances.map((b) => `<option value="${b.type}">${b.label}</option>`).join("")}</select></label>
    <label>From <input type="date" name="start" required></label><label>To <input type="date" name="end" required></label>
    <label>Note <input name="note"></label><button class="primary">Request time off</button><span class="error"></span>`;
  form.onsubmit = async (e) => {
    e.preventDefault();
    try { await rpc(["timeoff.request", Object.fromEntries(new FormData(form))]); render(); }
    catch (err) { form.querySelector(".error").textContent = err.message; }
  };

  const bankForm = el("form", "", "row");
  bankForm.id = "bank";
  bankForm.innerHTML = `<label>Bank <input name="bankName" required></label><label>Routing number <input name="routingNumber" required></label>
    <label>Account number <input name="accountNumber" required></label>
    <label>Type <select name="accountType"><option>checking</option><option>savings</option></select></label><button class="primary">Change</button><span class="error"></span>`;
  bankForm.onsubmit = async (e) => {
    e.preventDefault();
    try { await rpc(["bank.update", Object.fromEntries(new FormData(bankForm))]); render(); }
    catch (err) {
      if (err.code === "reauth_required") { location.href = err.data.url; return; }
      bankForm.querySelector(".error").textContent = err.message;
    }
  };

  main.replaceChildren(
    el("h1", "Hi, " + me.name.split(" ")[0]), card("Profile", profile), card("Payslips", slips),
    card("Time off", bal, reqs, form),
    card("Direct deposit", el("p", `${bank.bankName}, ${bank.accountType} account ending ${bank.accountLast4}`), bankForm),
  );
  const out = document.getElementById("signout");
  out.hidden = false;
  out.onclick = async () => { const r = await fetch("/api/auth/logout", { method: "POST" }).then((r) => r.json()); location.href = r.logoutUrl; };
}

(async () => {
  try { await refresh(); await render(); }
  catch (e) { if (e.signedOut) signedOut(e.message === "Sign in with Northwind ID." ? "" : e.message); else main.replaceChildren(el("p", e.message, "error")); }
})();
