import * as React from "react"
import { cn } from "cn"
import { NavLink } from "react-router"
import { Tabs as TabsPrimitive } from "radix-ui"

// Slate: pestanas con subrayado. Activa = texto `text` semibold + barra de 2px en `text` (igual que el nav activo,
// nunca `brand`); inactivas `text-muted`.
const LIST = "inline-flex w-full items-center gap-1 border-b border-border"
const TRIGGER = "relative inline-flex h-8 items-center justify-center gap-1.5 rounded-md px-2 py-1 text-sm whitespace-nowrap text-text-muted transition-colors outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 data-[state=active]:font-semibold data-[state=active]:text-text data-[state=active]:after:absolute data-[state=active]:after:inset-x-0 data-[state=active]:after:-bottom-px data-[state=active]:after:h-0.5 data-[state=active]:after:bg-text [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4"

function Tabs({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      className={cn("flex flex-col gap-3", className)}
      {...props}
    />
  )
}

function TabsList({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn(LIST, className)}
      {...props}
    />
  )
}

function TabsTrigger({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        TRIGGER,
        className
      )}
      {...props}
    />
  )
}

function TabsContent({
  className,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn("flex-1 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring", className)}
      {...props}
    />
  )
}

/**
 * Barra de pestañas que navega entre sub-rutas (misma forma que `TabsList`/`TabsTrigger`): la activa se marca con
 * `aria-current="page"` (NavLink) en vez de `data-state`.
 */
function TabsNav({ className, ...props }: React.ComponentProps<"nav">) {
  return <nav data-slot="tabs-list" className={cn(LIST, "overflow-x-auto", className)} {...props} />
}

function TabsNavLink({ className, ...props }: Omit<React.ComponentProps<typeof NavLink>, "className"> & { className?: string }) {
  return (
    <NavLink
      data-slot="tabs-trigger"
      className={cn(TRIGGER.replaceAll("data-[state=active]", "aria-[current=page]"), className)}
      {...props}
    />
  )
}

export { Tabs, TabsList, TabsTrigger, TabsContent, TabsNav, TabsNavLink }
