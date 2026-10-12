// agent-fitness-functions-native-checker: managed by agent-fitness-functions client install-hooks
import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";

const NON_GIT_RE = /not a git repository|cannot find (?:a )?git repository/i;
const TAGGED_PATH_RE = /^\[(.*?)(?:#[0-9a-fA-F]{4})?\]$/;
const UNICODE_SPACE_RE = /[\u00a0\u2000-\u200a\u202f\u205f\u3000]/g;

function trimmedOutput(stderr, stdout) {
  return [stderr, stdout].filter((value) => value).join("\n").trim();
}

function runProcess(command, args, options = {}) {
  return new Promise((resolve) => {
    let stdout = "";
    let stderr = "";
    let settled = false;
    const child = spawn(command, args, {
      cwd: options.cwd,
      env: options.env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    const finish = (result) => {
      if (settled) return;
      settled = true;
      resolve(result);
    };
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.on("error", (error) => finish({ error, stdout, stderr, code: null, signal: null }));
    child.on("close", (code, signal) => finish({ error: null, stdout, stderr, code, signal }));
    if (options.input !== undefined) child.stdin.end(options.input);
    else child.stdin.end();
  });
}

async function gitOutput(cwd, args) {
  const result = await runProcess("git", ["-C", cwd, ...args], { cwd });
  return result;
}

function blockedDiagnostic(result, fallback) {
  return trimmedOutput(result.stderr, result.stdout) || fallback;
}

async function resolveRepo(cwd) {
  const result = await gitOutput(cwd, ["rev-parse", "--show-toplevel"]);
  if (result.error) {
    return { repoRoot: "", diagnostic: `agent-fitness-functions: unable to resolve Git working tree: ${result.error.message}` };
  }
  if (result.code !== 0 || result.signal) {
    if (result.code !== 0 && NON_GIT_RE.test(trimmedOutput(result.stderr, result.stdout))) {
      return { repoRoot: "", noop: true };
    }
    return {
      repoRoot: "",
      diagnostic: blockedDiagnostic(result, "agent-fitness-functions: unable to resolve Git working tree"),
    };
  }
  const repoRoot = result.stdout.trim();
  if (!repoRoot) {
    return { repoRoot: "", diagnostic: "agent-fitness-functions: Git returned an empty working-tree root" };
  }
  return { repoRoot };
}

async function resolveHook(repoRoot, hookName) {
  const result = await gitOutput(repoRoot, ["rev-parse", "--git-path", `hooks/${hookName}`]);
  if (result.error || result.code !== 0 || result.signal) {
    return {
      path: "",
      diagnostic: blockedDiagnostic(result, `agent-fitness-functions: unable to resolve ${hookName}`),
    };
  }
  const rawPath = result.stdout.trim();
  if (!rawPath) {
    return { path: "", diagnostic: `agent-fitness-functions: Git returned an empty path for ${hookName}` };
  }
  return { path: path.isAbsolute(rawPath) ? rawPath : path.resolve(repoRoot, rawPath) };
}

async function invokeHook(repoRoot, hookName, payload, agent, action, sessionID) {
  const hook = await resolveHook(repoRoot, hookName);
  if (!hook.path) return { allowed: false, diagnostic: hook.diagnostic };
  const env = {
    ...process.env,
    AGENT_FITNESS_FUNCTIONS_HISTORY_SOURCE: "agent",
    AGENT_FITNESS_FUNCTIONS_HISTORY_TOOL: agent,
    AGENT_FITNESS_FUNCTIONS_HISTORY_ACTION: action,
    AGENT_FITNESS_FUNCTIONS_HISTORY_SESSION_ID: sessionID || "",
    AGENT_FITNESS_FUNCTIONS_HISTORY_WORKTREE: repoRoot,
    AGENT_FITNESS_FUNCTIONS_WORKTREE: repoRoot,
  };
  const result = await runProcess(hook.path, [], {
    cwd: repoRoot,
    env,
    input: JSON.stringify(payload),
  });
  if (result.error) {
    return { allowed: false, diagnostic: `agent-fitness-functions: unable to run ${hook.path}: ${result.error.message}` };
  }
  const diagnostic = trimmedOutput(result.stderr, result.stdout);
  if (result.code !== 0 || result.signal) {
    return { allowed: false, diagnostic: diagnostic || "Blocked by agent-fitness-functions" };
  }
  return { allowed: true, diagnostic };
}

async function check(cwd, agent, hookName, action, sessionID, toolInput) {
  const resolved = await resolveRepo(cwd);
  if (resolved.noop) return { allowed: true, diagnostic: "" };
  if (!resolved.repoRoot) return { allowed: false, diagnostic: resolved.diagnostic };
  return invokeHook(
    resolved.repoRoot,
    hookName,
    { tool_name: action, tool_input: toolInput, session_id: sessionID || "" },
    agent,
    action,
    sessionID || "",
  );
}

export async function checkProposal(cwd, agent, action, sessionID, filePath, content) {
  return check(cwd, agent, "agent-fitness-functions-pre-tool-use", action, sessionID, {
    file_path: filePath,
    content,
  });
}

export async function checkCommand(cwd, agent, sessionID, command) {
  return check(cwd, agent, "agent-fitness-functions-git-guard", "Bash", sessionID, { command });
}

export function resolveOMPFilePath(cwd, authoredPath) {
  if (typeof authoredPath !== "string") return "";
  let value = authoredPath;
  const tagged = value.match(TAGGED_PATH_RE);
  if (tagged) value = tagged[1];
  if (value.startsWith(":")) value = value.slice(1);
  if (value.startsWith("@") && (value[1] === "/" || value[1] === "~")) value = value.slice(1);
  value = value.replace(UNICODE_SPACE_RE, " ");
  if (value.startsWith("file://")) {
    try {
      value = decodeURIComponent(value.slice("file://".length));
    } catch {
      // Preserve invalid percent encoding as authored, matching OMP's path policy.
    }
  }
  if (value === "~") value = os.homedir();
  else if (value.startsWith("~/")) value = path.join(os.homedir(), value.slice(2));
  return path.resolve(cwd, value);
}

function normalizeText(value) {
  return value.replace(/\r\n|\r/g, "\n");
}

function occurrenceCount(text, needle) {
  let count = 0;
  let offset = 0;
  while (true) {
    const index = text.indexOf(needle, offset);
    if (index === -1) return count;
    count += 1;
    offset = index + needle.length;
  }
}

function replaceLiteral(text, oldText, newText, replaceAll) {
  if (replaceAll) return text.split(oldText).join(newText);
  const index = text.indexOf(oldText);
  return text.slice(0, index) + newText + text.slice(index + oldText.length);
}

export function reconstructOMPReplace(current, edits) {
  if (typeof current !== "string") throw new Error("agent-fitness-functions: OMP replacement requires UTF-8 text");
  if (!Array.isArray(edits) || edits.length === 0) {
    throw new Error("agent-fitness-functions: OMP replacement requires at least one edit");
  }
  const bom = current.startsWith("\uFEFF") ? "\uFEFF" : "";
  const withoutBom = bom ? current.slice(1) : current;
  const restoreCRLF = withoutBom.includes("\r\n");
  let result = normalizeText(withoutBom);
  for (const [index, edit] of edits.entries()) {
    if (!edit || typeof edit.old_string !== "string" || typeof edit.new_string !== "string") {
      throw new Error(`agent-fitness-functions: OMP replacement ${index + 1} requires string old_string and new_string`);
    }
    if (edit.old_string.length === 0) {
      throw new Error(`agent-fitness-functions: OMP replacement ${index + 1} cannot use an empty old_string`);
    }
    if (edit.replace_all !== undefined && typeof edit.replace_all !== "boolean") {
      throw new Error(`agent-fitness-functions: OMP replacement ${index + 1} replace_all must be boolean`);
    }
    const oldText = normalizeText(edit.old_string);
    const newText = normalizeText(edit.new_string);
    const matches = occurrenceCount(result, oldText);
    if (matches === 0) {
      throw new Error(`agent-fitness-functions: OMP replacement ${index + 1} old_string was not found`);
    }
    if (matches > 1 && edit.replace_all !== true) {
      throw new Error(`agent-fitness-functions: OMP replacement ${index + 1} old_string matched multiple locations; set replace_all=true`);
    }
    result = replaceLiteral(result, oldText, newText, edit.replace_all === true);
  }
  const restored = bom + (restoreCRLF ? result.replace(/\n/g, "\r\n") : result);
  if (restored === current) throw new Error("agent-fitness-functions: OMP replacement is a no-op");
  return restored;
}
