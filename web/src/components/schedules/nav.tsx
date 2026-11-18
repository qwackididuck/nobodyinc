"use client";

// Opening the Schedules tab from elsewhere in the control room: the
// director's proposal card (Edit), the office clock (an occurrence).

import { createContext, useContext } from "react";

// What the Schedules tab should show once opened.
export type ScheduleFocus = { kind: "schedule"; id: string } | { kind: "proposal"; id: string };

export const ScheduleNav = createContext<(focus?: ScheduleFocus) => void>(() => {});

// useOpenSchedules returns a function opening the Schedules tab, focused on
// a schedule or a proposal if given.
export function useOpenSchedules(): (focus?: ScheduleFocus) => void {
  return useContext(ScheduleNav);
}
