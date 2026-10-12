// agent-fitness-functions-pi-extension: managed by agent-fitness-functions client install-hooks
import { constants } from "node:fs";
import { access, mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import {
  createEditToolDefinition,
  createWriteToolDefinition,
  type ExtensionAPI,
} from "@earendil-works/pi-coding-agent";
import { checkCommand, checkProposal } from "./native-checker.mjs";

export default function (pi: ExtensionAPI): void {
  const editDefinition = createEditToolDefinition(process.cwd());
  const writeDefinition = createWriteToolDefinition(process.cwd());

  pi.registerTool({
    ...editDefinition,
    async execute(...args: Parameters<typeof editDefinition.execute>) {
      const [toolCallID, input, signal, onUpdate, ctx] = args;
      let diagnostic = "";
      const guarded = createEditToolDefinition(ctx.cwd, {
        operations: {
          readFile: (absolutePath) => readFile(absolutePath),
          access: (absolutePath) => access(absolutePath, constants.R_OK | constants.W_OK),
          writeFile: async (absolutePath, content) => {
            const verdict = await checkProposal(
              ctx.cwd,
              "pi",
              "edit",
              ctx.sessionManager.getSessionId() || "",
              input.path,
              content,
            );
            diagnostic = verdict.diagnostic;
            if (!verdict.allowed) throw new Error(verdict.diagnostic);
            await writeFile(absolutePath, content, "utf8");
          },
        },
      });
      const result = await guarded.execute(toolCallID, input, signal, onUpdate, ctx);
      if (diagnostic) {
        result.content.push({ type: "text", text: `agent-fitness-functions diagnostic:\n${diagnostic}` });
      }
      return result;
    },
  });

  pi.registerTool({
    ...writeDefinition,
    async execute(...args: Parameters<typeof writeDefinition.execute>) {
      const [toolCallID, input, signal, onUpdate, ctx] = args;
      let diagnostic = "";
      const guarded = createWriteToolDefinition(ctx.cwd, {
        operations: {
          mkdir: async () => {},
          writeFile: async (absolutePath, content) => {
            const verdict = await checkProposal(
              ctx.cwd,
              "pi",
              "write",
              ctx.sessionManager.getSessionId() || "",
              input.path,
              content,
            );
            diagnostic = verdict.diagnostic;
            if (!verdict.allowed) throw new Error(verdict.diagnostic);
            await mkdir(dirname(absolutePath), { recursive: true });
            await writeFile(absolutePath, content, "utf8");
          },
        },
      });
      const result = await guarded.execute(toolCallID, input, signal, onUpdate, ctx);
      if (diagnostic) {
        result.content.push({ type: "text", text: `agent-fitness-functions diagnostic:\n${diagnostic}` });
      }
      return result;
    },
  });

  pi.on("tool_call", async (event, ctx) => {
    if (event.toolName !== "bash" || typeof event.input.command !== "string") return;
    const verdict = await checkCommand(
      ctx.cwd,
      "pi",
      ctx.sessionManager.getSessionId() || "",
      event.input.command,
    );
    if (!verdict.allowed) return { block: true, reason: verdict.diagnostic };
  });
}
