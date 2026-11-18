"use client";

// Read marks: when the human last opened each chat (and the wastebasket), so
// that the avatar rail can count what arrived since. They are kept per
// project in this browser only.

import { useCallback, useState } from "react";

import { readStored, writeStored } from "@/lib/storage";

export type ReadMarks = Record<string, string>; // chat key → ISO time last seen

export function useReadMarks(projectId: string | undefined) {
  const key = `nobody.read.${projectId ?? "unknown"}`;
  const [marks, setMarks] = useState<{ key: string; value: ReadMarks }>({ key: "", value: {} });

  // Load the marks of this project the first time they are needed.
  const current = marks.key === key ? marks.value : readStored<ReadMarks>(key, {});

  const markSeen = useCallback(
    (chat: string, at: string) => {
      setMarks((m) => {
        const value = m.key === key ? m.value : readStored<ReadMarks>(key, {});
        if (value[chat] && value[chat] >= at) return m.key === key ? m : { key, value };
        const next = { ...value, [chat]: at };
        writeStored(key, next);
        return { key, value: next };
      });
    },
    [key],
  );

  return { marks: current, markSeen };
}
