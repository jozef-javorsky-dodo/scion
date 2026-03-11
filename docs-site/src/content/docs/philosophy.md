---
title: Philosophy
description: Thoughts on the philosophy behind Scion.
---

# Scion Philosophy

This document outlines some of the core principles and philosophy that guide the development and operation of Scion.

## Principles

### Less is More

As stated in the readme, as the frontier models capabilities continue to improve, they will become more capable of taking higher level intent and deciding more complex ways of fulfilling it. This means that the explicit structure of complex harnesses, protocols, etc may matter less than open and flexible substrates for agents to collaborate in. To this end Scion is not attempting to be the full stack solution for multi-agent solutions. It focuses on being a "hypervisor for agents". Multi-agent system components such as agent memory, agent chatrooms, task management can be integrated as orthogonal concerns to be integrated into a solution that uses Scion.

Part of this improvement in agents and models is that agents, in the right environment, are getting better at learning as needed. Concretely, in Scion, agents are able to use **Progressive Skills** by using the `scion --help` command to dynamically learn how to use the tool, which demonstrates another step in the evolution from MCP -> SKILLS.md -> `<cli> --help` as a technique.

### Isolation Over Constraints

For agents to be effective, they need to operate with agency. This can come from some balance of giving the agent limitations and rules INSIDE the agent context, or by giving it the go ahead to do everything it can, and then guardrail it OUTSIDE the agent. Scion favors running agents in `--yolo` mode, while isolating them in containers, git worktrees, and on compute nodes subject to network policy at the infrastructure layer.

### Interaction is imperative

Larger complex projects need collaboration. Expecting agents and workflows to proceed to completion without interaction is unreasonable. This means allowing humans to interact directly with the interactive mode of harnesses, as well as providing the means for agents to interact with each other "as users".

### Diversity results in higher quality

Specialization through system prompts, model vendors, model sizes, harnesses and configurations all bring an ecosystem of strengths and weaknesses. Complex multi-agent solutions should be able to leverage a blend of strengths. Scion attempts to be balanced between agnostic without being reductive.

### Agents lifecycles are dynamic

The graph of an agent swarm and the tasks it works through is dynamic and not practical to determine in advance. Agents span those that are specialized and long lived, or highly ephemeral and coupled to just one task. The mix of these agents may change dynamically as the new requirements, challenges, and details are flushed out from earlier stages of a project.

### Action over pondering

We are in a period of rapid discovery and experimentation. The hypotheses about "How" agents **should** work together vastly outnumbers the projects that have attempted or demonstrated what actually happens in practice. Scion aims to be a testbed to make such experiments simpler and more practical to explore.
