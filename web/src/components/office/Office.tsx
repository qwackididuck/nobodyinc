"use client";

// The office: every team in its room, every agent at its desk with its
// status, paper planes flying between agents as they write to each other.
//
// The scene itself is React (rooms, desks, props), positioned by
// layoutOffice. What happens is played on top of it by comparing the
// organization before and after each change:
//   - a recruit walks in through the door to its team's room; a quick
//     construction adds its desk (the room grows first, and neighbours move,
//     when it does not fit), then it sits down;
//   - a dismissed agent walks out and its desk is taken down;
//   - a new team gets its room built; a removed team's room is taken down;
//     the plot, an empty lot after the teams, opens the team dialog;
//   - a finished feature: its tickets fly from the director's board to the
//     archives' shelves.
// Rooms can be clicked (not the desks in them): a team's room opens its
// dialog, the director's office its chat, the archives their panel.
// Paper planes are launched by the event stream (message.sent).

import { useEffect, useMemo, useRef, useState } from "react";

import { type Agent, type Feature, type Team } from "@/lib/api";
import { useAppearance } from "@/lib/appearance";
import { useDaemonEvents, useLive } from "@/lib/live";
import { lookOf, teamColors } from "@/lib/look";
import { betweenAgents, isDirector } from "../chat/agents";
import { Banner } from "./Banner";
import { Desk } from "./Desk";
import { endedAt, isEnded } from "../schedules/scheduling";
import { ArchivesPanel } from "./ArchivesPanel";
import { ClockPanel } from "./ClockPanel";
import { FeatureReview } from "./FeatureReview";
import { SpendPanel } from "./SpendPanel";
import { TeamDialog } from "../teams/TeamDialog";
import { addWalker, celebrate, dismantle, fileTickets, flyPlane, site, walk, works } from "./effects";
import { BIN, DOOR, DOOR_STATION, headOf, layoutOffice, type Point, type Room, SCENE_WIDTH, WALL_HEIGHT } from "./layout";
import styles from "./office.module.css";

interface Props {
  selected: string | null; // address of the agent whose chat is open
  binUnread: number;
  onSelectAgent: (address: string) => void;
  onOpenBin: () => void;
  // When the human last looked at the clock (the end of the latest
  // occurrence of a scheduled task they saw), and how to record it.
  clockSeen: string | undefined;
  onClockSeen: (at: string) => void;
}

type Scene =
  | { id: number; kind: "arrive"; agent: Agent }
  | { id: number; kind: "leave"; agent: Agent; at: Point }
  | { id: number; kind: "team-built"; team: string }
  | { id: number; kind: "team-removed"; box: Room }
  | { id: number; kind: "archive"; feature: Feature };

// What the office last showed, to tell what changed.
interface Seen {
  key: string;
  agents: Agent[];
  teams: Team[];
}

function inputOf(agents: Agent[], teams: Team[], hidden: Set<string> = new Set()) {
  const shown = agents.filter((a) => !hidden.has(a.address));
  return {
    director: shown.find(isDirector)?.address ?? null,
    teams: teams.map((t) => ({ id: t.id, members: shown.filter((a) => a.team === t.id).map((a) => a.address) })),
  };
}

const orgKey = (agents: Agent[], teams: Team[]) =>
  `${teams.map((t) => t.id).join(",")}|${agents
    .map((a) => a.address)
    .sort()
    .join(",")}`;

let nextSceneID = 1;

export function Office({ selected, binUnread, onSelectAgent, onOpenBin, clockSeen, onClockSeen }: Props) {
  const { agents, teams, tickets, features, loaded, scheduleRuns } = useLive();
  const appearance = useAppearance();
  const moving = appearance.animations === "full";
  const layer = useRef<HTMLDivElement>(null);

  // Arrivals: "walking" agents are not placed yet (their desk does not exist
  // until they reach their room); "seating" ones have a desk, but their
  // walker has not sat down.
  const [arrivals, setArrivals] = useState<Map<string, "walking" | "seating">>(new Map());
  const [building, setBuilding] = useState<Set<string>>(new Set());
  const [scenes, setScenes] = useState<Scene[]>([]);
  const [seen, setSeen] = useState<Seen | null>(null);
  // The feature under review stays shown after it is finished (and no
  // longer active), so that the panel can say how it went.
  const [reviewing, setReviewing] = useState<Feature | null>(null);
  const [archivesOpen, setArchivesOpen] = useState(false);
  const [creatingTeam, setCreatingTeam] = useState(false);
  const [editingTeam, setEditingTeam] = useState<Team | null>(null);
  const [seenFinished, setSeenFinished] = useState<string | null>(null);
  const [clockOpen, setClockOpen] = useState(false);
  const [spendOpen, setSpendOpen] = useState(false);
  // The occurrences already done when last rendered, and how many times the
  // clock rang: a scheduled task newly done rings it (the alarm replays when
  // the count changes). Nothing rings for what was done before the page
  // loaded.
  const [doneSeen, setDoneSeen] = useState<string | null>(null);
  const [rings, setRings] = useState(0);
  const doneKey = scheduleRuns
    .filter((r) => r.status === "done")
    .map((r) => r.id)
    .join(",");
  if (loaded.scheduleRuns && doneSeen !== doneKey) {
    const before = new Set((doneSeen ?? "").split(","));
    if (doneSeen !== null && doneKey.split(",").some((id) => id && !before.has(id))) setRings((n) => n + 1);
    setDoneSeen(doneKey);
  }
  const clockUnread = scheduleRuns.filter((r) => isEnded(r) && endedAt(r) > (clockSeen ?? "")).length;

  // Compare with what was shown before (React's "adjust state when props
  // change" pattern: no effect, no flash of the new desk).
  const ready = Boolean(loaded.agents && loaded.teams);
  const key = orgKey(agents, teams);
  if (ready && seen?.key !== key) {
    if (seen) {
      const before = new Map(seen.agents.map((a) => [a.address, a]));
      const now = new Set(agents.map((a) => a.address));
      const previous = layoutOffice(inputOf(seen.agents, seen.teams));
      const added = agents.filter((a) => !before.has(a.address));
      const removed = seen.agents.filter((a) => !now.has(a.address));
      const teamIDs = new Set(teams.map((t) => t.id));
      const oldTeamIDs = new Set(seen.teams.map((t) => t.id));
      const newScenes: Scene[] = [
        ...added.map((agent): Scene => ({ id: nextSceneID++, kind: "arrive", agent })),
        ...removed.flatMap((agent): Scene[] => {
          const at = previous.desks.get(agent.address);
          return at ? [{ id: nextSceneID++, kind: "leave", agent, at }] : [];
        }),
        ...teams.filter((t) => !oldTeamIDs.has(t.id)).map((t): Scene => ({ id: nextSceneID++, kind: "team-built", team: t.id })),
        ...previous.rooms
          .filter((r) => r.kind === "team" && !teamIDs.has(r.id))
          .map((box): Scene => ({ id: nextSceneID++, kind: "team-removed", box })),
      ];
      if (added.length) setArrivals((m) => new Map([...m, ...added.map((a) => [a.address, "walking"] as const)]));
      if (newScenes.length) setScenes((s) => [...s, ...newScenes]);
    }
    setSeen({ key, agents, teams });
  }

  // A feature newly finished gets its tickets filed in the archives.
  const finishedKey = features
    .filter((f) => f.status === "finished")
    .map((f) => f.id)
    .join(",");
  if (loaded.features && seenFinished !== finishedKey) {
    if (seenFinished !== null) {
      const before = new Set(seenFinished.split(","));
      const newly = features.filter((f) => f.status === "finished" && !before.has(f.id));
      if (newly.length) setScenes((s) => [...s, ...newly.map((feature): Scene => ({ id: nextSceneID++, kind: "archive", feature }))]);
    }
    setSeenFinished(finishedKey);
  }

  const walking = useMemo(() => new Set([...arrivals].filter(([, phase]) => phase === "walking").map(([a]) => a)), [arrivals]);
  const layout = useMemo(() => layoutOffice(inputOf(agents, teams, walking)), [agents, teams, walking]);
  // Where everyone will sit once every walker has arrived.
  const full = useMemo(() => layoutOffice(inputOf(agents, teams)), [agents, teams]);

  // Effects read the latest layouts without restarting.
  const current = useRef({ layout, full, moving, appearance });
  useEffect(() => {
    current.current = { layout, full, moving, appearance };
  });

  // Play the scenes, once each.
  const started = useRef(new Set<number>());
  useEffect(() => {
    const el = layer.current;
    if (!el) return;
    for (const scene of scenes) {
      if (started.current.has(scene.id)) continue;
      started.current.add(scene.id);
      void play(scene, el).finally(() => setScenes((s) => s.filter((x) => x.id !== scene.id)));
    }

    async function play(scene: Scene, el: HTMLDivElement) {
      const { full: target, moving: motion } = current.current;
      switch (scene.kind) {
        case "arrive": {
          const a = scene.agent;
          const desk = target.desks.get(a.address);
          const room = target.rooms.find((r) => r.desks.some((d) => d.address === a.address));
          if (!desk || !room) {
            setArrivals((m) => without(m, a.address));
            return;
          }
          const walker = addWalker(el, lookOf(a.address, isDirector(a)), DOOR_STATION);
          await walk(walker, [DOOR_STATION, { x: DOOR_STATION.x, y: room.corridorY }, { x: desk.x, y: room.corridorY }], motion);
          setArrivals((m) => new Map(m).set(a.address, "seating"));
          works(el, desk, motion);
          await pause(motion ? 1100 : 0);
          await walk(walker, [
            { x: desk.x, y: room.corridorY },
            { x: desk.x, y: desk.y },
          ], motion);
          walker.remove();
          setArrivals((m) => without(m, a.address));
          celebrate(el, "NEW RECRUIT!", `${a.name} joined ${isDirector(a) ? "as director" : a.team.toUpperCase()}`, { x: room.x + room.width / 2, y: room.y + 40 }, motion);
          return;
        }
        case "leave": {
          const a = scene.agent;
          const walker = addWalker(el, lookOf(a.address, isDirector(a)), scene.at);
          works(el, scene.at, motion);
          const corridor = scene.at.y - 70;
          await walk(walker, [scene.at, { x: scene.at.x, y: corridor }, { x: DOOR_STATION.x, y: corridor }, DOOR_STATION], motion);
          walker.remove();
          return;
        }
        case "team-built": {
          setBuilding((b) => new Set(b).add(scene.team));
          const lot = target.rooms.find((r) => r.id === scene.team);
          if (lot) site(el, lot, 1800, motion);
          await pause(motion ? 2000 : 0);
          setBuilding((b) => {
            const next = new Set(b);
            next.delete(scene.team);
            return next;
          });
          const room = current.current.layout.rooms.find((r) => r.id === scene.team);
          celebrate(el, "NEW TEAM!", `${scene.team.toUpperCase()} has its room`, { x: room ? room.x + room.width / 2 : SCENE_WIDTH / 2, y: (room?.y ?? 200) + 40 }, motion);
          return;
        }
        case "team-removed":
          dismantle(el, scene.box, motion);
          return;
        case "archive":
          await archive(el, scene.feature, motion);
          return;
      }
    }

    // archive files a finished feature's tickets: they fly from the
    // director's board down to the archives' shelves.
    async function archive(el: HTMLDivElement, feature: Feature, motion: boolean) {
      const { layout: now } = current.current;
      const archives = now.rooms.find((r) => r.kind === "archives");
      const office = now.rooms.find((r) => r.kind === "director");
      if (!archives || !office) return;

      await fileTickets(el, { x: office.x + 220, y: office.y + 80 }, { x: archives.x + archives.width / 2 - 17, y: archives.y + 90 }, motion);
      celebrate(el, "ARCHIVED!", `${feature.id} is on the shelves`, { x: archives.x + archives.width / 2, y: archives.y + 40 }, motion);
    }
  }, [scenes]);

  // Paper planes: a message between two agents flies from desk to desk.
  useDaemonEvents((e) => {
    const el = layer.current;
    const { layout: now, appearance: look } = current.current;
    if (e.type !== "message.sent" || !el || look.animations === "off" || !betweenAgents(e.data)) return;
    const from = now.desks.get(e.data.from);
    const to = now.desks.get(e.data.to);
    if (!from || !to) return;
    const m = e.data;
    void flyPlane(el, headOf(from), headOf(to), {
      natural: look.animations === "full",
      label: look.planeLabels ? `${m.from.split("@")[0]} → ${m.to.split("@")[0]}: ${m.subject || m.act}` : null,
      reply: Boolean(m.in_reply_to),
    }).then(() => bounce(el.parentElement, m.to));
  });

  const director = agents.find(isDirector);
  const proposals = features.filter((f) => f.status === "proposed").length;
  const active = features.find((f) => f.active);
  const featureTickets = active ? tickets.filter((t) => t.feature === active.id && t.status !== "canceled") : [];
  const working = new Map(tickets.filter((t) => t.status === "in_progress" && t.assignee).map((t) => [t.assignee as string, t.id]));
  const queue = (team: string) => tickets.filter((t) => t.team === team && t.status === "open" && !t.assignee).length;
  const byAddress = new Map(agents.map((a) => [a.address, a]));

  return (
    <section className={styles.office} aria-label="Office">
      <Banner onOpenDirector={() => director && onSelectAgent(director.address)} onReview={() => setReviewing(active ?? null)} />
      {creatingTeam && <TeamDialog onClose={() => setCreatingTeam(false)} />}
      {editingTeam && <TeamDialog team={editingTeam} onClose={() => setEditingTeam(null)} />}
      {reviewing && <FeatureReview feature={reviewing} onClose={() => setReviewing(null)} />}
      {archivesOpen && <ArchivesPanel onClose={() => setArchivesOpen(false)} onOpenChat={onSelectAgent} />}
      {clockOpen && <ClockPanel onClose={() => setClockOpen(false)} onSeen={onClockSeen} />}
      {spendOpen && <SpendPanel onClose={() => setSpendOpen(false)} />}
      <div className={styles.wrap}>
        <div className={styles.scene} style={{ width: SCENE_WIDTH, height: Math.max(layout.height, full.height) }}>
          <Wall
            binUnread={binUnread}
            onOpenBin={onOpenBin}
            clockUnread={clockUnread}
            rings={moving ? rings : 0}
            onOpenClock={() => setClockOpen(true)}
            onOpenSpend={() => setSpendOpen(true)}
          />
          <div className={styles.floor} style={{ top: WALL_HEIGHT }} />
          {layout.rooms.map((room) =>
            room.kind === "plot" ? (
              <Plot key={room.id} room={room} first={teams.length === 0} onOpen={() => setCreatingTeam(true)} />
            ) : (
            <RoomView
              key={room.id}
              room={room}
              carpet={carpetOf(room, teams)}
              building={building.has(room.id)}
              queue={room.kind === "team" ? queue(room.id) : 0}
              onOpen={() => {
                if (room.kind === "team") setEditingTeam(teams.find((t) => t.id === room.id) ?? null);
                if (room.kind === "archives") setArchivesOpen(true);
                if (room.kind === "director" && director) onSelectAgent(director.address);
              }}
            >
              {room.kind === "archives" && <Shelves count={features.filter((f) => f.status === "finished").length} />}
              {room.kind === "director" && (
                <DirectorProps
                  proposals={proposals}
                  done={featureTickets.filter((t) => t.status === "done").length}
                  total={featureTickets.length}
                />
              )}
            </RoomView>
            ),
          )}
          {layout.rooms.flatMap((room) =>
            room.desks.map(({ address, at }) => {
              const agent = byAddress.get(address);
              if (!agent) return null;
              return (
                <Desk
                  key={address}
                  agent={agent}
                  at={at}
                  ticket={working.get(address)}
                  selected={selected === address}
                  arriving={arrivals.get(address) === "seating"}
                  onSelect={() => onSelectAgent(address)}
                />
              );
            }),
          )}
          <div ref={layer} className={styles.layer} />
        </div>
      </div>
    </section>
  );
}

interface WallProps {
  binUnread: number;
  onOpenBin: () => void;
  // Occurrences of scheduled tasks over since the human last looked.
  clockUnread: number;
  // How many times the clock rang; 0 never animates it.
  rings: number;
  onOpenClock: () => void;
  // The spending board (an easel with a chart) opens the spending popup.
  onOpenSpend: () => void;
}

function Wall({ binUnread, onOpenBin, clockUnread, rings, onOpenClock, onOpenSpend }: WallProps) {
  return (
    <div className={styles.wall} style={{ height: WALL_HEIGHT }}>
      {[60, 200, 620].map((x) => (
        <span key={x} className={styles.window} style={{ left: x }} />
      ))}
      {/* Keyed by the rings: each new one replays the alarm. */}
      <button
        key={rings}
        type="button"
        className={styles.clock}
        data-ringing={rings > 0}
        aria-label="The clock: scheduled tasks done or skipped"
        onClick={onOpenClock}
      >
        {clockUnread > 0 && <span className={styles.binBadge}>{clockUnread}</span>}
      </button>
      <button type="button" className={styles.easel} aria-label="The spending board: what the agents spent, and the budgets" onClick={onOpenSpend}>
        <span className={styles.easelChart} aria-hidden="true">
          $
        </span>
      </button>
      <span className={styles.door} style={{ left: DOOR.x, width: DOOR.width }} />
      <button type="button" className={styles.bin} style={{ left: BIN.x, top: BIN.y }} aria-label="Wastebasket: messages between agents" onClick={onOpenBin}>
        {binUnread > 0 && <span className={styles.binBadge}>{binUnread}</span>}
      </button>
    </div>
  );
}

interface RoomViewProps {
  room: Room;
  carpet?: string;
  building: boolean;
  queue: number;
  // Clicking the room (its floor, frame or sign; desks are above it) opens
  // what it is about.
  onOpen: () => void;
  children?: React.ReactNode;
}

// What clicking a room does, shown when hovering it.
const ROOM_HINT: Record<Room["kind"], string> = {
  team: "Edit the team ▸",
  director: "Open the chat ▸",
  archives: "Open the archives ▸",
  plot: "",
};

function RoomView({ room, carpet, building, queue, onOpen, children }: RoomViewProps) {
  const name = room.kind === "director" ? "director" : room.kind === "archives" ? "the archives" : `team ${room.id}`;
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`${name}: ${ROOM_HINT[room.kind].replace(" ▸", "").toLowerCase()}`}
      className={`${styles.room} ${styles.clickable} ${room.kind === "director" ? styles.rug : ""} ${building ? styles.building : ""}`}
      style={{ left: room.x, top: room.y, width: room.width, height: room.height, background: carpet }}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
    >
      <span className={styles.sign}>
        {room.kind === "director" ? "DIRECTOR" : room.id.toUpperCase()}
        {room.kind === "team" && <em> Q{queue}</em>}
      </span>
      <span className={styles.plant} />
      {children}
      <span className={styles.roomHint} aria-hidden="true">
        {ROOM_HINT[room.kind]}
      </span>
    </div>
  );
}

// The archives' floor: a quiet library green, whatever the teams.
const ARCHIVES_CARPET = "#9fae98";

// carpetOf is a room's floor: each team its colour (the same as in the
// kanban), the archives their own, the director its rug.
function carpetOf(room: Room, teams: Team[]): string | undefined {
  if (room.kind === "team") return teamColors(teams.findIndex((t) => t.id === room.id)).carpet;
  if (room.kind === "archives") return ARCHIVES_CARPET;
  return undefined;
}

// Plot is the empty lot after the teams: a "+" that opens the team dialog.
// While the project has no team, it says so.
function Plot({ room, first, onOpen }: { room: Room; first: boolean; onOpen: () => void }) {
  return (
    <button
      type="button"
      className={`${styles.plot} ${first ? styles.plotFirst : ""}`}
      style={{ left: room.x, top: room.y, width: room.width, height: room.height }}
      onClick={onOpen}
    >
      <span className={styles.plotPlus} aria-hidden="true">
        +
      </span>
      <span className={styles.plotLabel}>{first ? "Create your first team" : "New team"}</span>
      {first && <span className={styles.plotHint}>Then recruit its members from the catalog, or ask the director.</span>}
    </button>
  );
}

// The director's board: proposals waiting for the human and the active
// feature's progress.
function DirectorProps({ proposals, done, total }: { proposals: number; done: number; total: number }) {
  return (
    <span className={styles.board}>
      PROPOSALS
      <br />
      <b>{proposals} waiting</b>
      <br />
      TICKETS {done}/{total}
    </span>
  );
}

function without<K, V>(m: Map<K, V>, key: K): Map<K, V> {
  const next = new Map(m);
  next.delete(key);
  return next;
}

function pause(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

// bounce makes the recipient of a paper plane hop.
function bounce(scene: HTMLElement | null, address: string) {
  const desk = scene?.querySelector<HTMLElement>(`[data-address="${CSS.escape(address)}"]`);
  if (!desk) return;
  desk.classList.remove(styles.got);
  void desk.offsetWidth;
  desk.classList.add(styles.got);
}

// Shelves fill the archives: three bookcases, one box per finished feature
// (filled shelf by shelf), and a few books for the look.
const BOOKCASES = 3;
const SHELVES_PER_CASE = 3;
const BOXES_PER_SHELF = 3;
const BOOK_COLORS = ["#3f6fa5", "#b34a4a", "#4f8a5b", "#c9973f", "#7a5aa0"];

function Shelves({ count }: { count: number }) {
  const perCase = SHELVES_PER_CASE * BOXES_PER_SHELF;
  return (
    <span className={styles.shelves} aria-label={`${count} finished feature${count === 1 ? "" : "s"} in the archives`}>
      {Array.from({ length: BOOKCASES }, (_, c) => (
        <span key={c} className={styles.bookcase}>
          {Array.from({ length: SHELVES_PER_CASE }, (_, row) => {
            const first = c * perCase + row * BOXES_PER_SHELF;
            const boxes = Math.max(0, Math.min(BOXES_PER_SHELF, count - first));
            return (
              <span key={row} className={styles.shelf}>
                {Array.from({ length: boxes }, (_, i) => (
                  <span key={i} className={styles.box} />
                ))}
                {Array.from({ length: BOXES_PER_SHELF - boxes }, (_, i) => (
                  <span key={`b${i}`} className={styles.books} style={{ background: BOOK_COLORS[(c + row + i) % BOOK_COLORS.length] }} />
                ))}
              </span>
            );
          })}
        </span>
      ))}
    </span>
  );
}
