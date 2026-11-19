// The furniture of the left column's rooms (layout.ts): the library, which
// holds the project's knowledge base (ADR-0019), and the archives, a records
// room where finished features are filed. They must not look alike: the
// library is warm wood and coloured spines, the archives steel cabinets and
// cardboard boxes on a concrete floor.

import styles from "./office.module.css";

// The library's bookcases: three, three shelves each, with one spine per
// page up to what they hold (a large knowledge base fills them all), dealt
// shelf by shelf so that a small one still furnishes every shelf. The spines
// of pages just added (the last ones dealt) sparkle for a while.
const BOOKCASES = 3;
const SHELVES_PER_CASE = 3;
const SPINES_PER_SHELF = 8;
const SPINE_COLORS = ["#3f6fa5", "#b34a4a", "#4f8a5b", "#c9973f", "#7a5aa0", "#2f7f86", "#a05a8a"];

interface LibraryProps {
  pages: number;
  // How many spines sparkle, counted from the last one shown.
  fresh: number;
}

export function LibraryFurniture({ pages, fresh }: LibraryProps) {
  const shelves = BOOKCASES * SHELVES_PER_CASE;
  const shown = Math.min(pages, shelves * SPINES_PER_SHELF);

  return (
    <>
      <span className={styles.bookcases} aria-label={`${pages} page${pages === 1 ? "" : "s"} in the library`}>
        {Array.from({ length: BOOKCASES }, (_, c) => (
          <span key={c} className={styles.libraryCase}>
            {Array.from({ length: SHELVES_PER_CASE }, (_, row) => (
              <span key={row} className={styles.libraryShelf}>
                {Array.from({ length: SPINES_PER_SHELF }, (_, s) => {
                  // The n-th page dealt: one per shelf in turn.
                  const n = s * shelves + c * SHELVES_PER_CASE + row;
                  if (n >= shown) return null;
                  return (
                    <span
                      key={s}
                      className={styles.spine}
                      data-fresh={n >= shown - fresh}
                      style={{ background: SPINE_COLORS[(n * 3) % SPINE_COLORS.length], height: 14 + ((n * 7) % 9) }}
                    />
                  );
                })}
              </span>
            ))}
          </span>
        ))}
      </span>
      <span className={styles.lamp} aria-hidden="true" />
      <span className={styles.readingTable} aria-hidden="true" />
    </>
  );
}

// The archives: four steel cabinets, four drawers each, a label on one
// drawer per finished feature; and boxes stacked on a pallet.
const CABINETS = 4;
const DRAWERS_PER_CABINET = 4;
const BOXES = [
  [0, 0],
  [32, 0],
  [64, 0],
  [16, 24],
  [48, 24],
] as const;

export function ArchivesFurniture({ count }: { count: number }) {
  return (
    <>
      <span className={styles.cabinets} aria-label={`${count} finished feature${count === 1 ? "" : "s"} in the archives`}>
        {Array.from({ length: CABINETS }, (_, c) => (
          <span key={c} className={styles.cabinet}>
            {Array.from({ length: DRAWERS_PER_CABINET }, (_, d) => (
              <span key={d} className={styles.drawer} data-filed={c * DRAWERS_PER_CABINET + d < count} />
            ))}
          </span>
        ))}
      </span>
      <span className={styles.pallet} aria-hidden="true" />
      {BOXES.map(([dx, dy]) => (
        <span key={`${dx}-${dy}`} className={styles.cardboard} style={{ right: 96 - dx, bottom: 44 + dy }} aria-hidden="true" />
      ))}
      <span className={styles.safetyLine} aria-hidden="true" />
    </>
  );
}
