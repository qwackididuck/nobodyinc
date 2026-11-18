import type { NextConfig } from "next";

// The launcher serves the API (ADR-0014): its own routes under /api/, each
// open project's under /p/<project>/api/. In development the UI runs on its
// own port, so REST calls and event streams go through this proxy (no CORS
// needed); WebSockets (terminals, holding a project) go straight to the
// launcher, which allows this origin.
//
// The control room is a single client-side page and uses no server feature
// of Next.js, so that it can later be exported as a static site.
const daemonURL = process.env.NOBODY_DAEMON_URL ?? "http://127.0.0.1:7420";

const nextConfig: NextConfig = {
  rewrites() {
    return [
      { source: "/api/:path*", destination: `${daemonURL}/api/:path*` },
      { source: "/p/:path*", destination: `${daemonURL}/p/:path*` },
    ];
  },
};

export default nextConfig;
