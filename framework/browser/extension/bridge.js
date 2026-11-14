// Headless Chromium cannot show its password box or its passkey dialog, so
// what a site asks for there never comes. This page takes those requests
// instead, asks the app, and gives the site the app's answer. Desktop opens
// it in a tab of its own, and adds desktopAsk to it.
//
// Chrome still checks each request first: that a passkey's site is the
// page's own, and which site a password is for.

const pending = new Map();
let next = 0;

function ask(question) {
  return new Promise((resolve) => {
    const id = ++next;
    pending.set(id, resolve);
    desktopAsk(JSON.stringify({ id, ...question }));
  });
}

// Desktop calls this with the answer to a question.
window.desktopAnswer = (id, answer) => {
  pending.get(id)?.(answer);
  pending.delete(id);
};

// A site asks for a password over HTTP, such as a router's admin page.
// The answer is { authCredentials: { username, password } } or { cancel: true }.
chrome.webRequest.onAuthRequired.addListener(
  (details, callback) => {
    ask({
      kind: "password",
      requestId: details.requestId,
      url: details.url,
      host: details.challenger.host,
      port: details.challenger.port,
      realm: details.realm ?? "",
      isProxy: details.isProxy,
    }).then(callback);
  },
  { urls: ["<all_urls>"] },
  ["asyncBlocking"],
);

// A page asks for a passkey, with navigator.credentials. The options are
// the page's, as JSON, with the page's true origin added by Chrome. The
// answer is { responseJson } or { error: { name, message } }.
const proxy = chrome.webAuthenticationProxy;
proxy.onCreateRequest.addListener(async ({ requestId, requestDetailsJson }) => {
  const answer = await ask({ kind: "passkey", create: true, requestId: String(requestId), options: JSON.parse(requestDetailsJson) });
  // A request the page gave up on cannot be completed.
  await proxy.completeCreateRequest({ requestId, ...answer }).catch(() => {});
});
proxy.onGetRequest.addListener(async ({ requestId, requestDetailsJson }) => {
  const answer = await ask({ kind: "passkey", create: false, requestId: String(requestId), options: JSON.parse(requestDetailsJson) });
  await proxy.completeGetRequest({ requestId, ...answer }).catch(() => {});
});
// The app answers passkey requests itself, as a platform authenticator.
proxy.onIsUvpaaRequest.addListener(({ requestId }) => {
  proxy.completeIsUvpaaRequest({ requestId, isUvpaa: true });
});
proxy.onRequestCanceled.addListener((requestId) => {
  desktopAsk(JSON.stringify({ kind: "canceled", requestId: String(requestId) }));
});

// Desktop waits for this before it uses the browser. It is undefined once
// attached, or why not.
window.attached = proxy.attach();
