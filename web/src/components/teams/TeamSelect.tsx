"use client";

import { useState } from "react";

import { useLive } from "@/lib/live";
import ui from "../ui/ui.module.css";
import { TeamDialog } from "./TeamDialog";

// The option that opens the team dialog instead of picking a team.
const NEW_TEAM = "__new_team__";

interface Props {
  id: string;
  value: string;
  onChange: (team: string) => void;
}

// TeamSelect picks the team to recruit into, with a last option to create a
// new team on the spot: once created, it is picked.
export function TeamSelect({ id, value, onChange }: Props) {
  const { teams } = useLive();
  const [creating, setCreating] = useState(false);
  // The team just created may not be in the list yet (it comes with the
  // next org.changed event): keep it as an option meanwhile.
  const ids = teams.map((t) => t.id);
  const options = value && !ids.includes(value) ? [...ids, value] : ids;

  return (
    <>
      <select
        id={id}
        className={ui.select}
        value={value}
        onChange={(e) => (e.target.value === NEW_TEAM ? setCreating(true) : onChange(e.target.value))}
      >
        {options.length === 0 && <option value="">No team yet</option>}
        {options.map((t) => (
          <option key={t} value={t}>
            {t}
          </option>
        ))}
        <option value={NEW_TEAM}>+ New team…</option>
      </select>
      {creating && <TeamDialog onClose={() => setCreating(false)} onDone={(t) => onChange(t.id)} />}
    </>
  );
}
