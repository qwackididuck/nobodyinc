import type { NextConfig } from "next";

// The launcher serves the API (ADR-0014): its own routes under /api/, each
// open project's under /p/<project>/api/. In development the UI runs on its
// own port, so REST calls and event streams go through this proxy (no CORS
// needed); WebSockets (terminals, holding a project) go straight to the
// launcher, which allows this origin.
//
// The control room is a single client-side page and uses no server feature
// of Next.js. For the launcher and the desktop app (ADR-0016), it is
// exported as a static site (NOBODY_WEB_EXPORT=1, `task web:export`) that the
// launcher serves itself, on the same origin as the API: no proxy then.
const daemonURL = process.env.NOBODY_DAEMON_URL ?? "http://127.0.0.1:7420";
const staticExport = process.env.NOBODY_WEB_EXPORT === "1";

const nextConfig: NextConfig = staticExport
  ? { output: "export", images: { unoptimized: true }, env: { NEXT_PUBLIC_NOBODY_STATIC: "1" } }
  : {
      rewrites() {
        return [
          { source: "/api/:path*", destination: `${daemonURL}/api/:path*` },
          { source: "/p/:path*", destination: `${daemonURL}/p/:path*` },
        ];
      },
    };

export default nextConfig;
