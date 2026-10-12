// agent-fitness-functions-omp-extension: managed by agent-fitness-functions client install-hooks
import { readFile, stat } from "node:fs/promises";
import {
	checkCommand,
	checkProposal,
	reconstructOMPReplace,
	resolveOMPFilePath,
} from "./native-checker.mjs";
import { lookup, type AnySetting } from "@oh-my-pi/pi-coding-agent/config/registry";
import type { ExtensionAPI } from "@oh-my-pi/pi-coding-agent";

const SETTINGS_API_REQUIRED = "agent-fitness-functions: OMP 18.10.3-compatible settings API required";
const STRICT_REPLACE_REMEDIATION =
	"agent-fitness-functions: governed OMP requires strict replace mode; unset conflicting edit overrides or launch with PI_EDIT_VARIANT=replace PI_EDIT_FUZZY=0";

type SettingHandle = AnySetting;
type SettingScope = Parameters<SettingHandle["get"]>[0];
type SettingsHost = { settings?: unknown };
type PendingKey = string;

function settingsScope(pi: ExtensionAPI): SettingScope | undefined {
	const host = pi.pi as unknown as SettingsHost;
	if (!host.settings) return undefined;
	return host.settings as SettingScope;
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
	if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
	return value as Record<string, unknown>;
}

function pendingKey(sessionID: string, toolCallID: string): PendingKey {
	return `${sessionID}\u0000${toolCallID}`;
}

function isInternalResourcePath(value: string): boolean {
	if (/^(?:local|session|mcp|resource):\/\//i.test(value)) return true;
	return /\.(?:db|sqlite|sqlite3|zip|jar|tar|tgz|gz|bz2|xz|7z|rar):/i.test(value);
}

function pathInput(input: Record<string, unknown>): string | undefined {
	return typeof input.path === "string" && input.path.length > 0 ? input.path : undefined;
}

function strictEditEntries(input: Record<string, unknown>):
	| { path: string; edits: Array<{ old_string: string; new_string: string; replace_all?: boolean }> }
	| { error: string }
	| undefined {
	const path = pathInput(input);
	if (!path) return { error: "agent-fitness-functions: OMP edit requires a nonempty path" };
	if ("input" in input || "op" in input || "diff" in input) return { error: STRICT_REPLACE_REMEDIATION };
	if (Array.isArray(input.edits)) {
		if (input.edits.length === 0) return { error: "agent-fitness-functions: OMP edit requires at least one replacement" };
		const edits: Array<{ old_string: string; new_string: string; replace_all?: boolean }> = [];
		for (const [index, candidate] of input.edits.entries()) {
			if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) {
				return { error: `agent-fitness-functions: OMP edit ${index + 1} is not a replace entry` };
			}
			const entry = candidate as Record<string, unknown>;
			if (typeof entry.old_string !== "string" || typeof entry.new_string !== "string") {
				return { error: `agent-fitness-functions: OMP edit ${index + 1} requires string old_string and new_string` };
			}
			if (entry.replace_all !== undefined && typeof entry.replace_all !== "boolean") {
				return { error: `agent-fitness-functions: OMP edit ${index + 1} replace_all must be boolean` };
			}
			edits.push({
				old_string: entry.old_string,
				new_string: entry.new_string,
				...(entry.replace_all === undefined ? {} : { replace_all: entry.replace_all }),
			});
		}
		return { path, edits };
	}
	if (typeof input.old_string !== "string" || typeof input.new_string !== "string") {
		return { error: STRICT_REPLACE_REMEDIATION };
	}
	if (input.replace_all !== undefined && typeof input.replace_all !== "boolean") {
		return { error: "agent-fitness-functions: OMP edit replace_all must be boolean" };
	}
	return {
		path,
		edits: [
			{
				old_string: input.old_string,
				new_string: input.new_string,
				...(input.replace_all === undefined ? {} : { replace_all: input.replace_all }),
			},
		],
	};
}

export default function (pi: ExtensionAPI): void {
	let configurationDiagnostic = SETTINGS_API_REQUIRED;
	let modeHandle: SettingHandle | undefined;
	let fuzzyHandle: SettingHandle | undefined;
	let variantsHandle: SettingHandle | undefined;
	const pendingDiagnostics = new Map<PendingKey, string>();

	const configure = (): void => {
		const scope = settingsScope(pi);
		modeHandle = lookup("edit.mode");
		fuzzyHandle = lookup("edit.fuzzyMatch");
		variantsHandle = lookup("edit.modelVariants");
		if (!scope || !modeHandle || !fuzzyHandle || !variantsHandle) {
			configurationDiagnostic = SETTINGS_API_REQUIRED;
			return;
		}
		try {
			modeHandle.override(scope, "replace");
			fuzzyHandle.override(scope, false);
			const variants = recordValue(variantsHandle.get(scope));
			const strictVariants = Object.fromEntries(Object.keys(variants ?? {}).map((key) => [key, "replace"]));
			variantsHandle.override(scope, strictVariants);
			configurationDiagnostic = "";
		} catch (error) {
			configurationDiagnostic = `agent-fitness-functions: unable to configure OMP strict replace mode: ${
				error instanceof Error ? error.message : String(error)
			}`;
		}
	};

	const strictModeDiagnostic = (): string => {
		if (configurationDiagnostic) return configurationDiagnostic;
		const scope = settingsScope(pi);
		if (!scope || !modeHandle || !fuzzyHandle || !variantsHandle) return SETTINGS_API_REQUIRED;
		try {
			if (modeHandle.get(scope) !== "replace" || fuzzyHandle.get(scope) !== false) return STRICT_REPLACE_REMEDIATION;
			const variants = recordValue(variantsHandle.get(scope));
			if (variants && Object.values(variants).some((value) => value !== "replace")) return STRICT_REPLACE_REMEDIATION;
			return "";
		} catch (error) {
			return `agent-fitness-functions: unable to read OMP edit settings: ${error instanceof Error ? error.message : String(error)}`;
		}
	};

	pi.on("tool_call", async (event, ctx) => {
		const sessionID = ctx.sessionManager.getSessionId() || "";
		if (event.toolName === "bash") {
			if (typeof event.input.command !== "string") return;
			const bashCwd = "cwd" in event.input && typeof event.input.cwd === "string" ? event.input.cwd : ctx.cwd;
			const verdict = await checkCommand(bashCwd, "omp", sessionID, event.input.command);
			if (!verdict.allowed) return { block: true, reason: verdict.diagnostic };
			return;
		}
		if (event.toolName !== "edit" && event.toolName !== "write" && event.toolName !== "apply_patch") return;
		const settingsError = strictModeDiagnostic();
		if (settingsError) return { block: true, reason: settingsError };
		if (event.toolName === "apply_patch") return { block: true, reason: STRICT_REPLACE_REMEDIATION };

		const input = event.input as Record<string, unknown>;
		const authoredPath = pathInput(input);
		if (!authoredPath || isInternalResourcePath(authoredPath)) return;
		const absolutePath = resolveOMPFilePath(ctx.cwd, authoredPath);

		if (event.toolName === "write") {
			if (typeof input.content !== "string") return { block: true, reason: "agent-fitness-functions: OMP write requires string content" };
			const verdict = await checkProposal(ctx.cwd, "omp", "write", sessionID, absolutePath, input.content);
			if (!verdict.allowed) return { block: true, reason: verdict.diagnostic };
			if (verdict.diagnostic) pendingDiagnostics.set(pendingKey(sessionID, event.toolCallId), verdict.diagnostic);
			return { input: { ...input, path: absolutePath } };
		}

		const parsed = strictEditEntries(input);
		if (!parsed || "error" in parsed) return { block: true, reason: parsed?.error ?? STRICT_REPLACE_REMEDIATION };
		try {
			const information = await stat(absolutePath);
			if (!information.isFile()) throw new Error("target is not a regular file");
			const current = new TextDecoder("utf-8", { fatal: true }).decode(await readFile(absolutePath));
			const proposal = reconstructOMPReplace(current, parsed.edits);
			const verdict = await checkProposal(ctx.cwd, "omp", "edit", sessionID, absolutePath, proposal);
			if (!verdict.allowed) return { block: true, reason: verdict.diagnostic };
			if (verdict.diagnostic) pendingDiagnostics.set(pendingKey(sessionID, event.toolCallId), verdict.diagnostic);
			return { input: { ...input, path: absolutePath } };
		} catch (error) {
			return {
				block: true,
				reason: error instanceof Error ? error.message : String(error),
			};
		}
	});

	pi.on("tool_result", (event, _ctx) => {
		const sessionID = _ctx.sessionManager.getSessionId() || "";
		const key = pendingKey(sessionID, event.toolCallId);
		const diagnostic = pendingDiagnostics.get(key);
		if (!diagnostic) return;
		pendingDiagnostics.delete(key);
		return {
			content: [...event.content, { type: "text", text: `agent-fitness-functions diagnostic:\n${diagnostic}` }],
		};
	});

	pi.on("session_shutdown", () => {
		pendingDiagnostics.clear();
	});

	pi.on("session_start", () => {
		configure();
	});

	configure();
}
