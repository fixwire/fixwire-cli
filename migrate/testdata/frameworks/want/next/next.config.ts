import { withFixwireConfig as withSentryConfig } from "@fixwire/nextjs";
import type { NextConfig } from "next";

const nextConfig: NextConfig = { reactStrictMode: true };

export default withSentryConfig(nextConfig, {
  org: "acme",
  project: "store",
  silent: !process.env.CI,
});
