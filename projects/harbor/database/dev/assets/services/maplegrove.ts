// Maple Grove's parent portal has no API: its pages are rendered on the
// server, so these tools fetch them and read their tables. Signing in goes
// through the school district's SAML sign-in on sso.maplegrove.localhost,
// so a page asked for without a session redirects there; fetch cannot
// follow a redirect to another site here, so the tools ask not to, and
// treat the redirect as signed out.
//
// Grades are not here: the portal shows them in a frame from GradeBook Pro,
// another company's site, which the portal's page cannot read.
import { defineService } from "harbor"

const origin = "https://maplegrove.localhost:8443"

async function page(path: string) {
  const response = await fetch(path, { credentials: "include", redirect: "manual" })
  if (response.type === "opaqueredirect") {
    throw new Error("signed out of Maple Grove Schools; sign in again")
  }
  if (response.status === 404) {
    return null
  }
  if (!response.ok) {
    throw new Error(`${path} answered HTTP ${response.status}`)
  }
  return new DOMParser().parseFromString(await response.text(), "text/html")
}

function text(node: Element | null | undefined) {
  return node?.textContent?.replace(/\s+/g, " ").trim() ?? ""
}

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

// isoDate turns "Oct 5, 2026" into "2026-10-05".
function isoDate(shown: string) {
  const m = /^(\w{3}) (\d{1,2}), (\d{4})$/.exec(shown)
  if (!m || !months.includes(m[1])) {
    throw new Error(`cannot read the date ${shown}`)
  }
  return `${m[3]}-${String(months.indexOf(m[1]) + 1).padStart(2, "0")}-${m[2].padStart(2, "0")}`
}

function rows(doc: Document, table: Element | null = doc.querySelector("table")) {
  return Array.from(table?.querySelectorAll("tr") ?? [])
    .filter((row) => row.querySelector("td"))
    .map((row) => Array.from(row.querySelectorAll("td")).map((cell) => text(cell)))
}

async function students() {
  const doc = await page("/students")
  return Array.from(doc!.querySelectorAll(".card")).map((card) => {
    const link = card.querySelector("a")
    // "Grade 4 · Maple Grove Elementary · Ms. Hannah Cole, Room 12"
    const [grade, school, room] = text(card.querySelector("p")).split(" · ")
    const [teacher, homeroom] = room.split(", ")
    return {
      id: link?.getAttribute("href")?.split("/").pop() ?? "",
      name: text(link),
      grade: Number(grade.replace("Grade ", "")),
      school,
      teacher,
      homeroom,
    }
  })
}

export default defineService({
  name: "maplegrove",
  title: "Maple Grove Schools",
  description: "Your children, their assignments, and school events, from Maple Grove Schools' parent portal.",
  url: origin + "/students",

  login: {
    url: "/students",
    check: async () => {
      // Signed out, the tab ends up on the district's sign-in page, where
      // asking the portal is not possible.
      if (location.origin !== origin) {
        return false
      }
      const response = await fetch("/students", { credentials: "include", redirect: "manual" })
      if (response.type === "opaqueredirect") {
        return false
      }
      if (!response.ok) {
        throw new Error("cannot tell yet whether you are signed in")
      }
      return true
    },
  },

  tools: {
    list_students: {
      description: "List your children at Maple Grove schools.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["students"],
        properties: {
          students: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "name", "grade", "school", "teacher", "homeroom"],
              properties: {
                id: { type: "string", description: "Use it with list_assignments." },
                name: { type: "string" },
                grade: { type: "integer" },
                school: { type: "string" },
                teacher: { type: "string" },
                homeroom: { type: "string" },
              },
            },
          },
        },
      },
      run: async () => ({ students: await students() }),
    },

    list_assignments: {
      description: "List a child's assignments, by due date, with whether each is assigned, submitted, missing, or graded.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: {
          student: { type: "string", description: "Student id from list_students, such as s-sam. Defaults to every child." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["assignments"],
        properties: {
          assignments: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["student", "due", "course", "title", "status"],
              properties: {
                student: { type: "string", description: "The child's name." },
                due: { type: "string" },
                course: { type: "string" },
                title: { type: "string" },
                status: { type: "string" },
              },
            },
          },
        },
      },
      run: async (input) => {
        const ids = input.student ? [input.student] : (await students()).map((s) => s.id)
        const assignments = []
        for (const id of ids) {
          const doc = await page(`/students/${encodeURIComponent(id)}`)
          if (!doc) {
            throw new Error(`there is no student ${id}; list_students has the ids`)
          }
          const name = text(doc.querySelector("h1"))
          for (const [due, course, title, status] of rows(doc)) {
            assignments.push({ student: name, due: isoDate(due), course, title, status })
          }
        }
        return { assignments }
      },
    },

    list_events: {
      description: "List school events and days off, by date.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["events"],
        properties: {
          events: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["date", "title", "school"],
              properties: {
                date: { type: "string" },
                title: { type: "string" },
                school: { type: "string", description: "Which school, or All schools." },
              },
            },
          },
        },
      },
      run: async () => {
        const doc = await page("/events")
        return { events: rows(doc!).map(([date, title, school]) => ({ date: isoDate(date), title, school })) }
      },
    },
  },
})
