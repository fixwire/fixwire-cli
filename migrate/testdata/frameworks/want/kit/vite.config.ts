import { fixwireSvelteKit as sentrySvelteKit } from "@fixwire/sveltekit";
import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [sentrySvelteKit({ sourceMapsUploadOptions: { org: "acme", project: "kit-shop" } }), sveltekit()],
});
