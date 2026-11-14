<script setup lang="ts">
// The family at a glance: schoolwork from Maple Grove Schools, what's coming
// up at school and at the doctor's, messages from Clearwater Health's care
// team and test results for everyone, and the latest photos from Shoebox.
// Opening a message marks it read. Clearwater's list of people is asked for
// once, though three widgets need it.
import { computed } from "vue"
import { format, useService, AgendaWidget, GalleryWidget, Grid, InboxWidget, StatusWidget, TasksWidget } from "harbor"

const maplegrove = useService("maplegrove")
const clearwater = useService("clearwater")
const shoebox = useService("shoebox")

// Today where you are, as YYYY-MM-DD.
const today = new Date().toLocaleDateString("en-CA")
const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1)

const assignments = computed(() => maplegrove.list_assignments().assignments)
const events = computed(() => maplegrove.list_events().events.filter((e) => e.date >= today))
const schoolwork = computed(() =>
  assignments.value
    .filter((a) => a.status === "missing" || (a.status === "assigned" && a.due >= today))
    .sort((a, b) => a.student.localeCompare(b.student) || (a.status === "missing" ? -1 : 1))
    .map((a) => ({
      group: a.student,
      title: a.title,
      status: cap(a.status),
      tone: a.status === "assigned" ? "neutral" : undefined,
      due: a.due,
      detail: a.course,
    })),
)

// Clearwater shows one person at a time, so each read says whom it is
// about. Every person's read is asked for before any is used, so they load
// at once.
function forEveryone(read, list) {
  const asked = clearwater.list_people().people.map((p) => [p, read(p)])
  return asked.flatMap(([p, out]) => out[list].map((x) => ({ ...x, person: p.name, personId: p.id })))
}
const visits = computed(() => forEveryone((p) => clearwater.list_appointments({ person: p.id, upcoming: true }), "appointments"))
const results = computed(() =>
  forEveryone((p) => clearwater.list_results({ person: p.id }), "results")
    .sort((a, b) => b.collected.localeCompare(a.collected))
    .slice(0, 6)
    .map((r) => ({
      label: `${r.person}: ${r.name}`,
      detail: `Collected ${format(r.collected, "date")}, ordered by ${r.ordered_by}`,
      status: r.flagged ? `${r.flagged} out of range` : cap(r.status),
    })),
)
const messages = computed(() =>
  forEveryone((p) => clearwater.list_messages({ person: p.id }), "messages")
    .sort((a, b) => b.sent.localeCompare(a.sent))
    .map((m) => ({ from: m.from, subject: `${m.person}: ${m.subject}`, preview: m.body, at: m.sent, unread: !m.read, id: m.id, personId: m.personId })),
)

// School events and appointments, as one agenda.
const comingUp = computed(() => [
  ...events.value.map((e) => ({ start: e.date, title: e.title, where: e.school })),
  ...visits.value.map((v) => ({ start: v.start, title: v.reason, where: `${v.clinician}, ${v.location}`, who: v.person })),
])

// The album with the newest photo, and its newest photos.
const photos = computed(() => {
  const albums = shoebox.list_albums().albums.map((a) => shoebox.get_album({ album: a.id }))
  const latest = (album) => album.photos.reduce((m, p) => (p.taken > m ? p.taken : m), "")
  let newest = null
  for (const album of albums) {
    if (!newest || latest(album) > latest(newest)) {
      newest = album
    }
  }
  if (!newest) {
    return { album: "", pictures: [] }
  }
  const picks = [...newest.photos].sort((a, b) => b.taken.localeCompare(a.taken)).slice(0, 6)
  const files = picks.map((p) => shoebox.download_photo({ photo: p.id }))
  return { album: newest.title, pictures: picks.map((p, i) => ({ src: files[i].data_url, alt: p.name })) }
})
</script>

<template>
  <Grid>
    <TasksWidget title="Schoolwork" :tasks="schoolwork" empty="Nothing due." />
    <AgendaWidget title="Coming up" :events="comingUp" :limit="8" />
    <InboxWidget
      title="Messages from the care team"
      :messages="messages"
      :limit="5"
      @open="(m) => m.unread && clearwater.mark_message_read({ id: m.id, person: m.personId })"
    />
    <StatusWidget title="Test results" :span="8" :items="results" />
    <GalleryWidget :title="photos.album || 'Photos'" :pictures="photos.pictures" />
  </Grid>
</template>
