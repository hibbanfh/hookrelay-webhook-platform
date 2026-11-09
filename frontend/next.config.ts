import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Self-contained server.js for the slim runtime image (frontend/Dockerfile).
  output: "standalone",
  poweredByHeader: false,
};

export default nextConfig;
