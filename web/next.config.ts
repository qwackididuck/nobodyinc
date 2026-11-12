import type { NextConfig } from "next";

// The daemon serves the API. In development the UI runs on its own port, so
// REST calls go through this proxy (no CORS needed); terminals use a direct
// WebSocket to the daemon, which allows this origin.
const daemonURL = process.env.NOBODY_DAEMON_URL ?? "http://127.0.0.1:7420";

const nextConfig: NextConfig = {
  cacheComponents: true,
  partialPrefetching: true,
  rewrites() {
    return [{ source: "/api/:path*", destination: `${daemonURL}/api/:path*` }];
  },
};

export default nextConfig;
