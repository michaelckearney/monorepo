<script setup lang="ts">
import { Button } from "@ui/components/button";
import { computed, onMounted, ref, watch } from "vue";
import { Tools } from "@backend";
import type { Manifest } from "../manifest";

// Whether you are signed in to the service, in Harbor's browser, and the way
// to sign in. Checking starts the browser the first time.
const props = defineProps<{ service: Manifest }>();

type State = "checking" | "signed-in" | "signed-out" | "signing-in" | "failed";
const state = ref<State>("checking");
const emit = defineEmits<{ state: [state: State] }>();
watch(state, (s) => emit("state", s), { immediate: true });
const problem = ref("");
const message = (e: unknown) => (e instanceof Error ? e.message : String(e));
// The browser layer puts the plain sentence first and Chromium's own reason
// after a newline: the person using the app reads the sentence, and whoever
// is debugging still has the reason, muted on the same line and in its title.
const headline = computed(() => problem.value.split("\n", 1)[0]);
const reason = computed(() => {
  const at = problem.value.indexOf("\n");
  return at < 0 ? "" : problem.value.slice(at + 1);
});

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
      <span class="dot need"></span>Signed out
      <Button size="sm" variant="outline" type="button" @click="signIn">Sign in</Button>
    </template>
    <template v-else-if="state === 'signing-in'">
      Signing in to {{ service.title }}…
    </template>
    <template v-else>
      <span class="problem">{{ headline }}</span>
      <span v-if="reason">{{ reason }}</span>
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
.dot.need {
  background: var(--warning);
}
.problem {
  color: var(--destructive);
}
</style>
