# Control plane design

## Current state
as of git commit 26b284cfb6ffb588e635ffebdde85b0979ea9769 the control plane is based entirely on local CLI usage and the filesystem.

There is initial feature in kubernetes to watch a pod

## v1 complted goal desired state

 - A setting provides an endpoint for a control plane service API
 - agent status and logs are synced to this service
 - the service provides agent config and state pesistance in a configurable datastore (with multiple database harness backends supports)
 - the data model of the service supports the grove and agent names, and adds metadata such as user (user of the tool), start time, etc
 - the service offers lifecycle management of agents (see related doc on programmatic access)
 - the service has a module that provides a web UI

## Recommended Implementation Path

Based on the current state, the recommended path to achieve the v1 goals is to prioritize the **Programmatic Control Design** before advancing the Kubernetes execution plane.

1.  **Refactor Core Logic (`pkg/agent`)**: Decouple the agent lifecycle logic from `cmd/` into a reusable Go library (`pkg/agent`). This defines the internal API.
2.  **Define Data Model**: Formalize the structures for Agents, Groves, and Events within `pkg/agent` to support serialization and metadata requirements.
3.  **Service Prototype**: Build a standalone HTTP/gRPC service that wraps `pkg/agent`, exposing the API defined in step 1.
4.  **CLI Integration**: Update the CLI to support a "Remote Mode" that targets this service, alongside the existing "Local Mode".
5.  **Kubernetes & Persistence**: Once the control plane (Management) layer is stable, expand the Execution layer to support Kubernetes and integrate database backends for persistence.




