import type { NextConfig } from "next";

// The daemon serves the API. In development the UI runs on its own port, so
// REST calls and the event stream (/api/events) go through this proxy (no
// CORS needed); terminals use a direct WebSocket to the daemon, which allows
// this origin.
//
// The control room is a single client-side page and uses no server feature
// of Next.js, so that it can later be exported as a static site.
const daemonURL = process.env.NOBODY_DAEMON_URL ?? "http://127.0.0.1:7420";

const nextConfig: NextConfig = {
  rewrites() {
    return [{ source: "/api/:path*", destination: `${daemonURL}/api/:path*` }];
  },
};

export default nextConfig;
