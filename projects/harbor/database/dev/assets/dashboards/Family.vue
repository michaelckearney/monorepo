<script setup lang="ts">
// The family at a glance: schoolwork from Maple Grove Schools, appointments
// and test results from Clearwater Health for everyone, and the latest
// photos from Shoebox.
import { call, Card, format, Grid, List, useData } from "harbor"

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
    return { work, events: events.filter((e) => e.date >= today).slice(0, 4) }
  },
  { source: "Maple Grove Schools" },
)

// Clearwater shows one person at a time; each call says whom it is about.
async function forEveryone(tool, input, list) {
  const { people } = await call("clearwater", "list_people")
  const all = []
  for (const p of people) {
    const out = await call("clearwater", tool, { person: p.id, ...input })
    all.push(...out[list].map((x) => ({ ...x, person: p.name })))
  }
  return all
}
const visits = useData(
  async () => (await forEveryone("list_appointments", { upcoming: true }, "appointments")).sort((a, b) => a.start.localeCompare(b.start)),
  { source: "Clearwater Health" },
)
const results = useData(
  async () => (await forEveryone("list_results", {}, "results")).sort((a, b) => b.collected.localeCompare(a.collected)).slice(0, 6),
  { source: "Clearwater Health" },
)

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
    <List
      title="Schoolwork"
      :items="school"
      field="work"
      group="student"
      :label="(a) => (a.status === 'missing' ? `Missing: ${a.title}` : a.title)"
      :detail="(a) => (a.status === 'missing' ? a.course : `${a.course}, due ${format(a.due, 'date')}`)"
      empty="Nothing due."
    />
    <List
      title="Coming up at school"
      :items="school"
      field="events"
      label="title"
      :detail="(e) => `${format(e.date, 'date')}, ${e.school}`"
      empty="No events coming up."
    />
    <List
      title="Appointments"
      :items="visits"
      :label="(v) => `${v.person}: ${v.reason}`"
      :detail="(v) => `${format(v.start.slice(0, 10), 'date')} at ${v.start.slice(11, 16)}, ${v.clinician}`"
      empty="No appointments coming up."
    />
    <List
      title="Test results"
      :items="results"
      :label="(r) => `${r.person}: ${r.name}`"
      :detail="(r) => `${format(r.collected, 'date')}, ${r.status}${r.flagged ? ` · ${r.flagged} out of range` : ''}`"
      :span="2"
    />
    <Card :title="photos.data?.album || 'Photos'" :from="photos" span="full">
      <div class="photos">
        <img v-for="p in photos.data.photos" :key="p.id" :src="p.src" :alt="p.name" />
      </div>
    </Card>
  </Grid>
</template>

<style scoped>
.photos {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(8rem, 1fr));
  gap: 0.5rem;
}
img {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: calc(var(--radius) - 2px);
}
</style>
