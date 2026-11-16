// Where everything stands in the office: a pure function from the
// organization to rooms and desks, in pixels of the scene.
//
// The scene is SCENE_WIDTH wide. The wall runs along the top, with the
// entrance door on the right: newcomers walk in through it, dismissed agents
// walk out. Below, rooms are laid out in rows, left to right: the director's
// office first (always shown, vacant while the project has no director), then
// one room per team in the order of .nobody/org.yaml, then the archives, where
// the librarian files the tickets of finished features. A room is as wide as its
// desks need; when a row is full, the next room starts a new row, and the
// floor grows downwards (the office scrolls). Between two rows runs a
// corridor, where walkers go.
//
// A desk position is the top-left corner of its "station" (character, desk,
// name plate), DESK_WIDTH × DESK_HEIGHT.

export const SCENE_WIDTH = 960;
export const WALL_HEIGHT = 84;
export const DESK_WIDTH = 104;
export const DESK_HEIGHT = 118;

const MARGIN_X = 20;
const ROW_TOP = WALL_HEIGHT + 24;
const ROOM_GAP = 20;
const ROW_GAP = 40; // the corridor between two rows of rooms
const ROOM_PADDING_X = 36;
const ROOM_PADDING_TOP = 58; // room for the hanging sign
const ROOM_PADDING_BOTTOM = 40;
const DESK_STEP_X = 150;
const DESK_STEP_Y = 150;
const MIN_ROOM_WIDTH = 200;
const DIRECTOR_ROOM_WIDTH = 340;
const ARCHIVES_ROOM_WIDTH = 300;

// The librarian is a character of the office, not an agent: it carries the
// tickets of finished features to the archives. Its desk uses this address.
export const LIBRARIAN = "~librarian";
const FLOOR_BOTTOM = 30;

// The entrance door, in the wall; walkers appear and vanish at DOOR_STATION.
export const DOOR = { x: 850, width: 56 };
export const DOOR_STATION = { x: DOOR.x + DOOR.width / 2 - DESK_WIDTH / 2, y: 20 };
// The wastebasket stands by the door.
export const BIN = { x: 918, y: 52 };

export interface Point {
  x: number;
  y: number;
}

export interface Room {
  id: string; // "director", "archives" or the team ID
  kind: "director" | "team" | "archives";
  x: number;
  y: number;
  width: number;
  height: number;
  // Where a walker stands in the corridor just above the room (station top).
  corridorY: number;
  desks: { address: string; at: Point }[];
}

export interface OfficeLayout {
  rooms: Room[];
  desks: Map<string, Point>;
  height: number;
}

export interface OfficeInput {
  // The director's address, or null while the project has none.
  director: string | null;
  teams: { id: string; members: string[] }[];
}

const maxRowWidth = SCENE_WIDTH - 2 * MARGIN_X;
// How many desks fit side by side in the widest room.
const DESKS_PER_ROW = Math.floor((maxRowWidth - 2 * ROOM_PADDING_X + (DESK_STEP_X - DESK_WIDTH)) / DESK_STEP_X);

function roomSize(kind: Room["kind"], members: number): { width: number; height: number; perRow: number } {
  const perRow = Math.max(1, Math.min(members, DESKS_PER_ROW));
  const deskRows = Math.max(1, Math.ceil(members / DESKS_PER_ROW));
  const height = ROOM_PADDING_TOP + DESK_HEIGHT + (deskRows - 1) * DESK_STEP_Y + ROOM_PADDING_BOTTOM;
  if (kind === "director") return { width: DIRECTOR_ROOM_WIDTH, height, perRow: 1 };
  if (kind === "archives") return { width: ARCHIVES_ROOM_WIDTH, height, perRow: 1 };
  const width = Math.max(MIN_ROOM_WIDTH, 2 * ROOM_PADDING_X + perRow * DESK_STEP_X - (DESK_STEP_X - DESK_WIDTH));
  return { width, height, perRow };
}

export function layoutOffice(input: OfficeInput): OfficeLayout {
  const wanted: { id: string; kind: Room["kind"]; members: string[] }[] = [
    { id: "director", kind: "director", members: input.director ? [input.director] : [] },
    ...input.teams.map((t) => ({ id: t.id, kind: "team" as const, members: t.members })),
    { id: "archives", kind: "archives", members: [LIBRARIAN] },
  ];

  const rooms: Room[] = [];
  const desks = new Map<string, Point>();
  let x = MARGIN_X;
  let y = ROW_TOP;
  let rowHeight = 0;

  for (const w of wanted) {
    const size = roomSize(w.kind, w.members.length);
    if (x > MARGIN_X && x + size.width > MARGIN_X + maxRowWidth) {
      x = MARGIN_X;
      y += rowHeight + ROW_GAP;
      rowHeight = 0;
    }

    const room: Room = { id: w.id, kind: w.kind, x, y, width: size.width, height: size.height, corridorY: y - 70, desks: [] };
    w.members.forEach((address, i) => {
      const at = {
        x: x + ROOM_PADDING_X + (i % size.perRow) * DESK_STEP_X,
        y: y + ROOM_PADDING_TOP + Math.floor(i / size.perRow) * DESK_STEP_Y,
      };
      room.desks.push({ address, at });
      desks.set(address, at);
    });
    rooms.push(room);

    x += size.width + ROOM_GAP;
    rowHeight = Math.max(rowHeight, size.height);
  }

  return { rooms, desks, height: y + rowHeight + FLOOR_BOTTOM };
}

// headOf is where a character's head is, for paper planes.
export function headOf(desk: Point): Point {
  return { x: desk.x + DESK_WIDTH / 2, y: desk.y + 24 };
}

// pathFromDoor is the walk of a newcomer: through the door, down to the
// corridor above its room, along it, then into the room to its desk. Points
// are station positions.
export function pathFromDoor(desk: Point, corridorY: number): Point[] {
  return [DOOR_STATION, { x: DOOR_STATION.x, y: corridorY }, { x: desk.x, y: corridorY }, desk];
}
