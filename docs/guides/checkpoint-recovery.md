# Checkpoint Recovery

Hector provides a robust checkpoint and recovery system to ensure your agents are fault-tolerant and can resume execution after interruptions, crashes, or manual stops.

## Overview

The Checkpoint system captures the state of an agent at critical points during its execution. This allows you to:

*   **Recover from crashes**: Resume execution exactly where it left off.
*   **Handle interruptions**: Stop a long-running task and resume it days later.
*   **Enable Human-in-the-Loop (HITL)**: Persist state while waiting for human approval.
*   **Debug**: Inspect the exact state of an agent at a specific point in time.

## How It Works

Hector v2 uses a **Session Event Sourcing** architecture for checkpoints. Instead of serializing complex internal memory structures (which can be fragile), Hector effectively "replays" the session history to rebuild the agent's state.

When an agent resumes:
1.  It loads the session history from the database.
2.  It re-hydrates its context (memory, variables) from the events.
3.  It determines the next step based on the last recorded event.

This approach is highly robust and compatible across version upgrades.

## Configuration

Enable checkpointing in your `config.yaml`:

```yaml
storage:
  checkpoint:
    enabled: true
    strategy: event
    recovery:
      auto_resume: true
```

### Strategies

The `strategy` setting controls *when* checkpoints are saved:

| Strategy | Description | Recommended For |
| :--- | :--- | :--- |
| **`event`** | Saves after every agent turn (Post-LLM). | **Production**. Safest and most consistent. |
| **`interval`** | Saves every `N` iterations (plus events). | Long-running loops or very expensive steps. |
| **`hybrid`** | Combines both strategies. | Complex workflows needing maximum safety. |

### Modifiers

You can fine-tune triggers with these flags:

*   **`after_tools: true`**: Save immediately after a tool execution finishes.
    *   *Why?* If a tool takes 5 minutes to run (e.g., a scrape) and the server crashes, you don't want to re-run it.
*   **`before_llm: true`**: Save right before calling the LLM.
    *   *Why?* Useful for debugging prompt generation issues.

### Recovery Behavior

Control what happens when the server restarts:

*   **`auto_resume`**: If `true`, the server automatically attempts to resume any "pending" tasks on startup.
*   **`auto_resume_hitl`**:
    *   `true`: Automatically resumes tasks that were paused for **Human-in-the-Loop** (HITL) approval. It checks if you've since approved/denied the action and proceeds.
    *   `false`: Paused tasks remain paused until you manually trigger them (security best practice for sensitive actions).

## Human-in-the-Loop (HITL)

Checkpointing is critical for HITL workflows (`input_required`):

1.  Agent wants to run a sensitive tool (e.g., `deploy_production`).
2.  It pauses execution and creates a **Checkpoint**.
3.  It sends a notification requesting approval.
4.  Server can be restarted, upgraded, or stopped.
5.  When you approve the action (via Studio or API), the agent resumes from the checkpoint and executes the tool.

## Storage Backends

Checkpoints are stored in your configured `storage.sessions` backend:

*   **SQL (PostgreSQL/MySQL)**: Recommended for production.
*   **InMemory**: Good for testing, but checkpoints are lost on restart.

## Architecture Notes

*   **Stateless Recovery**: Because recovery relies on event sourcing, you can safely upgrade Hector versions without "breaking" old checkpoints, as long as the event history format remains compatible.
*   **Performance**: Checkpoints are lightweight (metadata references), so saving them frequently (`event` strategy) has negligible performance impact.
