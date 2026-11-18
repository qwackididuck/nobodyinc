"use client";

import { useEffect, useState } from "react";

import { listSkills, type Skill } from "@/lib/api";
import ui from "../ui/ui.module.css";
import styles from "./staff.module.css";

// What an agent does with a skill.
type Choice = "available" | "preload" | "excluded";

interface Props {
  // The agent's skills (preloaded) and skills_exclude.
  preload: string[];
  exclude: string[];
  onChange: (preload: string[], exclude: string[]) => void;
}

// SkillChoices sets, for each skill every agent gets (ADR-0011), whether
// this agent simply has it, preloads it (its instructions are handed over at
// every session start) or does not get it. Built-in skills can be excluded,
// not preloaded: there is no file to hand over. Names absent from this
// machine are kept and shown as such: another developer may have them.
export function SkillChoices({ preload, exclude, onChange }: Props) {
  const [skills, setSkills] = useState<Skill[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    listSkills()
      .then((l) => !cancelled && setSkills(l.skills.filter((s) => !s.shadowed)))
      .catch(() => !cancelled && setSkills([]));
    return () => {
      cancelled = true;
    };
  }, []);

  const choiceOf = (name: string): Choice => (preload.includes(name) ? "preload" : exclude.includes(name) ? "excluded" : "available");

  function choose(name: string, choice: Choice) {
    const without = (list: string[]) => list.filter((n) => n !== name);
    onChange(choice === "preload" ? [...without(preload), name] : without(preload), choice === "excluded" ? [...without(exclude), name] : without(exclude));
  }

  const names = new Set(skills?.map((s) => s.name));
  const elsewhere = [...preload, ...exclude].filter((n) => !names.has(n));
  const group = (scope: Skill["scope"]) => (skills ?? []).filter((s) => s.scope === scope);
  const row = (s: Skill) => <SkillRow key={`${s.scope}/${s.name}`} skill={s} choice={choiceOf(s.name)} onChoose={(c) => choose(s.name, c)} />;
  const builtin = group("builtin");
  const builtinExcluded = builtin.filter((s) => exclude.includes(s.name)).length;

  return (
    <div className={`${ui.label} ${styles.wide}`}>
      SKILLS
      <span className={styles.hint}>Every agent gets every skill. Preload what this one always needs; exclude what it must not use.</span>
      <div className={styles.mcpServers}>
        {group("project").length > 0 && <span className={styles.hint}>Project</span>}
        {group("project").map(row)}
        {group("personal").length > 0 && <span className={styles.hint}>Personal · all your projects</span>}
        {group("personal").map(row)}
        {skills && group("project").length + group("personal").length === 0 && (
          <span className={styles.hint}>No project or personal skill: create one in Settings → Skills.</span>
        )}
        {builtin.length > 0 && (
          <details>
            <summary className={styles.hint}>
              Built-in ({builtin.length}){builtinExcluded > 0 && ` · ${builtinExcluded} excluded`}
            </summary>
            <div className={styles.mcpServers}>{builtin.map(row)}</div>
          </details>
        )}
      </div>
      {skills && elsewhere.length > 0 && (
        <div className={styles.chips}>
          <span className={styles.hint}>Also listed, for skills absent from this machine:</span>
          {elsewhere.map((n) => (
            <button key={n} type="button" className={`${ui.chip} ${ui.chipOn}`} title="Click to drop it" onClick={() => choose(n, "available")}>
              {preload.includes(n) ? "preload" : "exclude"} {n} ✕
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// SkillRow is one skill and its three-way choice.
function SkillRow({ skill, choice, onChoose }: { skill: Skill; choice: Choice; onChoose: (c: Choice) => void }) {
  const choices: { value: Choice; label: string }[] = [
    { value: "available", label: "Available" },
    ...(skill.scope === "builtin" ? [] : [{ value: "preload" as const, label: "Preload" }]),
    { value: "excluded", label: "Excluded" },
  ];

  return (
    <div className={styles.skillRow}>
      <strong>
        {skill.name}
        {skill.has_hooks && (
          <span className={styles.skillHooks} title="Invoking it asks for your approval each time, then its hooks run.">
            hooks
          </span>
        )}
      </strong>
      <span className={styles.hint} title={skill.description}>
        {skill.description}
      </span>
      <div className={ui.seg} role="group" aria-label={`${skill.name}: available, preloaded or excluded`}>
        {choices.map((c) => (
          <button key={c.value} type="button" aria-pressed={choice === c.value} onClick={() => onChoose(c.value)}>
            {c.label}
          </button>
        ))}
      </div>
    </div>
  );
}
