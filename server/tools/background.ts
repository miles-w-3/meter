import { defineTool } from "@earendil-works/pi-durable";
import { z } from "zod";
import type { BackgroundJobs } from "../background-jobs.ts";

const runParams = z.strictObject({
  command: z.string().trim().min(1).max(16_000),
  label: z.string().trim().max(120).optional(),
  mode: z.enum(["exit", "monitor"]).optional(),
  timeoutSeconds: z.number().min(1).max(1800).optional(),
});
const idParams = z.strictObject({ id: z.string().uuid() });
function schema(value: z.ZodType) { const result = z.toJSONSchema(value); delete result.$schema; return result; }
const output = (text: string, isError = false) => ({ isError, output: [{ type: "text" as const, text }] });

export function backgroundTools(jobs: BackgroundJobs) {
  const run = defineTool({
    name: "bg_run", replay: "unsafe",
    description: "Run a foreground shell command in the background. Default mode 'exit' notifies you when the command finishes; mode 'monitor' delivers stdout lines as events while it runs. Monitor defaults to 5 minutes and stops after 60 event batches; noisy bursts may drop lines and report the count. Print only meaningful events. Return to other work rather than polling. Jobs belong to the conversation and stop when the Meter daemon shuts down. Do not use nohup, setsid, or a trailing &.",
    parameters: schema(runParams),
    execute: async (args, api, context) => {
      try {
        context.abortSignal?.throwIfAborted();
        const value = runParams.parse(args);
        if (!api.env) throw new Error("No execution environment for this conversation");
        const job = jobs.start({ ...value, label: value.label ?? "", cwd: api.env.cwd, conversationId: String(api.conversationId), timeoutMs: value.timeoutSeconds === undefined ? undefined : value.timeoutSeconds * 1000 });
        return output(`Started ${job.label} (id ${job.id}, mode ${job.mode}). You will be notified; do not poll.`);
      } catch (error) { return output(error instanceof Error ? error.message : "Could not start background job", true); }
    },
  });
  const show = defineTool({
    name: "bg_output", replay: "safe", description: "Inspect a background job's status and recent output. Do not repeatedly poll; notifications arrive automatically.", parameters: schema(idParams),
    execute: async (args, api) => {
      const parsed = idParams.safeParse(args);
      const job = parsed.success && jobs.list(String(api.conversationId)).find(item => item.id === parsed.data.id);
      return job ? output(JSON.stringify(job)) : output("Unknown background job", true);
    },
  });
  const stop = defineTool({
    name: "bg_stop", replay: "unsafe", description: "Stop a background job owned by this conversation.", parameters: schema(idParams),
    execute: async (args, api) => {
      try { return output(JSON.stringify(await jobs.stop(String(api.conversationId), idParams.parse(args).id))); }
      catch (error) { return output(error instanceof Error ? error.message : "Could not stop job", true); }
    },
  });
  return [run, show, stop];
}
