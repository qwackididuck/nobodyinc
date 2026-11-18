// Where everything stands in the office: a pure function from the
// organization to rooms and desks, in pixels of the scene.
//
// The scene is SCENE_WIDTH wide. The wall runs along the top, with the
// entrance door on the right: newcomers walk in through it, dismissed agents
// walk out. Below, a fixed column on the left holds the director's office
// and, under it, the archives, where finished features' tickets are filed on
// shelves. To its right, rooms are laid out in rows, left to right: one room
// per team in the order of .nobody/org.yaml, then the plot (an empty lot with
// a "+" where the next team's room will be built). A room is as wide as its
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
const LEFT_COLUMN_WIDTH = 340; // the director's office and the archives
const PLOT_ROOM_WIDTH = 200;
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
  id: string; // "director", "plot", "archives" or the team ID
  kind: "director" | "team" | "plot" | "archives";
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

// The teams' area, right of the left column.
const AREA_X = MARGIN_X + LEFT_COLUMN_WIDTH + ROOM_GAP;
const AREA_WIDTH = SCENE_WIDTH - MARGIN_X - AREA_X;
// How many desks fit side by side in the widest team room.
const DESKS_PER_ROW = Math.max(1, Math.floor((AREA_WIDTH - 2 * ROOM_PADDING_X + (DESK_STEP_X - DESK_WIDTH)) / DESK_STEP_X));
// A room with one row of desks.
const ONE_ROW_HEIGHT = ROOM_PADDING_TOP + DESK_HEIGHT + ROOM_PADDING_BOTTOM;

function roomSize(kind: "team" | "plot", members: number): { width: number; height: number; perRow: number } {
  if (kind === "plot") return { width: PLOT_ROOM_WIDTH, height: ONE_ROW_HEIGHT, perRow: 1 };
  const perRow = Math.max(1, Math.min(members, DESKS_PER_ROW));
  const deskRows = Math.max(1, Math.ceil(members / DESKS_PER_ROW));
  const height = ONE_ROW_HEIGHT + (deskRows - 1) * DESK_STEP_Y;
  const width = Math.max(MIN_ROOM_WIDTH, 2 * ROOM_PADDING_X + perRow * DESK_STEP_X - (DESK_STEP_X - DESK_WIDTH));
  return { width, height, perRow };
}

function room(id: string, kind: Room["kind"], x: number, y: number, width: number, height: number): Room {
  return { id, kind, x, y, width, height, corridorY: y - 70, desks: [] };
}

export function layoutOffice(input: OfficeInput): OfficeLayout {
  const desks = new Map<string, Point>();

  // The left column: the director's office, the archives under it.
  const director = room("director", "director", MARGIN_X, ROW_TOP, LEFT_COLUMN_WIDTH, ONE_ROW_HEIGHT);
  if (input.director) {
    const at = { x: director.x + ROOM_PADDING_X, y: director.y + ROOM_PADDING_TOP };
    director.desks.push({ address: input.director, at });
    desks.set(input.director, at);
  }
  const archives = room("archives", "archives", MARGIN_X, director.y + director.height + ROW_GAP, LEFT_COLUMN_WIDTH, ONE_ROW_HEIGHT);
  const rooms: Room[] = [director];

  // The teams' area: team rooms, then the plot, in rows.
  const wanted = [...input.teams.map((t) => ({ id: t.id, kind: "team" as const, members: t.members })), { id: "plot", kind: "plot" as const, members: [] }];
  let x = AREA_X;
  let y = ROW_TOP;
  let rowHeight = 0;

  for (const w of wanted) {
    const size = roomSize(w.kind, w.members.length);
    if (x > AREA_X && x + size.width > AREA_X + AREA_WIDTH) {
      x = AREA_X;
      y += rowHeight + ROW_GAP;
      rowHeight = 0;
    }

    const r = room(w.id, w.kind, x, y, size.width, size.height);
    w.members.forEach((address, i) => {
      const at = {
        x: x + ROOM_PADDING_X + (i % size.perRow) * DESK_STEP_X,
        y: y + ROOM_PADDING_TOP + Math.floor(i / size.perRow) * DESK_STEP_Y,
      };
      r.desks.push({ address, at });
      desks.set(address, at);
    });
    rooms.push(r);

    x += size.width + ROOM_GAP;
    rowHeight = Math.max(rowHeight, size.height);
  }

  rooms.push(archives);
  const bottom = Math.max(archives.y + archives.height, y + rowHeight);

  return { rooms, desks, height: bottom + FLOOR_BOTTOM };
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
