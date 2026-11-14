import { computed, ref } from "vue";
import { sql, useQuery } from "@desktop";
import { Tools } from "@backend";

/** A service, and the logins Harbor last found you are not through. */
export interface ServiceStatus {
  id: number;
  name: string;
  /** The service's name, while its file cannot be read. */
  title: string;
  /** Empty while its file cannot be read. */
  url: string;
  /** Why its file cannot be read, or "". */
  error: string;
  /** Empty when Harbor has not found you signed out. */
  needed: { tool: string; heading: string }[];
}

/** Every service and what it needs from you, kept current. */
export function useServices() {
  const rows = useQuery<{
    id: number;
    name: string;
    title: string;
    url: string;
    error: string;
    tool: string | null;
    heading: string | null;
  }>(
    () => sql`
      SELECT s.id, s.name, coalesce(json_extract(s.manifest, '$.title'), s.name) AS title,
        coalesce(json_extract(s.manifest, '$.url'), '') AS url, coalesce(s.manifest_error, '') AS error, n.tool, n.heading
      FROM services s LEFT JOIN sign_ins_needed n ON n.service = s.name
      ORDER BY title COLLATE NOCASE, n.tool`,
  );
  const services = computed(() => {
    const byID = new Map<number, ServiceStatus>();
    for (const r of rows.value) {
      const s = byID.get(r.id) ?? { id: r.id, name: r.name, title: r.title, url: r.url, error: r.error, needed: [] };
      if (r.heading) {
        s.needed.push({ tool: r.tool ?? "", heading: r.heading });
      }
      byID.set(r.id, s);
    }
    return [...byID.values()];
  });
  const needYou = computed(() => services.value.filter((s) => s.needed.length > 0));
  return { services, needYou };
}

// The sign-in under way, one at a time, as service/tool, and why the last failed.
export const signingIn = ref("");
export const signInProblem = ref("");

/** Signs you in to a service, or a tool's login: Harbor shows the login page if it must. */
export async function signIn(service: string, tool = "") {
  signingIn.value = `${service}/${tool}`;
  signInProblem.value = "";
  try {
    return await Tools.SignIn(service, tool);
  } catch (e) {
    signInProblem.value = e instanceof Error ? e.message : String(e);
    return false;
  } finally {
    signingIn.value = "";
  }
}
