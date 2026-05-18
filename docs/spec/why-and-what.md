---
title: "CALM PoC: Why and What"
aliases: ["CALM PoC: Why and What"]
linter-yaml-title-alias: "CALM PoC: Why and What"
date created: Sunday, May 18th 2026
date modified: Sunday, May 18th 2026
---

# CALM PoC: Why and What

> Engineering detail: [engineering-spec.md](engineering-spec.md)
> Original concept: [CALM: The Architectural Compass for AI-Driven Development](../../../../ObsidianNotes/Work/drafts/Week%20of%2020260510/CALM%20AaC.md)

---

## The Problem

AI coding agents produce working code that violates architectural intent. No deterministic mechanism today prevents an agent from increasing cyclomatic complexity beyond acceptable bounds, exposing too many internal details through a module's public interface, or reimplementing utilities already present in the codebase. Structural debt accumulates silently until a human reviews it — too late to prevent.

Static linters catch syntax and style. They do not enforce architectural fitness. Architecture diagrams document intent. They do not stop code that contradicts them. The gap between architectural intent and produced code grows with every agent-assisted PR.

The same problem applies to human developers, who introduce the same violations under time pressure or without architectural awareness.

---

## The Approach

This PoC uses the **FINOS Common Architecture Language Model (CALM)** as a machine-readable rules layer. Architectural fitness functions are defined as CALM patterns — JSON Schema rules that describe what "good" looks like for each module in the system.

A custom bridge, `calm-bridge`, analyzes source code, translates metrics into a CALM-compliant architecture document, and calls the FINOS `calm` CLI validator. The validator returns a pass or a structured list of violations. Hooks at two interception points — Claude Code pre-tool-use and git pre-commit — capture proposed changes before they land, and either block with explanation or advise, based on per-repository enforcement configuration.

The result: architectural violations become as visible and actionable as syntax errors — before the code is written or committed.

---

## Goals

- Prove that CALM fitness functions can intercept AI agent writes in real time and trigger self-correction without human intervention.
- Provide the same guardrails to human developers via git pre-commit hooks — the same rules, the same enforcement.
- Demonstrate malleable enforcement: block mode for mature repositories, advisory mode for repositories being onboarded — from a single shared rule set.
- Establish a baselining workflow that calibrates thresholds to existing code before enforcement begins, avoiding false positives on day one.
- Produce a repeatable, scripted manual demonstration that validates each fitness function independently.

---

## Non-Goals

- This PoC does not ship a production-ready tool. It validates the workflow.
- This PoC does not define organizational architecture policies. It demonstrates generic, adaptable patterns.
- This PoC does not implement all six CALM metrics from the original concept document. It implements three: Cyclomatic Complexity, Deep vs. Shallow, and AI Slop.
- This PoC does not replace code review. It augments it with pre-review automated enforcement.

---

## Success Criteria

### Must Pass

1. An agent self-corrects a cyclomatic complexity violation without human intervention — at least once per block-mode repository.
2. The git pre-commit hook blocks a commit containing a violation — at least once per language.
3. Advisory mode (`ringstation`) emits a structured guidance message the agent visibly acts on.

### Should Pass

4. Violation count in a repository trends down across three or more consecutive agent task iterations.
5. Bridge latency stays at or below 500 ms for Python and Go; at or below 2 s for C# on the synchronous path.
6. Zero false positives fire against existing clean code in each repository at PoC start (post-baseline).

### Manual Demo

7. Scripted red-green demonstration per fitness function: introduce a known violation → hook blocks → fix → commit succeeds. Run in both enforcement modes.

### Stretch

8. An agent chooses a path change — not a refactor — when a cyclomatic complexity violation cannot be resolved within the configured limit.
9. LDR and DDC flag at least one AI-generated hollow file during a real agent task run.

---

## The Four Test Repositories

| Repository | Language | Enforcement Mode |
|---|---|---|
| `StackOverflow/StackOverflow.Api.V3` | C# | Block + explanation |
| `SlackStatus` | C# | Block + explanation |
| `graft` | Go | Block + explanation |
| `ringstation` | Python 3.12 | Advisory |

---

*Authored By Peter O'Connor with Assistance from Claude Code (databricks-claude-sonnet-4-6) · 2026-05-18 · CALM PoC — Why and What*
