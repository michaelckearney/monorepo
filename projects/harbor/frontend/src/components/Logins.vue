<script setup lang="ts">
import { computed, ref } from "vue";
import { Pencil, Plus, ShieldCheck, TriangleAlert } from "lucide-vue-next";
import { sql, useQuery } from "@desktop";
import { Tools } from "@backend";
import { Badge } from "@ui/components/badge";
import { Button } from "@ui/components/button";
import { Input } from "@ui/components/input";
import DeleteButton from "@ui/components/DeleteButton.vue";
import IconButton from "@ui/components/IconButton.vue";
import SettingsList from "./SettingsList.vue";
import SettingsPane from "@ui/components/SettingsPane.vue";
import SettingsRow from "./SettingsRow.vue";
import Tip from "@ui/components/Tip.vue";

// The logins Harbor types in for you when a site signs you out, by site, and
// Harbor's own passkeys. Harbor offers to save a login after you sign in
// through its window; here you add one yourself, such as to give the secret
// an authenticator app keeps. Harbor asks you before it uses a passkey,
// unless you let it use one without asking. Both are kept unencrypted.
type Login = { origin: string; username: string; password: string; totp_secret: string };
const logins = useQuery<Login>(() => sql`SELECT origin, username, password, totp_secret FROM logins ORDER BY origin`);
type Passkey = { id: string; rp_id: string; user_name: string; auto: number };
const passkeys = useQuery<Passkey>(() => sql`SELECT id, rp_id, user_name, auto FROM passkeys ORDER BY rp_id, created`);

// A long list gets a filter.
const filter = ref("");
const shown = computed(() => {
  const f = filter.value.trim().toLowerCase();
  return f ? logins.value.filter((l) => l.origin.toLowerCase().includes(f) || l.username.toLowerCase().includes(f)) : logins.value;
});

const host = (origin: string) => new URL(origin).host;

// The login form, open in place of the lists: a new login, or the saved one
// for editing's site.
const form = ref(false);
const editing = ref("");
const site = ref("");
const username = ref("");
const password = ref("");
const secret = ref("");
const failure = ref("");

function edit(row?: Login) {
  form.value = true;
  editing.value = row?.origin ?? "";
  site.value = row?.origin ?? "";
  username.value = row?.username ?? "";
  password.value = row?.password ?? "";
  secret.value = row?.totp_secret ?? "";
  failure.value = "";
}

async function save() {
  failure.value = "";
  try {
    await Tools.SaveLogin(site.value, { username: username.value, password: password.value, totpSecret: secret.value });
    form.value = false;
  } catch (e) {
    failure.value = e instanceof Error ? e.message : String(e);
  }
}

async function setAuto(row: Passkey, e: Event) {
  await Tools.UsePasskeyWithoutAsking(row.id, (e.target as HTMLInputElement).checked);
}
</script>

<template>
  <SettingsPane v-if="form" :title="editing ? 'Edit login' : 'Add a login'" back @back="form = false">
    <form aria-label="Save a login" class="grid gap-4" @submit.prevent="save">
      <label class="grid gap-1.5">
        <span class="font-medium">Site</span>
        <Input v-model="site" placeholder="https://example.com" :disabled="!!editing" required />
      </label>
      <label class="grid gap-1.5">
        <span class="font-medium">Username</span>
        <Input v-model="username" autocomplete="off" required />
      </label>
      <label class="grid gap-1.5">
        <span class="font-medium">Password</span>
        <Input v-model="password" type="password" autocomplete="off" required />
      </label>
      <label class="grid gap-1.5">
        <span class="font-medium">Authenticator secret</span>
        <Input v-model="secret" autocomplete="off" spellcheck="false" placeholder="Optional" />
      </label>
      <p v-if="failure" class="text-destructive" role="alert">{{ failure }}</p>
      <Button type="submit" size="sm" class="justify-self-start">Save</Button>
    </form>
  </SettingsPane>

  <SettingsPane v-else title="Logins & passkeys">
    <template #title>
      <Badge variant="outline" class="text-muted-foreground"><TriangleAlert />Stored unencrypted</Badge>
    </template>
    <template #actions>
      <IconButton label="Add a login" @click="edit()"><Plus /></IconButton>
    </template>

    <section v-if="logins.length" aria-label="Saved logins" class="grid gap-2">
      <div class="flex items-center gap-3">
        <h3 class="flex-1 font-medium text-muted-foreground">Logins</h3>
        <Input v-if="logins.length > 8" v-model="filter" type="search" placeholder="Filter" aria-label="Filter logins" class="h-control-sm w-48" />
      </div>
      <SettingsList>
        <SettingsRow v-for="row in shown" :key="row.origin" :title="host(row.origin)" :detail="row.username">
          <template #status>
            <Tip v-if="row.totp_secret" label="Authenticator">
              <ShieldCheck class="size-4 shrink-0 text-muted-foreground" aria-label="With authenticator" />
            </Tip>
          </template>
          <IconButton label="Edit" :name="`Edit the login for ${row.origin}`" size="icon-xs" @click="edit(row)"><Pencil /></IconButton>
          <DeleteButton size="icon-xs" :what="`the login for ${row.origin}`" @confirm="Tools.DeleteLogin(row.origin)" />
        </SettingsRow>
      </SettingsList>
    </section>

    <section v-if="passkeys.length" aria-label="Passkeys" class="grid gap-2">
      <h3 class="font-medium text-muted-foreground">Passkeys</h3>
      <SettingsList>
        <SettingsRow v-for="row in passkeys" :key="row.id" :title="row.rp_id" :detail="row.user_name">
          <template #status>
            <label class="flex shrink-0 items-center gap-1.5 text-muted-foreground">
              <input type="checkbox" :checked="row.auto === 1" @change="setAuto(row, $event)" />
              Use without asking
            </label>
          </template>
          <DeleteButton size="icon-xs" :what="`the passkey for ${row.user_name} on ${row.rp_id}`" @confirm="Tools.DeletePasskey(row.id)" />
        </SettingsRow>
      </SettingsList>
    </section>
  </SettingsPane>
</template>
