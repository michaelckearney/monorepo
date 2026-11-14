<script setup lang="ts">
// The family at a glance: schoolwork from Maple Grove Schools, what's coming
// up at school and at the doctor's, messages from Clearwater Health's care
// team and test results for everyone, and the latest photos from Shoebox.
// Clicking a message marks it read. Clearwater's list of people is asked
// for once, though three widgets need it: they ask at the same time.
import { computed } from "vue"
import { call, format, useAction, useData, Agenda, Gallery, Grid, Inbox, StatusList, Tasks, Widget } from "harbor"

// Today where you are, as YYYY-MM-DD.
const today = new Date().toLocaleDateString("en-CA")

const school = useData(
  async () => {
    const [{ assignments }, { events }] = await Promise.all([
      call("maplegrove", "list_assignments"),
      call("maplegrove", "list_events"),
    ])
    const work = assignments
      .filter((a) => a.status === "missing" || (a.status === "assigned" && a.due >= today))
      .sort((a, b) => a.student.localeCompare(b.student) || (a.status === "missing" ? -1 : 1))
    return { work, events: events.filter((e) => e.date >= today) }
  },
  { source: "Maple Grove Schools" },
)

// Clearwater shows one person at a time; each call says whom it is about.
async function forEveryone(tool, input, list) {
  const { people } = await call("clearwater", "list_people")
  const all = []
  for (const p of people) {
    const out = await call("clearwater", tool, { person: p.id, ...input })
    all.push(...out[list].map((x) => ({ ...x, person: p.name, personId: p.id })))
  }
  return all
}
const visits = useData(() => forEveryone("list_appointments", { upcoming: true }, "appointments"), { source: "Clearwater Health" })
const results = useData(
  async () => (await forEveryone("list_results", {}, "results")).sort((a, b) => b.collected.localeCompare(a.collected)).slice(0, 6),
  { source: "Clearwater Health" },
)
const messages = useData(
  async () => (await forEveryone("list_messages", {}, "messages")).sort((a, b) => b.sent.localeCompare(a.sent)),
  { source: "Clearwater Health" },
)
const markRead = useAction("clearwater", "mark_message_read")

// School events and appointments, as one agenda.
const comingUp = computed(() => [
  ...(school.data?.events ?? []).map((e) => ({ start: e.date, what: e.title, where: e.school })),
  ...(visits.data ?? []).map((v) => ({ start: v.start, what: v.reason, where: `${v.clinician}, ${v.location}`, who: v.person })),
])

// The album with the newest photo, and its newest photos.
const photos = useData(
  async () => {
    const { albums } = await call("shoebox", "list_albums")
    const latest = (album) => album.photos.reduce((m, p) => (p.taken > m ? p.taken : m), "")
    let newest = null
    for (const a of albums) {
      const album = await call("shoebox", "get_album", { album: a.id })
      if (!newest || latest(album) > latest(newest)) {
        newest = album
      }
    }
    if (!newest) {
      return { album: "", photos: [] }
    }
    const picks = [...newest.photos].sort((a, b) => b.taken.localeCompare(a.taken)).slice(0, 6)
    const shown = []
    for (const p of picks) {
      const file = await call("shoebox", "download_photo", { photo: p.id })
      shown.push({ id: p.id, name: p.name, src: file.data_url })
    }
    return { album: newest.title, photos: shown }
  },
  { source: "Shoebox" },
)
</script>

<template>
  <Grid>
    <Widget title="Schoolwork" :from="school">
      <Tasks
        :items="school"
        field="work"
        group="student"
        label="title"
        status="status"
        due="due"
        detail="course"
        :tone="{ assigned: 'neutral' }"
        empty="Nothing due."
      />
    </Widget>
    <Widget title="Coming up" :from="[school, visits]">
      <Agenda :items="comingUp" start="start" label="what" where="where" who="who" :limit="8" />
    </Widget>
    <Widget title="Messages from the care team" :from="[messages, markRead]">
      <Inbox
        :items="messages"
        sender="from"
        :subject="(m) => `${m.person}: ${m.subject}`"
        preview="body"
        time="sent"
        :unread="(m) => !m.read"
        :limit="5"
        @open="(m) => !m.read && markRead.run({ id: m.id, person: m.personId })"
      />
    </Widget>
    <Widget title="Test results" :from="results" :span="8">
      <StatusList
        :items="results"
        :label="(r) => `${r.person}: ${r.name}`"
        :detail="(r) => `Collected ${format(r.collected, 'date')}, ordered by ${r.ordered_by}`"
        :status="(r) => (r.flagged ? `${r.flagged} out of range` : r.status)"
      />
    </Widget>
    <Widget :title="photos.data?.album || 'Photos'" :from="photos">
      <Gallery :items="photos" field="photos" src="src" alt="name" />
    </Widget>
  </Grid>
</template>
