Yes. The pattern I’d use is a fitness-function language server with gradient feedback, rather than a pass/fail pre-commit gate.
Your current system is acting like a quality gate. What you want is more like an architectural typechecker that runs continuously and tells the developer or agent: “you are moving closer to a violation,” not merely “you violated it.” This fits well with architectural fitness-function thinking, where fitness functions measure how closely architecture aligns with desired qualities, not only whether code passes a binary rule.  
The most practical implementation pattern is:
Fitness Function Language Server
Wrap your architectural fitness engine behind an LSP server or editor plugin.
Language Server Protocol already has the right UX primitive: diagnostics with ranges, severities, and messages. That lets you surface architectural feedback as hints, warnings, or errors directly in the editor, similar to type errors or lint warnings.  
Instead of only returning:
Blocked: function A duplicates B and C.
you return earlier feedback like:
processInvoiceDraft is 82% structurally similar to processInvoice; project threshold is 90%. Continuing this shape is likely to trigger the duplication rule.
That is the “friction” signal.
The important shift: from verdicts to distance
Each rule needs two forms:
1. Hard ruleUsed in CI, pre-commit, or pre-push.
2. Distance functionUsed while coding.
For example:
Function duplication
Hard rule:
Block if normalized AST similarity is above 0.90 for multiple functions.
Realtime friction:
{
  "rule": "near-duplicate-function",
  "severity": "warning",
  "confidence": 0.86,
  "currentScore": 0.82,
  "threshold": 0.90,
  "trend": "+0.11 over last 4 edits",
  "message": "This function is becoming structurally similar to calculateRefund(). Consider parameterizing the variation instead of copying the shape."
}
Deep interfaces, not wide interfaces
Hard rule:
Block if interface surface area exceeds usefulness/abstraction depth threshold.
Realtime friction:
{
  "rule": "wide-interface",
  "severity": "hint",
  "currentScore": 0.68,
  "threshold": 0.80,
  "message": "This interface gained 4 public methods but the implementations remain shallow. Consider splitting commands from queries or hiding lower-level operations."
}
The user does not need to know the math. They need to feel the architectural pressure early.
I’d model this as an “architectural friction score”
Think of every rule as producing a scalar value:
0.00 = no concern
0.50 = mild architectural smell
0.75 = likely future violation
0.90 = pre-commit failure threshold
1.00 = hard violation
Then give feedback based on both absolute score and direction of travel.
A file or symbol with score 0.72 may not be terrible. But if it was 0.31 ten minutes ago, that is useful signal.
The most valuable message is often not:
This is bad.
It is:
The last three edits moved this code toward the duplication rule.
That is the “friction gradient.”
For agents, expose it as a planning tool
For human developers, LSP/editor diagnostics are probably enough.
For coding agents, I’d expose a small set of tools:
analyzeDiff(diff): FitnessDiagnostic[]

simulatePatch(patch): FitnessDiagnostic[]

getArchitectureContext(symbol): ArchitecturalContext

suggestRefactor(diagnostic): RefactorOption[]
Then instruct the agent to call analyzeDiff after each patch or before creating new abstractions.

The agent loop becomes:
plan → edit → fitness check → adjust → edit → fitness check → commit
instead of:
plan → huge edit → pre-commit failure → rewrite
This is especially useful because agents tend to duplicate shape, over-generalize abstractions, and create wide interfaces unless they receive tight feedback.
Useful severity levels
I would not make every signal noisy. Use levels like:
Level	Meaning	UX
Hint	Directional concern	Gray underline or side-panel note
Info	Architectural smell emerging	Non-blocking diagnostic
Warning	Close to threshold	Yellow diagnostic
Error	Same as CI/pre-commit failure	Red diagnostic
For agents, you can make the same levels actionable:
hint = note in context
info = consider alternative
warning = revise before continuing
error = must fix
The key design principle
Do not just report violations. Report architectural trajectory.
That means storing a little temporal state:
symbol score before edit
symbol score after edit
delta
nearest violated rule
nearest existing similar construct
suggested lower-friction path
For example:
PaymentService grew from 7 to 13 public methods.
The interface-depth score moved from 0.42 to 0.76.
At 0.80 this rule becomes a warning; at 0.90 it blocks.
Most added methods are orchestration helpers already used only by StripePaymentService.
Suggested direction: keep those methods private or extract PaymentWorkflow.
That is far more useful than a delayed gate.
Name I’d give this pattern
Several names would be reasonable:
Continuous Architectural DiagnosticsBest general name.
Fitness Function Language ServerBest implementation name.
Architectural Friction MeterBest product/UX name.
Gradient Fitness FunctionsBest technical name.
Your existing system already has the hard part: objective architectural checks. The next layer is to make each check emit a proximity score, a trend, and a localized recommendation, then surface that through LSP/editor diagnostics or agent tool calls before the code reaches pre-commit.
