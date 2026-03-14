import { createFileRoute, isRedirect, redirect } from "@tanstack/react-router"

import { ChatPage } from "@/components/chat/chat-page"
import { getOnboardStatus } from "@/api/onboard"

export const Route = createFileRoute("/")({
  beforeLoad: async () => {
    try {
      const status = await getOnboardStatus()
      if (status.needs_onboard) {
        throw redirect({ to: "/onboard" })
      }
    } catch (err) {
      if (isRedirect(err)) throw err
      // If API fails (e.g. network error), let the user through
    }
  },
  component: ChatPage,
})
