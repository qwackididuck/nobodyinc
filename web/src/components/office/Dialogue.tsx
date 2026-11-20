"use client";

// The speech bubbles of teammates writing to each other (speech.ts has the
// rules): above an agent's head, replacing its status bubble while it talks.

import { useEffect, useMemo, useState } from "react";

import type { Agent, Message } from "@/lib/api";
import { excerpt, nextEnd, type Speech, speechesOf, type Talking, visibleSpeeches } from "./speech";
import styles from "./office.module.css";

// A little after a bubble is due to go, so that the clock is past it.
const SLACK_MS = 20;
// From this many bubbles, each shows two lines of text instead of three: a
// full stack must fit above the first row's desks, under the scene's top edge.
const DENSE_FROM = 3;

// useSpeeches follows what each agent says: the speeches of the recent
// messages, the clock moving on to the next bubble due to go away.
export function useSpeeches(messages: Message[], agents: Agent[]): Map<string, Talking> {
  const present = useMemo(() => new Set(agents.map((a) => a.address)), [agents]);
  const speeches = useMemo(() => speechesOf(messages, present), [messages, present]);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const end = nextEnd(speeches, now);
    if (end === null) return;
    // The clock may be behind (nothing was due for a while): never wait
    // for a time already gone.
    const timer = setTimeout(() => setNow(Date.now()), Math.max(0, end - Date.now()) + SLACK_MS);
    return () => clearTimeout(timer);
  }, [speeches, now]);

  return useMemo(() => visibleSpeeches(speeches, now), [speeches, now]);
}

interface Props {
  talking: Talking;
  // A new bubble pops in; otherwise it just appears.
  animate: boolean;
}

// Dialogue is an agent's stack of bubbles: the oldest on top, the newest
// nearest its head, with a tail pointing at it. Waiting for an answer shows
// in the border; so does being an answer.
export function Dialogue({ talking, animate }: Props) {
  return (
    <span className={styles.speeches} data-animate={animate} data-dense={talking.bubbles.length >= DENSE_FROM}>
      {talking.bubbles.map((s, i) => (
        <span key={s.id} className={styles.speech} data-waiting={s.waiting} data-reply={s.reply} title={full(s)}>
          {i === 0 && talking.more > 0 && <span className={styles.speechMore}>+{talking.more}</span>}
          <span className={styles.speechAct}>
            {s.act} → {s.to.split("@")[0]}
          </span>
          <span className={styles.speechText}>
            {s.subject && <b>{s.subject} </b>}
            {excerpt(s.body)}
          </span>
        </span>
      ))}
    </span>
  );
}

// full is a bubble's whole message, shown when hovering it.
function full(s: Speech): string {
  return `${s.act} → ${s.to}\n${s.subject}\n\n${s.body}`;
}
