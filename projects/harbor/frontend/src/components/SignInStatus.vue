<script setup lang="ts">
import { Button } from "@ui/components/button";
import { onMounted, ref } from "vue";
import { Tools } from "@backend";
import type { Manifest } from "../manifest";

// Whether you are signed in to the service, in Harbor's browser, and the way
// to sign in. Checking starts the browser the first time.
const props = defineProps<{ service: Manifest }>();

type State = "checking" | "signed-in" | "signed-out" | "signing-in" | "failed";
const state = ref<State>("checking");
const problem = ref("");
const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

async function checkNow() {
  state.value = "checking";
  try {
    state.value = (await Tools.SignedIn(props.service.name)) ? "signed-in" : "signed-out";
  } catch (e) {
    problem.value = message(e);
    state.value = "failed";
  }
}

async function signIn() {
  state.value = "signing-in";
  try {
    state.value = (await Tools.SignIn(props.service.name, "")) ? "signed-in" : "signed-out";
  } catch (e) {
    problem.value = message(e);
    state.value = "failed";
  }
}

onMounted(checkNow);
</script>

<template>
  <div class="sign-in" role="status" aria-label="Sign-in">
    <template v-if="state === 'checking'">Checking whether you are signed in…</template>
    <template v-else-if="state === 'signed-in'"><span class="dot ok"></span>Signed in</template>
    <template v-else-if="state === 'signed-out'">
      <span class="dot"></span>Signed out
      <Button size="sm" variant="outline" type="button" @click="signIn">Sign in</Button>
    </template>
    <template v-else-if="state === 'signing-in'">
      Signing in to {{ service.title }}…
    </template>
    <template v-else>
      <span class="problem">{{ problem }}</span>
      <Button size="sm" variant="outline" type="button" @click="checkNow">Check again</Button>
    </template>
  </div>
</template>

<style scoped>
.sign-in {
  display: flex;
  align-items: center;
  gap: calc(var(--spacing) * 2);
  margin-top: calc(var(--spacing) * 3);
  color: var(--muted-foreground);
}
.dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 50%;
  background: var(--muted-foreground);
}
.dot.ok {
  background: var(--success);
}
.problem {
  color: var(--destructive);
}
</style>
