// Helpers for agent and template definitions (docs/agents.md).

import type { AgentDefinition, Template } from "@/lib/api";

// emptyDefinition is the starting point of an agent written from scratch.
export function emptyDefinition(): AgentDefinition {
  return { permissions: { profile: "default" }, tools: ["Read", "Grep", "Glob"] };
}

// toDefinition keeps the file fields of a template (drops its catalog info).
export function toDefinition(t: Template): AgentDefinition {
  return {
    description: t.description,
    version: t.version,
    role: t.role,
    expertise: t.expertise,
    model: t.model,
    permissions: t.permissions,
    tools: t.tools,
    mcp: t.mcp,
    skills: t.skills,
    prompt: t.prompt,
  };
}

// cleanDefinition trims list entries and drops the empty ones before sending.
export function cleanDefinition(d: AgentDefinition): AgentDefinition {
  const clean = (items?: string[]) => (items ?? []).map((s) => s.trim()).filter(Boolean);
  return {
    ...d,
    tools: clean(d.tools),
    mcp: clean(d.mcp),
    skills: clean(d.skills),
    permissions: { ...d.permissions, allow: clean(d.permissions?.allow), deny: clean(d.permissions?.deny) },
  };
}

// A name is also part of an address (name@team): lowercase, digits, dashes.
export const NAME_PATTERN = "[a-z][a-z0-9-]*";
