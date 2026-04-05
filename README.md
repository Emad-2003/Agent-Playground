# Agent Playground

This repository is centered on a CLI-first coding system that aims to give users real freedom in orchestration, execution style, and agent collaboration.

The core idea is simple: instead of forcing one rigid agent loop, the system should let users run direct actions, delegated subagents, staged tool workflows, and future orchestration modes from the same environment. The long-term outcome is a coding assistant that can shift between operator, coordinator, and researcher without losing context.

## Final Outcome

- A practical coding CLI built around `crwlr`, with strong session management, tool execution, model routing, and subagent-style delegation.
- A system where orchestration is flexible rather than fixed: one task may run directly, another may branch into specialized workers, and another may stay under tight user approval.
- A platform for permission-aware research workflows, where new behaviors are proposed first and executed only after approval.

## Local Observer Model

One of the main ideas for the project is a local companion model that acts like a student-observer for development work.

It watches how work evolves over time, tracks repeated patterns, and builds local memory from sessions without replacing the main coding agent. If it notices a meaningful pattern, gap, or promising line of thought, it does not act silently. It asks permission to start a deeper research pass.

That makes it useful for future innovation workflows. For example, if you are solving DSA problems with the system, the local observer can watch the solution process, detect recurring constructions or invariants, and then propose research directions such as:

- generating a genuinely new DSA problem,
- exploring a new variant of an existing pattern,
- testing whether a recurring strategy can be formalized into a reusable method.

The goal is not just assistance with current tasks, but a path toward future research and invention driven by observed patterns from real development sessions.

## Current Focus

The primary active implementation lives under [crawler-ai/README.md](crawler-ai/README.md), where `crwlr` is the installable CLI entrypoint.

