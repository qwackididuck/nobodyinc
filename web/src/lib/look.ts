// How an agent looks in the control room: hair, skin and shirt colors of its
// pixel character, and the color of its avatar in the chat (its shirt).
//
// The look is derived from the agent's address, so that it stays the same
// across reloads and browsers without being stored anywhere. The director
// always wears a dark suit with a gold tie.

export interface Look {
  hair: string;
  skin: string;
  shirt: string;
  // Tie color; the shirt color when there is no tie.
  tie: string;
}

const HAIR = ["#1d1b26", "#3a2418", "#6b4a2f", "#e0a24a", "#c2462e", "#8a6bb8", "#2b2238", "#d9d2c0"];
const SKIN = ["#f2c9a8", "#f0c2a0", "#e9b48f", "#d9a27b", "#c88a5e", "#b07a55", "#8d5a3b", "#6e4630"];
const SHIRT = ["#3fb0e0", "#b86fe0", "#ef6b86", "#57c27f", "#ff9a4d", "#4a6fa5", "#e0a630", "#00add8", "#c96f4a", "#7d86c9"];

const DIRECTOR: Look = { hair: "#e8e2d0", skin: "#e9b48f", shirt: "#2f2b44", tie: "#ffd34d" };

// hash is FNV-1a: small, stable, and spreads similar names apart.
function hash(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

export function lookOf(address: string, isDirector: boolean): Look {
  if (isDirector) return DIRECTOR;
  const h = hash(address);
  const shirt = SHIRT[h % SHIRT.length];
  return {
    hair: HAIR[(h >>> 4) % HAIR.length],
    skin: SKIN[(h >>> 8) % SKIN.length],
    shirt,
    tie: shirt,
  };
}

// The avatar color in the chat: the character's shirt, or gold for the
// director (whose suit is too dark for an avatar).
export function avatarColor(address: string, isDirector: boolean): string {
  return isDirector ? "#e0a630" : lookOf(address, false).shirt;
}
