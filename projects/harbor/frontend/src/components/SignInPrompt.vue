<script setup lang="ts">
import { Button } from "@ui/components/button";
import { ref, watch } from "vue";
import { Tools } from "@backend";
import type { Prompt } from "../prompt";

// What a sign-in asks you beside the page, which headless Chromium cannot
// ask you itself: an HTTP password, a passkey, a security key's PIN, a page's
// dialog or file picker; and whether to save the login you typed. It is
// drawn over the page, which waits for the answer.
const props = defineProps<{ service: string; prompt: Prompt }>();
const emit = defineEmits<{ problem: [message: string] }>();

const username = ref("");
const password = ref("");
const withoutAsking = ref(false);
const pin = ref("");
const text = ref(props.prompt.default ?? "");
const files = ref<File[]>([]);
watch(
  () => props.prompt.id,
  () => {
    password.value = "";
    pin.value = "";
    text.value = props.prompt.default ?? "";
    files.value = [];
  },
);

const base64 = async (f: File) => {
  const bytes = new Uint8Array(await f.arrayBuffer());
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
};

async function answer(reply: {
  cancel?: boolean;
  username?: string;
  password?: string;
  passkey?: string;
  withoutAsking?: boolean;
  securityKey?: boolean;
  pin?: string;
  text?: string;
  files?: { name: string; data: string }[];
}) {
  try {
    await Tools.Answer(props.service, {
      prompt: props.prompt.id,
      cancel: false,
      username: "",
      password: "",
      passkey: "",
      withoutAsking: false,
      securityKey: false,
      pin: "",
      text: "",
      files: [],
      ...reply,
    } as never);
  } catch (e) {
    emit("problem", e instanceof Error ? e.message : String(e));
  }
}

async function giveFiles() {
  const chosen = await Promise.all(files.value.map(async (f) => ({ name: f.name, data: await base64(f) })));
  await answer({ files: chosen });
}
</script>

<template>
  <div class="over">
    <form
      v-if="prompt.kind === 'password'"
      class="prompt"
      :aria-label="`Password for ${prompt.site}`"
      @submit.prevent="answer({ username, password })"
    >
      <p>
        {{ prompt.site }} asks for a password<template v-if="prompt.realm"> for “{{ prompt.realm }}”</template>.
        Harbor saves it, and gives it to the site from now on.
      </p>
      <p v-if="prompt.wrong" class="problem">That username or password was wrong. Try again.</p>
      <label>Username <input v-model="username" autocomplete="off" /></label>
      <label>Password <input v-model="password" type="password" autocomplete="off" /></label>
      <div class="actions">
        <Button size="sm" type="submit">Sign in</Button>
        <Button size="sm" variant="outline" type="button" @click="answer({ cancel: true })">Don't sign in</Button>
      </div>
    </form>
    <section v-else-if="prompt.kind === 'save-login'" class="prompt" :aria-label="`Save the login for ${prompt.site}`">
      <p>
        Save the login for {{ prompt.site }}<template v-if="prompt.account"> ({{ prompt.account }})</template>? Harbor
        then signs you in again on its own when the site signs you out.
      </p>
      <div class="actions">
        <Button size="sm" type="button" @click="answer({})">Save login</Button>
        <Button size="sm" variant="outline" type="button" @click="answer({ cancel: true })">Don't save</Button>
      </div>
    </section>
    <section v-else-if="prompt.kind === 'passkey'" class="prompt" :aria-label="`Passkey for ${prompt.site}`">
      <template v-if="prompt.create">
        <p v-if="prompt.keyOnly">
          {{ prompt.site }} asks to save a passkey for {{ prompt.account }}. It takes no kind of passkey Harbor makes, so
          save it on a security key.
        </p>
        <p v-else>
          {{ prompt.site }} asks to save a passkey for {{ prompt.account }}: in Harbor, which uses it only when you allow
          it here, or on a security key.
        </p>
        <div class="actions">
          <Button size="sm" v-if="!prompt.keyOnly" type="button" @click="answer({})">Save passkey</Button>
          <Button size="sm" variant="outline" type="button" @click="answer({ securityKey: true })">Use a security key</Button>
          <Button size="sm" variant="outline" type="button" @click="answer({ cancel: true })">Don't save</Button>
        </div>
      </template>
      <template v-else-if="prompt.passkeys?.length">
        <p>{{ prompt.site }} asks for a passkey. Sign in with:</p>
        <div class="actions">
          <Button size="sm"
            v-for="key in prompt.passkeys"
            :key="key.id"
            type="button"
           
            @click="answer({ passkey: key.id, withoutAsking })"
          >
            {{ key.account }}
          </Button>
          <Button size="sm" variant="outline" type="button" @click="answer({ securityKey: true })">Use a security key</Button>
          <Button size="sm" variant="outline" type="button" @click="answer({ cancel: true })">Don't use a passkey</Button>
        </div>
        <label class="check">
          <input v-model="withoutAsking" type="checkbox" />
          From now on, use it without asking while Harbor signs in to {{ prompt.site }}
        </label>
      </template>
      <template v-else>
        <p>
          {{ prompt.site }} asks for a passkey, but Harbor has none for it. Use a security key, or sign in another way;
          then the site may offer to add a passkey.
        </p>
        <div class="actions">
          <Button size="sm" variant="outline" type="button" @click="answer({ securityKey: true })">Use a security key</Button>
          <Button size="sm" variant="outline" type="button" @click="answer({ cancel: true })">OK</Button>
        </div>
      </template>
    </section>
    <form
      v-else-if="prompt.kind === 'security-key'"
      class="prompt"
      :aria-label="`Security key for ${prompt.site}`"
      @submit.prevent="answer({ pin })"
    >
      <template v-if="prompt.step === 'pin'">
        <p v-if="prompt.problem" class="problem">{{ prompt.problem }}</p>
        <label>
          Your security key's PIN
          <input v-model="pin" type="password" autocomplete="off" />
        </label>
        <p class="meta">{{ prompt.retries }} tries left before the key locks its PIN.</p>
      </template>
      <p v-else-if="prompt.step === 'touch'">Touch your security key.</p>
      <p v-else>Plug in your security key.</p>
      <div class="actions">
        <Button size="sm" v-if="prompt.step === 'pin'" type="submit">Continue</Button>
        <Button size="sm" variant="outline" type="button" @click="answer({ cancel: true })">Cancel</Button>
      </div>
    </form>
    <form
      v-else-if="prompt.kind === 'dialog'"
      class="prompt"
      :aria-label="`${prompt.site} says`"
      @submit.prevent="answer({ text })"
    >
      <p v-if="prompt.dialog === 'beforeunload'">{{ prompt.site }} asks whether to leave the page. What you changed may not be saved.</p>
      <p v-else class="message">{{ prompt.site }} says: {{ prompt.message }}</p>
      <label v-if="prompt.dialog === 'prompt'">Your answer <input v-model="text" autocomplete="off" /></label>
      <div class="actions">
        <Button size="sm" type="submit">{{ prompt.dialog === "beforeunload" ? "Leave" : "OK" }}</Button>
        <Button size="sm" variant="outline" v-if="prompt.dialog !== 'alert'" type="button" @click="answer({ cancel: true })">
          {{ prompt.dialog === "beforeunload" ? "Stay" : "Cancel" }}
        </Button>
      </div>
    </form>
    <form
      v-else-if="prompt.kind === 'files'"
      class="prompt"
      :aria-label="`Files for ${prompt.site}`"
      @submit.prevent="giveFiles"
    >
      <p>{{ prompt.site }} asks for {{ prompt.multiple ? "files" : "a file" }}.</p>
      <label>
        {{ prompt.multiple ? "Files" : "File" }}
        <input
          type="file"
          :multiple="prompt.multiple"
          :accept="prompt.accept || undefined"
          @change="files = Array.from(($event.target as HTMLInputElement).files ?? [])"
        />
      </label>
      <div class="actions">
        <Button size="sm" type="submit" :disabled="!files.length">Give it</Button>
        <Button size="sm" variant="outline" type="button" @click="answer({ cancel: true })">Cancel</Button>
      </div>
    </form>
    <section v-else-if="prompt.kind === 'downloaded'" class="prompt" :aria-label="`Downloaded ${prompt.name}`">
      <p>{{ prompt.site }} downloaded {{ prompt.name }}. It is in {{ prompt.saved }}.</p>
      <div class="actions">
        <Button size="sm" type="button" @click="answer({})">OK</Button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.over {
  display: grid;
  place-items: center;
  height: 100%;
  background: var(--overlay);
}
.prompt {
  max-width: 32rem;
  padding: calc(var(--spacing) * 3);
  background: var(--background);
  border: 1px solid var(--primary);
  border-radius: var(--radius-sm);
}
.prompt p {
  margin: 0 0 calc(var(--spacing) * 2);
}
.message {
  white-space: pre-wrap;
}
.prompt label {
  display: flex;
  align-items: center;
  gap: calc(var(--spacing) * 2);
  margin-bottom: calc(var(--spacing) * 2);
}
.prompt input {
  flex: 1;
  min-width: 0;
  font: inherit;
  padding: calc(var(--spacing) * 1.5) calc(var(--spacing) * 2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--background);
  color: var(--foreground);
}
.prompt label.check {
  margin: calc(var(--spacing) * 2) 0 0;
}
.meta {
  color: var(--muted-foreground);
}
.problem {
  color: var(--destructive);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: calc(var(--spacing) * 2);
}
</style>
