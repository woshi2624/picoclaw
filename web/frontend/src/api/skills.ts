// API client for skill management.

export interface SkillInfo {
  name: string
  path: string
  source: string // "workspace" | "global" | "builtin"
  description: string
  version?: string
}

export interface SearchResult {
  score: number
  slug: string
  display_name: string
  summary: string
  version: string
  registry_name: string
}

interface SkillsListResponse {
  skills: SkillInfo[]
  total: number
}

interface SkillsSearchResponse {
  results: SearchResult[]
  total: number
  query: string
}

interface SkillInstallResponse {
  status: string
  slug: string
  version: string
  path: string
  warning?: string
  summary?: string
}

interface SkillUninstallResponse {
  status: string
  name: string
}

const BASE_URL = ""

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, options)
  if (!res.ok) {
    const body = await res.text()
    let message = `API error: ${res.status} ${res.statusText}`
    try {
      const json = JSON.parse(body)
      if (json.error) message = json.error
    } catch {
      // use default message
    }
    throw new Error(message)
  }
  return res.json() as Promise<T>
}

export async function getInstalledSkills(): Promise<SkillsListResponse> {
  return request<SkillsListResponse>("/api/skills")
}

export async function searchSkills(
  query: string,
  limit?: number,
): Promise<SkillsSearchResponse> {
  const params = new URLSearchParams({ q: query })
  if (limit) params.set("limit", String(limit))
  return request<SkillsSearchResponse>(`/api/skills/search?${params}`)
}

export async function installSkill(
  slug: string,
  registry: string,
  version?: string,
  force?: boolean,
): Promise<SkillInstallResponse> {
  return request<SkillInstallResponse>("/api/skills/install", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ slug, registry, version, force }),
  })
}

export async function uninstallSkill(
  name: string,
): Promise<SkillUninstallResponse> {
  return request<SkillUninstallResponse>(`/api/skills/${encodeURIComponent(name)}`, {
    method: "DELETE",
  })
}

// --- Upgrade API ---

export interface UpgradeResult {
  slug: string
  old_version: string
  new_version: string
  status: string // "upgraded" | "up_to_date" | "failed" | "upgrade_available"
  error?: string
}

interface UpgradeResponse {
  results: UpgradeResult[]
  total: number
}

export async function checkUpgrades(): Promise<UpgradeResponse> {
  return request<UpgradeResponse>("/api/skills/upgrade", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ slug: "", dry_run: true }),
  })
}

export async function upgradeSkill(slug: string): Promise<UpgradeResponse> {
  return request<UpgradeResponse>("/api/skills/upgrade", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ slug, dry_run: false }),
  })
}

export async function upgradeAllSkills(): Promise<UpgradeResponse> {
  return request<UpgradeResponse>("/api/skills/upgrade", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ slug: "", dry_run: false }),
  })
}

export type { SkillsListResponse, SkillsSearchResponse, SkillInstallResponse, UpgradeResponse }
