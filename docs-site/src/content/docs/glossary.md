---
title: Glossary
description: Standardized terminology for the Scion project.
---

This glossary defines key terms used throughout the Scion documentation and ecosystem.

### Agent
An isolated worker instance running an LLM harness. Each agent has its own identity, workspace, and configuration.

### Grove
A project-level grouping of agents and configuration, typically corresponding to a git repository and a `.scion` directory.

### Harness
An adapter that allows an underlying LLM tool (like Gemini CLI or Claude Code) to run within the Scion orchestration layer.

### Hub
The centralized control plane in a hosted Scion deployment. It manages identity, grove registration, and dispatches tasks to Runtime Brokers.

### Profile
A set of configuration overrides that define how a runtime should execute an agent (e.g., resource limits, environment variables).

### Runtime
The underlying technology used to execute agent containers (e.g., Docker, Podman, Apple Virtualization, Kubernetes).

### Runtime Broker
A compute node that executes agents. It connects to a Hub to receive instructions and reports agent status.

### sciontool
A helper utility bundled with Scion that is injected into agent containers to provide status reporting, metadata access, and task management.

### Template
A versioned blueprint for an agent, defining its base image, system prompt, tools, and initial state.

### Workspace
The working directory mounted into an agent container, typically managed as a Git worktree to ensure isolation from other agents.