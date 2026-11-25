// Speech bubbles between teammates: the office's replacement for paper planes
// when two agents of the same team write to each other. What is shown follows
// from the messages alone (no event to miss: a page reloaded shows the same
// bubbles), by these rules:
//
//   - a message between two agents of one team is a bubble above its sender,
//     with the message in it; a new bubble comes under the older ones;
//   - a message that expects an answer (request, query, propose) keeps its
//     bubble up until it is answered, however long that takes: a stuck
//     conversation shows;
//   - one that expects none (inform, agree, refuse, done) lasts LINGER_MS;
//   - an answered message lasts LINGER_MS more after its first answer: the
//     answer is seen next to the question, and when the answer expects none,
//     both go away together, the conversation being over;
//   - ... unless its recipient read it and has since finished its turn
//     (delivered, and idle): the agent acted on it, or dropped it, without
//     answering, and nothing more will come, so the bubble goes away
//     instead of lingering for ever;
//   - a message to or from an agent no longer in the organization leaves no
//     bubble, and a message deleted (the wastebasket) takes its bubble away.
//
// Messages between teams, with the director, the human or nobody keep their
// paper plane (effects.ts).
//
// This file has no React in it, so that its rules can be tried on their own.

import type { Act, Message } from "@/lib/api";

// How long a bubble lingers once nothing more is expected of it.
export const LINGER_MS = 2000;
// How many bubbles an agent shows at once: the most recent ones; the oldest
// of them says how many more there are.
export const MAX_BUBBLES = 3;

// The acts that expect an answer (the system prompt tells agents the same).
const EXPECTS_ANSWER: ReadonlySet<Act> = new Set<Act>(["request", "query", "propose"]);

// teamOfAddress is the team of an agent's address ("dev1@dev" is "dev"); ""
// for an address without a team (the director, the human, nobody).
export function teamOfAddress(address: string): string {
  const at = address.indexOf("@");
  return at < 0 ? "" : address.slice(at + 1);
}

// sameTeam tells whether two different agents belong to one team.
export function sameTeam(a: string, b: string): boolean {
  const team = teamOfAddress(a);
  return a !== b && team !== "" && team === teamOfAddress(b);
}

export interface Speech {
  id: string;
  from: string;
  to: string;
  act: Act;
  subject: string;
  body: string;
  // It answers another message.
  reply: boolean;
  // It expects an answer that has not come.
  waiting: boolean;
  // When it goes away, in milliseconds since the epoch; null: not before it
  // is answered.
  endsAt: number | null;
}

// speechesOf turns messages (oldest first) into the speeches of the agents
// that are in the organization, whatever the time: visibleSpeeches tells
// which are still up.
//
// done holds the addresses of the agents that finished their turn (idle).
export function speechesOf(messages: Message[], present: ReadonlySet<string>, done: ReadonlySet<string>): Speech[] {
  const firstReply = new Map<string, number>();
  for (const m of messages) {
    if (!m.in_reply_to) continue;
    const at = Date.parse(m.sent_at);
    const seen = firstReply.get(m.in_reply_to);
    if (seen === undefined || at < seen) firstReply.set(m.in_reply_to, at);
  }

  const speeches: Speech[] = [];
  for (const m of messages) {
    if (!sameTeam(m.from, m.to) || !present.has(m.from) || !present.has(m.to)) continue;

    // What its end waits for: its answer if it expects one, else itself.
    const expects = EXPECTS_ANSWER.has(m.act);
    let closedAt = expects ? firstReply.get(m.id) : Date.parse(m.sent_at);
    // Read, then the recipient's turn ended with no answer: nothing more is coming.
    if (closedAt === undefined && m.delivered_at && done.has(m.to)) closedAt = Date.parse(m.delivered_at);

    speeches.push({
      id: m.id,
      from: m.from,
      to: m.to,
      act: m.act,
      subject: m.subject,
      body: m.body,
      reply: Boolean(m.in_reply_to),
      waiting: expects && closedAt === undefined,
      endsAt: closedAt === undefined ? null : closedAt + LINGER_MS,
    });
  }

  return speeches;
}

// What an agent says at a given time: its latest bubbles, oldest first (the
// newest is the one nearest its head), and how many older ones are not shown.
export interface Talking {
  bubbles: Speech[];
  more: number;
}

// visibleSpeeches picks the speeches still up at now, by sender.
export function visibleSpeeches(speeches: Speech[], now: number): Map<string, Talking> {
  const up = new Map<string, Speech[]>();
  for (const s of speeches) {
    if (s.endsAt !== null && s.endsAt <= now) continue;
    up.set(s.from, [...(up.get(s.from) ?? []), s]);
  }

  const talking = new Map<string, Talking>();
  for (const [from, list] of up) {
    talking.set(from, { bubbles: list.slice(-MAX_BUBBLES), more: Math.max(0, list.length - MAX_BUBBLES) });
  }

  return talking;
}

// nextEnd is the soonest time after now at which a speech goes away; null
// when none will (all wait for an answer, or none is up).
export function nextEnd(speeches: Speech[], now: number): number | null {
  let soonest: number | null = null;
  for (const s of speeches) {
    if (s.endsAt !== null && s.endsAt > now && (soonest === null || s.endsAt < soonest)) soonest = s.endsAt;
  }

  return soonest;
}

// The longest excerpt of a message's body a bubble holds.
const EXCERPT_CHARS = 160;

// excerpt is the start of a body on one line, cut with an ellipsis.
export function excerpt(body: string): string {
  const line = body.replace(/\s+/g, " ").trim();
  return line.length > EXCERPT_CHARS ? `${line.slice(0, EXCERPT_CHARS).trimEnd()}…` : line;
}
