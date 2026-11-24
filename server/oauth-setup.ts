import { registerBunOAuthFlows } from "@earendil-works/pi-ai/bun-oauth";

// OAuth flows use variable imports by default, which standalone Bun binaries
// cannot discover. Register pi-ai's statically bundled flows before use.
registerBunOAuthFlows();
