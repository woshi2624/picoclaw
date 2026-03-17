import { useAtomValue } from "jotai"
import type { ReactNode } from "react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Toaster } from "sonner"

import { AppHeader } from "@/components/app-header"
import { AppSidebar } from "@/components/app-sidebar"
import { SidebarProvider } from "@/components/ui/sidebar"
import { TooltipProvider } from "@/components/ui/tooltip"
import { gatewayAtom } from "@/store"

export function AppLayout({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const { status } = useAtomValue(gatewayAtom)

  // Show full-screen overlay only when the service is starting on initial page load.
  // We track whether we've received the first real status (anything other than the
  // initial "unknown" placeholder). If the first real status is "starting", show
  // the overlay until the service finishes starting.
  const initialCheckDone = useRef(false)
  const [showOverlay, setShowOverlay] = useState(false)

  useEffect(() => {
    if (status === "unknown") return

    if (!initialCheckDone.current) {
      initialCheckDone.current = true
      if (status === "starting") {
        setShowOverlay(true)
      }
      return
    }

    if (showOverlay && status !== "starting") {
      setShowOverlay(false)
    }
  }, [status, showOverlay])

  return (
    <TooltipProvider>
      <SidebarProvider className="flex h-dvh flex-col overflow-hidden">
        <AppHeader />

        <div className="flex flex-1 overflow-hidden">
          <AppSidebar />
          <div className="flex w-full flex-col overflow-hidden">
            <main className="flex min-h-0 w-full max-w-full flex-1 flex-col overflow-hidden">
              {children}
            </main>
          </div>
        </div>
        <Toaster position="bottom-center" />
      </SidebarProvider>

      {/* Full-screen startup overlay */}
      {showOverlay && (
        <div className="bg-background/90 fixed inset-0 z-[9999] flex flex-col items-center justify-center gap-4 backdrop-blur-sm">
          <div className="border-primary h-12 w-12 animate-spin rounded-full border-4 border-t-transparent" />
          <p className="text-muted-foreground text-sm font-medium">
            {t("header.gateway.status.starting")}
          </p>
        </div>
      )}
    </TooltipProvider>
  )
}
