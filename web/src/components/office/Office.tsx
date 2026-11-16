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
//   - a finished feature: the librarian fetches its tickets from the
//     director's office and files them in the archives.
// Paper planes are launched by the event stream (message.sent).

import { useEffect, useMemo, useRef, useState } from "react";

import { type Agent, type Feature, type Team } from "@/lib/api";
import { useAppearance } from "@/lib/appearance";
import { useDaemonEvents, useLive } from "@/lib/live";
import { lookOf, teamColors } from "@/lib/look";
import { betweenAgents, isDirector } from "../chat/agents";
import { Banner } from "./Banner";
import { Desk } from "./Desk";
import { Sprite } from "./sprite";
import { ArchivesPanel } from "./ArchivesPanel";
import { FeatureReview } from "./FeatureReview";
import { addWalker, celebrate, dismantle, flyPlane, walk, works } from "./effects";
import { BIN, DOOR, DOOR_STATION, headOf, layoutOffice, LIBRARIAN, type Point, type Room, SCENE_WIDTH, WALL_HEIGHT } from "./layout";
import styles from "./office.module.css";

interface Props {
  selected: string | null; // address of the agent whose chat is open
  binUnread: number;
  onSelectAgent: (address: string) => void;
  onOpenBin: () => void;
  onHireDirector: () => void;
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

export function Office({ selected, binUnread, onSelectAgent, onOpenBin, onHireDirector }: Props) {
  const { agents, teams, tickets, features, loaded } = useLive();
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
  // While the librarian is out fetching tickets, its chair is empty.
  const [librarianAway, setLibrarianAway] = useState(false);
  const [seenFinished, setSeenFinished] = useState<string | null>(null);

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

  // A feature newly finished sends the librarian to file its tickets.
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
          celebrate(el, "NEW RECRUIT!", `${a.name} joined ${isDirector(a) ? "as director" : a.team.toUpperCase()}`, { x: SCENE_WIDTH / 2, y: room.y + 40 }, motion);
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
          await pause(motion ? 2000 : 0);
          setBuilding((b) => {
            const next = new Set(b);
            next.delete(scene.team);
            return next;
          });
          const room = current.current.layout.rooms.find((r) => r.id === scene.team);
          celebrate(el, "NEW TEAM!", `${scene.team.toUpperCase()} has its room`, { x: SCENE_WIDTH / 2, y: (room?.y ?? 200) + 40 }, motion);
          return;
        }
        case "team-removed":
          dismantle(el, scene.box, motion);
          return;
        case "archive":
          await fileTickets(el, scene.feature, motion);
          return;
      }
    }

    // fileTickets plays the librarian fetching a finished feature's tickets
    // from the director's office and bringing them back to the archives.
    async function fileTickets(el: HTMLDivElement, feature: Feature, motion: boolean) {
      const { layout: now } = current.current;
      const desk = now.desks.get(LIBRARIAN);
      const archives = now.rooms.find((r) => r.kind === "archives");
      const office = now.rooms.find((r) => r.kind === "director");
      if (!desk || !archives || !office) return;

      const pileAt = { x: office.x + 190, y: office.y + 70 };
      const pile = addPile(el, pileAt);
      setLibrarianAway(true);
      const walker = addWalker(el, lookOf(LIBRARIAN, false), desk);
      const route = [desk, { x: desk.x, y: archives.corridorY }, { x: pileAt.x, y: archives.corridorY }, { x: pileAt.x, y: office.corridorY }, pileAt];
      await walk(walker, route, motion);
      pile.remove();
      carry(walker);
      await walk(walker, [...route].reverse(), motion);
      walker.remove();
      setLibrarianAway(false);
      celebrate(el, "ARCHIVED!", `${feature.id} filed by the librarian`, { x: SCENE_WIDTH / 2, y: archives.y + 40 }, motion);
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
      <Banner onOpenDirector={() => (director ? onSelectAgent(director.address) : onHireDirector())} onReview={() => setReviewing(active ?? null)} />
      {reviewing && <FeatureReview feature={reviewing} onClose={() => setReviewing(null)} />}
      {archivesOpen && <ArchivesPanel onClose={() => setArchivesOpen(false)} onOpenChat={onSelectAgent} />}
      <div className={styles.wrap}>
        <div className={styles.scene} style={{ width: SCENE_WIDTH, height: Math.max(layout.height, full.height) }}>
          <Wall binUnread={binUnread} onOpenBin={onOpenBin} />
          <div className={styles.floor} style={{ top: WALL_HEIGHT }} />
          {layout.rooms.map((room, i) => (
            <RoomView
              key={room.id}
              room={room}
              carpet={room.kind === "director" ? undefined : teamColors(i - 1).carpet}
              building={building.has(room.id)}
              queue={room.kind === "team" ? queue(room.id) : 0}
            >
              {room.kind === "archives" && <Shelves count={features.filter((f) => f.status === "finished").length} onOpen={() => setArchivesOpen(true)} />}
              {room.kind === "director" && (
                <DirectorProps
                  room={room}
                  vacant={!director}
                  proposals={proposals}
                  done={featureTickets.filter((t) => t.status === "done").length}
                  total={featureTickets.length}
                  onHire={onHireDirector}
                />
              )}
            </RoomView>
          ))}
          {layout.rooms.flatMap((room) =>
            room.desks.map(({ address, at }) => {
              if (address === LIBRARIAN) return <Librarian key={address} at={at} away={librarianAway} onOpen={() => setArchivesOpen(true)} />;
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

function Wall({ binUnread, onOpenBin }: { binUnread: number; onOpenBin: () => void }) {
  return (
    <div className={styles.wall} style={{ height: WALL_HEIGHT }}>
      {[60, 200, 620].map((x) => (
        <span key={x} className={styles.window} style={{ left: x }} />
      ))}
      <span className={styles.clock} />
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
  children?: React.ReactNode;
}

function RoomView({ room, carpet, building, queue, children }: RoomViewProps) {
  return (
    <div
      className={`${styles.room} ${room.kind === "director" ? styles.rug : ""} ${building ? styles.building : ""}`}
      style={{ left: room.x, top: room.y, width: room.width, height: room.height, background: carpet }}
    >
      <span className={styles.sign}>
        {room.kind === "director" ? "DIRECTOR" : room.id.toUpperCase()}
        {room.kind === "team" && <em> Q{queue}</em>}
      </span>
      <span className={styles.plant} />
      {children}
    </div>
  );
}

interface DirectorPropsProps {
  room: Room;
  vacant: boolean;
  proposals: number;
  done: number;
  total: number;
  onHire: () => void;
}

// The director's board: proposals waiting for the human and the active
// feature's progress. While the office is vacant, a sign offers to recruit.
function DirectorProps({ vacant, proposals, done, total, onHire }: DirectorPropsProps) {
  return (
    <>
      <span className={styles.board}>
        PROPOSALS
        <br />
        <b>{proposals} waiting</b>
        <br />
        TICKETS {done}/{total}
      </span>
      {vacant && (
        <button type="button" className={styles.vacant} onClick={onHire}>
          VACANT
          <br />
          <span>Recruit a director</span>
        </button>
      )}
    </>
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

// Librarian is the archives' keeper at its desk: a character of the office,
// not an agent. Clicking it opens the archives.
function Librarian({ at, away, onOpen }: { at: Point; away: boolean; onOpen: () => void }) {
  return (
    <button type="button" className={styles.desk} style={{ left: at.x, top: at.y }} data-status="idle" aria-label="The librarian: open the archives" onClick={onOpen}>
      {away ? (
        <span className={styles.chair} />
      ) : (
        <>
          <span className={styles.char}>
            <Sprite look={lookOf(LIBRARIAN, false)} />
          </span>
          <span className={`${styles.say} ${styles.sayIdle}`} aria-hidden="true">
            z
          </span>
        </>
      )}
      <span className={styles.deskTop}>
        <span className={styles.book} />
      </span>
      <span className={styles.plate}>
        librarian{away && <em> out</em>}
      </span>
    </button>
  );
}

// Shelves hold one box per finished feature. Clicking them opens the archives.
function Shelves({ count, onOpen }: { count: number; onOpen: () => void }) {
  const shown = Math.min(count, 12);
  return (
    <button type="button" className={styles.shelves} aria-label={`Archives: ${count} finished features`} onClick={onOpen}>
      {[0, 1, 2].map((row) => (
        <span key={row} className={styles.shelf}>
          {Array.from({ length: Math.max(0, Math.min(4, shown - row * 4)) }, (_, i) => (
            <span key={i} className={styles.box} />
          ))}
        </span>
      ))}
    </button>
  );
}

// addPile drops a pile of ticket cards where the librarian will pick it up.
function addPile(layer: HTMLElement, at: Point): HTMLElement {
  const pile = document.createElement("div");
  pile.className = styles.pile;
  pile.style.left = `${at.x + 34}px`;
  pile.style.top = `${at.y + 60}px`;
  layer.append(pile);
  return pile;
}

// carry puts the pile above the walker's head.
function carry(walker: HTMLElement) {
  const pile = document.createElement("span");
  pile.className = styles.carried;
  walker.append(pile);
}
