// agent-fitness-functions-opencode-plugin: managed by agent-fitness-functions client install-hooks
import { spawnSync } from "child_process";
import path from "path";

export const AgentFitnessFunctionsPlugin = async () => {
  return {
    "tool.execute.before": async (input, output) => {
      if (!input || !input.tool) return;
      const tool = input.tool.toLowerCase();

      let hookName = "";
      let payload = null;

      if (tool === "bash") {
        hookName = "agent-fitness-functions-git-guard";
        const command = output?.args?.command || input?.args?.command || "";
        if (!command) return;
        payload = { tool_name: "Bash", tool_input: { command } };
      } else if (
        tool === "edit" ||
        tool === "write" ||
        tool === "new_file" ||
        tool === "newfile"
      ) {
        hookName = "agent-fitness-functions-pre-tool-use";
        const args = output?.args || input?.args || {};
        payload = { tool_name: input.tool, tool_input: args };
      } else {
        return;
      }

      const sessionID =
        typeof input.sessionID === "string" ? input.sessionID : "";
      payload.session_id = sessionID;

      const gitTopProc = spawnSync("git", ["rev-parse", "--show-toplevel"], {
        encoding: "utf-8",
      });
      if (gitTopProc.status !== 0 || !gitTopProc.stdout.trim()) {
        return;
      }
      const topLevel = gitTopProc.stdout.trim();

      const gitPathProc = spawnSync(
        "git",
        ["rev-parse", "--git-path", `hooks/${hookName}`],
        {
          encoding: "utf-8",
          cwd: topLevel,
        },
      );
      if (gitPathProc.status !== 0 || !gitPathProc.stdout.trim()) {
        return;
      }
      const rawHookPath = gitPathProc.stdout.trim();
      const hookPath = path.isAbsolute(rawHookPath)
        ? rawHookPath
        : path.resolve(topLevel, rawHookPath);

      const proc = spawnSync(hookPath, [], {
        input: JSON.stringify(payload),
        encoding: "utf-8",
        cwd: topLevel,
        env: {
          ...process.env,
          AGENT_FITNESS_FUNCTIONS_HISTORY_SOURCE: "agent",
          AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL: "opencode",
          AGENT_FITNESS_FUNCTIONS_HISTORY_ACTION: input.tool,
          AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID: sessionID,
        },
      });

      if (proc.status !== 0) {
        const errorMsg = (
          proc.stderr ||
          proc.stdout ||
          "Blocked by agent-fitness-functions"
        ).trim();
        throw new Error(errorMsg);
      }
    },
  };
};
