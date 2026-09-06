// agent-fitness-functions-opencode-plugin: managed by agent-fitness-functions client install-hooks
import { spawnSync } from "child_process";

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
      } else if (tool === "edit" || tool === "write" || tool === "new_file" || tool === "newfile") {
        hookName = "agent-fitness-functions-pre-tool-use";
        const args = output?.args || input?.args || {};
        payload = { tool_name: input.tool, tool_input: args };
      } else {
        return;
      }

      const gitPathProc = spawnSync("git", ["rev-parse", "--git-path", `hooks/${hookName}`], {
        encoding: "utf-8",
      });
      if (gitPathProc.status !== 0 || !gitPathProc.stdout.trim()) {
        return;
      }
      const hookPath = gitPathProc.stdout.trim();

      const proc = spawnSync(hookPath, [], {
        input: JSON.stringify(payload),
        encoding: "utf-8",
        env: { ...process.env },
      });

      if (proc.status !== 0) {
        const errorMsg = (proc.stderr || proc.stdout || "Blocked by agent-fitness-functions").trim();
        throw new Error(errorMsg);
      }
    },
  };
};
