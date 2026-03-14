import {
  Outlet,
  createRootRoute,
  useRouterState,
} from "@tanstack/react-router"
import { TanStackRouterDevtools } from "@tanstack/react-router-devtools"

import { AppLayout } from "@/components/app-layout"

const RootLayout = () => {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const isOnboard = pathname === "/onboard"

  if (isOnboard) {
    return (
      <>
        <Outlet />
        <TanStackRouterDevtools />
      </>
    )
  }

  return (
    <AppLayout>
      <Outlet />
      <TanStackRouterDevtools />
    </AppLayout>
  )
}

export const Route = createRootRoute({ component: RootLayout })
