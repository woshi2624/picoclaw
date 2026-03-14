import {
  IconDownload,
  IconPackage,
  IconSearch,
  IconTrash,
  IconLoader2,
  IconAlertTriangle,
  IconRefresh,
  IconArrowUp,
} from "@tabler/icons-react"
import { createFileRoute } from "@tanstack/react-router"
import { useState, useCallback, useMemo } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import {
  getInstalledSkills,
  searchSkills,
  installSkill,
  uninstallSkill,
  checkUpgrades,
  upgradeSkill,
  upgradeAllSkills,
} from "@/api/skills"
import type { SkillInfo, SearchResult, UpgradeResult } from "@/api/skills"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"

export const Route = createFileRoute("/skills")({
  component: SkillsPage,
})

function SkillsPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [searchQuery, setSearchQuery] = useState("")
  const [searchInput, setSearchInput] = useState("")
  const [uninstallTarget, setUninstallTarget] = useState<string | null>(null)
  const [upgradeResults, setUpgradeResults] = useState<UpgradeResult[]>([])
  const [isCheckingUpgrades, setIsCheckingUpgrades] = useState(false)

  // Fetch installed skills
  const {
    data: installedData,
    isLoading: isLoadingInstalled,
    error: installedError,
  } = useQuery({
    queryKey: ["skills", "installed"],
    queryFn: getInstalledSkills,
  })

  // Search skills from registry
  const {
    data: searchData,
    isLoading: isSearching,
    error: searchError,
  } = useQuery({
    queryKey: ["skills", "search", searchQuery],
    queryFn: () => searchSkills(searchQuery, 10),
    enabled: searchQuery.length > 0,
  })

  // Install mutation
  const installMutation = useMutation({
    mutationFn: ({ slug, registry }: { slug: string; registry: string }) =>
      installSkill(slug, registry),
    onSuccess: (data) => {
      toast.success(
        t("skills.installSuccess", {
          name: data.slug,
          version: data.version,
        }),
      )
      if (data.warning) {
        toast.warning(data.warning)
      }
      queryClient.invalidateQueries({ queryKey: ["skills", "installed"] })
    },
    onError: (err: Error) => {
      toast.error(t("skills.installError") + ": " + err.message)
    },
  })

  // Uninstall mutation
  const uninstallMutation = useMutation({
    mutationFn: (name: string) => uninstallSkill(name),
    onSuccess: (data) => {
      toast.success(t("skills.uninstallSuccess", { name: data.name }))
      queryClient.invalidateQueries({ queryKey: ["skills", "installed"] })
      setUninstallTarget(null)
    },
    onError: (err: Error) => {
      toast.error(t("skills.uninstallError") + ": " + err.message)
      setUninstallTarget(null)
    },
  })

  // Check for upgrades
  const handleCheckUpgrades = useCallback(async () => {
    setIsCheckingUpgrades(true)
    try {
      const data = await checkUpgrades()
      setUpgradeResults(data.results)
      const available = data.results.filter((r) => r.status === "upgrade_available")
      if (available.length > 0) {
        toast.success(t("skills.upgradesAvailable", { count: available.length }))
      } else {
        toast.success(t("skills.allUpToDate"))
      }
    } catch (err) {
      toast.error(t("skills.checkUpgradeError") + ": " + (err as Error).message)
    } finally {
      setIsCheckingUpgrades(false)
    }
  }, [t])

  // Upgrade single skill
  const upgradeMutation = useMutation({
    mutationFn: (slug: string) => upgradeSkill(slug),
    onSuccess: (data) => {
      const result = data.results[0]
      if (result?.status === "upgraded") {
        toast.success(t("skills.upgradeSuccess", { name: result.slug, version: result.new_version }))
        setUpgradeResults((prev) => prev.filter((r) => r.slug !== result.slug))
        queryClient.invalidateQueries({ queryKey: ["skills", "installed"] })
      } else if (result?.status === "failed") {
        toast.error(t("skills.upgradeError") + ": " + result.error)
      }
    },
    onError: (err: Error) => {
      toast.error(t("skills.upgradeError") + ": " + err.message)
    },
  })

  // Upgrade all skills
  const upgradeAllMutation = useMutation({
    mutationFn: () => upgradeAllSkills(),
    onSuccess: (data) => {
      const upgraded = data.results.filter((r) => r.status === "upgraded")
      if (upgraded.length > 0) {
        toast.success(t("skills.upgradeAllSuccess", { count: upgraded.length }))
        setUpgradeResults([])
        queryClient.invalidateQueries({ queryKey: ["skills", "installed"] })
      } else {
        toast.success(t("skills.allUpToDate"))
      }
    },
    onError: (err: Error) => {
      toast.error(t("skills.upgradeError") + ": " + err.message)
    },
  })

  const handleSearch = useCallback(() => {
    const trimmed = searchInput.trim()
    if (trimmed) {
      setSearchQuery(trimmed)
    }
  }, [searchInput])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter") handleSearch()
    },
    [handleSearch],
  )

  const installedSkills = useMemo(
    () => installedData?.skills ?? [],
    [installedData],
  )
  const searchResults = useMemo(
    () => searchData?.results ?? [],
    [searchData],
  )

  // Map of slug -> upgrade result for quick lookup.
  const upgradeMap = useMemo(() => {
    const map = new Map<string, UpgradeResult>()
    for (const r of upgradeResults) {
      if (r.status === "upgrade_available") {
        map.set(r.slug, r)
      }
    }
    return map
  }, [upgradeResults])

  // Check if a skill is already installed
  const isInstalled = useCallback(
    (slug: string) => {
      return installedSkills.some(
        (s: SkillInfo) =>
          s.name === slug || s.name.toLowerCase() === slug.toLowerCase(),
      )
    },
    [installedSkills],
  )

  const sourceLabel = (source: string) => {
    switch (source) {
      case "workspace":
        return t("skills.source.workspace")
      case "global":
        return t("skills.source.global")
      case "builtin":
        return t("skills.source.builtin")
      default:
        return source
    }
  }

  const sourceBadgeVariant = (source: string) => {
    switch (source) {
      case "builtin":
        return "secondary" as const
      case "global":
        return "default" as const
      default:
        return "outline" as const
    }
  }

  return (
    <div className="mx-auto max-w-4xl space-y-8 p-6">
      {/* Page header */}
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">
          {t("skills.title")}
        </h1>
        <p className="text-muted-foreground mt-1 text-sm">
          {t("skills.description")}
        </p>
      </div>

      {/* Installed Skills */}
      <section>
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-medium">
            {t("skills.installed")} ({installedSkills.length})
          </h2>
          <div className="flex gap-2">
            {upgradeMap.size > 0 && (
              <Button
                size="sm"
                variant="default"
                onClick={() => upgradeAllMutation.mutate()}
                disabled={upgradeAllMutation.isPending}
              >
                {upgradeAllMutation.isPending ? (
                  <IconLoader2 className="mr-1 size-3.5 animate-spin" />
                ) : (
                  <IconArrowUp className="mr-1 size-3.5" />
                )}
                {t("skills.upgradeAll")} ({upgradeMap.size})
              </Button>
            )}
            <Button
              size="sm"
              variant="outline"
              onClick={handleCheckUpgrades}
              disabled={isCheckingUpgrades}
            >
              {isCheckingUpgrades ? (
                <IconLoader2 className="mr-1 size-3.5 animate-spin" />
              ) : (
                <IconRefresh className="mr-1 size-3.5" />
              )}
              {t("skills.checkUpgrades")}
            </Button>
          </div>
        </div>

        {isLoadingInstalled && (
          <div className="text-muted-foreground flex items-center gap-2 py-8 text-sm">
            <IconLoader2 className="size-4 animate-spin" />
            {t("labels.loading")}
          </div>
        )}

        {installedError && (
          <div className="text-destructive flex items-center gap-2 py-4 text-sm">
            <IconAlertTriangle className="size-4" />
            {t("skills.loadError")}
          </div>
        )}

        {!isLoadingInstalled && installedSkills.length === 0 && (
          <p className="text-muted-foreground py-4 text-sm">
            {t("skills.noInstalled")}
          </p>
        )}

        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          {installedSkills.map((skill: SkillInfo) => (
            <Card key={skill.name + skill.source} className="group relative">
              <CardHeader className="pb-2">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <IconPackage className="text-muted-foreground size-4" />
                    <CardTitle className="text-sm font-medium">
                      {skill.name}
                    </CardTitle>
                    {skill.version && (
                      <Badge variant="outline" className="text-xs">
                        v{skill.version}
                      </Badge>
                    )}
                  </div>
                  <Badge variant={sourceBadgeVariant(skill.source)}>
                    {sourceLabel(skill.source)}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="pb-3">
                <CardDescription className="line-clamp-2 text-xs">
                  {skill.description || t("skills.noDescription")}
                </CardDescription>
                {skill.source === "workspace" && (
                  <div className="mt-2 flex items-center justify-end gap-2">
                    {upgradeMap.has(skill.name) && (
                      <Button
                        variant="default"
                        size="sm"
                        className="h-7 px-2 text-xs"
                        onClick={() => upgradeMutation.mutate(skill.name)}
                        disabled={upgradeMutation.isPending}
                      >
                        {upgradeMutation.isPending &&
                        upgradeMutation.variables === skill.name ? (
                          <IconLoader2 className="mr-1 size-3.5 animate-spin" />
                        ) : (
                          <IconArrowUp className="mr-1 size-3.5" />
                        )}
                        {t("skills.upgrade")} → v{upgradeMap.get(skill.name)?.new_version}
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="sm"
                      className="text-destructive hover:text-destructive h-7 px-2 text-xs"
                      onClick={() => setUninstallTarget(skill.name)}
                      disabled={uninstallMutation.isPending}
                    >
                      <IconTrash className="mr-1 size-3.5" />
                      {t("skills.uninstall")}
                    </Button>
                  </div>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      </section>

      {/* Search & Install */}
      <section>
        <h2 className="mb-4 text-lg font-medium">
          {t("skills.searchAndInstall")}
        </h2>

        <div className="mb-4 flex gap-2">
          <Input
            placeholder={t("skills.searchPlaceholder")}
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={handleKeyDown}
            className="flex-1"
          />
          <Button
            onClick={handleSearch}
            disabled={!searchInput.trim() || isSearching}
          >
            {isSearching ? (
              <IconLoader2 className="mr-1 size-4 animate-spin" />
            ) : (
              <IconSearch className="mr-1 size-4" />
            )}
            {t("skills.search")}
          </Button>
        </div>

        {searchError && (
          <div className="text-destructive flex items-center gap-2 py-4 text-sm">
            <IconAlertTriangle className="size-4" />
            {t("skills.searchError")}
          </div>
        )}

        {searchQuery && !isSearching && searchResults.length === 0 && (
          <p className="text-muted-foreground py-4 text-sm">
            {t("skills.noResults")}
          </p>
        )}

        <div className="space-y-2">
          {searchResults.map((result: SearchResult) => {
            const alreadyInstalled = isInstalled(result.slug)
            return (
              <Card key={result.slug + result.registry_name}>
                <CardContent className="flex items-center justify-between py-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">
                        {result.display_name || result.slug}
                      </span>
                      {result.version && (
                        <Badge variant="outline" className="text-xs">
                          v{result.version}
                        </Badge>
                      )}
                      <Badge variant="secondary" className="text-xs">
                        {result.registry_name}
                      </Badge>
                    </div>
                    <p className="text-muted-foreground mt-0.5 line-clamp-1 text-xs">
                      {result.summary}
                    </p>
                  </div>
                  <Button
                    size="sm"
                    variant={alreadyInstalled ? "outline" : "default"}
                    className="ml-3 shrink-0"
                    disabled={
                      alreadyInstalled || installMutation.isPending
                    }
                    onClick={() =>
                      installMutation.mutate({
                        slug: result.slug,
                        registry: result.registry_name,
                      })
                    }
                  >
                    {installMutation.isPending &&
                    installMutation.variables?.slug === result.slug ? (
                      <IconLoader2 className="mr-1 size-3.5 animate-spin" />
                    ) : (
                      <IconDownload className="mr-1 size-3.5" />
                    )}
                    {alreadyInstalled
                      ? t("skills.alreadyInstalled")
                      : t("skills.install")}
                  </Button>
                </CardContent>
              </Card>
            )
          })}
        </div>
      </section>

      {/* Uninstall confirmation dialog */}
      <AlertDialog
        open={!!uninstallTarget}
        onOpenChange={(open) => !open && setUninstallTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("skills.uninstallConfirm.title")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("skills.uninstallConfirm.description", {
                name: uninstallTarget,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                uninstallTarget && uninstallMutation.mutate(uninstallTarget)
              }
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("skills.uninstall")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
