import type {
  Widget,
  ToolWidget,
  ThinkingWidget,
  ApprovalWidget,
  TextWidget,
  ToolWidgetStatus,
} from "../types";
import { handleError } from "./error-handler";
import type { StreamDispatcher } from "./stream-utils";

type Dispatcher = StreamDispatcher;

const TEXT_MARKER_PREFIX = "$$text_marker$$";

/**
 * StreamParser
 *
 * Handles the parsing of Server-Sent Events (SSE) from the Agent to Agent (A2A) protocol.
 *
 * Key Architecture Features:
 * 1. **Dispatch Buffering**: High-frequency text updates (tokens) are buffered internally and flushed
 *    to the Zustand store at a maximum rate of 20fps. This prevents the Main Thread from being blocked
 *    by excessive State Serialization (JSON.stringify) during high-speed streaming.
 *
 * 2. **Text Markers**: Uses `$$text_marker$$` IDs to manage text segments interleaved with other widgets
 *    (Tools, Thinking) within the same message, ensuring correct content ordering.
 *
 * 3. **Read-Consistency**: The parser maintains a local view of the message (`applyPendingBuffer`) to ensure
 *    that logic remains correct even if the Store is slightly behind the stream due to buffering.
 */
export class StreamParser {
  private sessionId: string;
  private messageId: string;
  private currentController: AbortController | null = null;
  private dispatch: Dispatcher;

  // Track created widgets to avoid duplicates
  private createdToolWidgets = new Set<string>();
  private createdThinkingWidgets = new Set<string>();

  // Use local state to track the active text widget ID to prevent fragmentation
  private activeTextWidgetId: string | null = null;
  private activeTextWidgetAuthor: string | null = null;
  private lastDispatchedActiveAgentId: string | null = null;

  // Buffer to batch high-frequency text updates (performance optimization)
  private pendingTextBuffer = new Map<string, string>();
  private flushTimeout: ReturnType<typeof setTimeout> | null = null;
  private readonly FLUSH_INTERVAL_MS = 50; // 20fps cap on store updates

  constructor(sessionId: string, messageId: string, dispatch: Dispatcher) {
    this.sessionId = sessionId;
    this.messageId = messageId;
    this.dispatch = dispatch;
  }

  abort() {
    // Clear pending timeout without flushing (we're aborting, not completing)
    if (this.flushTimeout) {
      clearTimeout(this.flushTimeout);
      this.flushTimeout = null;
    }
    if (this.currentController) {
      this.currentController.abort();
    }
  }

  cleanup() {
    // Clear any pending streaming buffers to prevent memory leaks
    this.pendingTextBuffer.forEach((_, widgetId) => {
      this.dispatch.clearStreamingTextContent(widgetId);
    });
    this.pendingTextBuffer.clear();

    if (this.flushTimeout) {
      clearTimeout(this.flushTimeout);
      this.flushTimeout = null;
    }

    this.createdToolWidgets.clear();
    this.createdThinkingWidgets.clear();
    this.abort();
  }

  // Queue a text update for batch processing
  private queueTextUpdate(widgetId: string, text: string) {
    const current = this.pendingTextBuffer.get(widgetId) || "";
    this.pendingTextBuffer.set(widgetId, current + text);
    this.scheduleFlush();
  }

  private scheduleFlush() {
    if (!this.flushTimeout) {
      this.flushTimeout = setTimeout(
        () => this.flush(),
        this.FLUSH_INTERVAL_MS,
      );
    }
  }

  // Flush pending updates to the store
  private flush() {
    if (this.flushTimeout) {
      clearTimeout(this.flushTimeout);
      this.flushTimeout = null;
    }

    if (this.pendingTextBuffer.size === 0)
      return;

    // Dispatch all batched text updates
    if (this.pendingTextBuffer.size > 0) {
      this.pendingTextBuffer.forEach((text, widgetId) => {
        this.dispatch.appendTextWidgetContent(
          this.sessionId,
          this.messageId,
          widgetId,
          text,
        );
      });
      this.pendingTextBuffer.clear();
    }
  }

  // Apply pending updates to a message object (read-consistent view)
  // This ensures that the parser logic sees the "Full" message content including
  // buffered characters that haven't hit the store yet.
  private applyPendingBuffer(message: any) {
    if (this.pendingTextBuffer.size === 0) return message;

    const newWidgets = message.widgets.map((w: Widget) => {
      if (this.pendingTextBuffer.has(w.id)) {
        const delta = this.pendingTextBuffer.get(w.id)!;
        return {
          ...w,
          content: (w.content || "") + delta,
        };
      }
      return w;
    });

    return {
      ...message,
      widgets: newWidgets,
    };
  }

  public async stream(url: string, requestBody: unknown) {
    this.currentController = new AbortController();

    try {
      const response = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(requestBody),
        signal: this.currentController.signal,
      });

      if (!response.ok) {
        const errorText = await response.text().catch(() => "Unknown error");
        throw new Error(
          "HTTP " + response.status + ": " + errorText.substring(0, 200),
        );
      }

      if (!response.body) {
        throw new Error("No response body");
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";

        for (const line of lines) {
          if (line.startsWith("data: ")) {
            try {
              const data = JSON.parse(line.substring(6));
              this.handleData(data);
            } catch {
              // Ignore parse errors from partial JSON
            }
          }
        }
      }

      this.flush(); // Flush before finalizing
      this.finalizeStream();
      this.dispatch.setIsGenerating(false);
    } catch (error: unknown) {
      this.flush(); // Flush on error
      this.dispatch.setIsGenerating(false);
      if (error instanceof Error && error.name === "AbortError") {
        this.dispatch.updateMessage(this.sessionId, this.messageId, {
          cancelled: true,
        });
      } else {
        handleError(error, "Stream error");
      }
    } finally {
      this.currentController = null;
      this.dispatch.setActiveAgentId(null);
    }
  }

  private handleData(data: unknown) {
    const result =
      (data as { result?: A2AResult })?.result || (data as A2AResult);

    if (result.taskId) {
      this.dispatch.setSessionTaskId(this.sessionId, result.taskId);
    }

    switch (result.kind) {
      case "status-update":
        this.handleStatusUpdate(result);
        break;
      case "artifact-update":
        this.processArtifactUpdate(result);
        break;
      case "task":
        if (result.artifacts) {
          for (const artifact of result.artifacts) {
            this.processArtifactUpdate({ ...result, artifact });
          }
        }
        break;
      default:
        if (result.artifact) {
          this.processArtifactUpdate(result);
        }
    }
  }

  private processArtifactUpdate(result: A2AResult) {
    let rawMessage = this.dispatch.getMessage(this.sessionId, this.messageId);
    if (!rawMessage) return;

    // Apply pending buffer to keep local state consistent with stream
    const message = this.applyPendingBuffer(rawMessage);

    const isPartial = result.metadata?.partial === true;

    const widgetMap = new Map<string, Widget>();
    const contentOrder: string[] = message.metadata?.contentOrder
      ? [...message.metadata.contentOrder]
      : [];

    message.widgets.forEach((w: Widget) => {
      widgetMap.set(w.id, w);
    });

    let accumulatedText = message.text || "";
    let needsFullUpdate = false;

    if (result.metadata?.tool_results) {
      for (const tr of result.metadata.tool_results) {
        if (this.processToolResult(tr, widgetMap)) {
          needsFullUpdate = true;
        }
      }
    }

    if (result.artifact?.parts) {
      for (const part of result.artifact.parts) {
        const author =
          (result.metadata?.author as string) ||
          (result.metadata?.["event_author"] as string);
        if (author && author !== this.lastDispatchedActiveAgentId) {
          this.dispatch.setActiveAgentId(author);
          this.lastDispatchedActiveAgentId = author;
        }

        if (part.kind === "text" && part.text) {
          const result = this.processTextPart(
            part.text,
            accumulatedText,
            widgetMap,
            contentOrder,
            isPartial,
            author,
          );
          accumulatedText = result.text;
          // Always update message for text parts to keep accumulatedText in sync
          // This ensures next SSE event has current text for dedup checks
          if (result.type === "create" || result.type === "append") {
            needsFullUpdate = true;
          }
        } else if (part.kind === "data" && part.data) {
          const data = part.data as Record<string, unknown>;
          needsFullUpdate = true;

          if (data.type === "thinking") {
            const id = data.id as string;
            const content = data.content as string;
            const status = data.status as string;
            const isCompleted = status === "completed";
            this.processThinking(
              id,
              content || "",
              isCompleted,
              "default",
              widgetMap,
              contentOrder,
              author,
            );
          } else if (data.type === "tool_use") {
            const toolId = data.id as string;
            if (!this.createdToolWidgets.has(toolId)) {
              this.processToolCallFromPart(
                data,
                widgetMap,
                contentOrder,
                author,
              );
            }
          } else if (data.type === "tool_result") {
            const toolCallId = data.tool_call_id as string;
            if (
              this.processToolResult(
                {
                  tool_call_id: toolCallId,
                  content: data.content as string,
                  is_error: data.is_error as boolean,
                  status: data.status as string,
                },
                widgetMap,
              )
            ) {
              needsFullUpdate = true;
            }
          }
        }
      }
    }

    // Mark active widgets as completed if stream is done
    if (!isPartial) {
      needsFullUpdate = true;
      widgetMap.forEach((widget: Widget, id: string) => {
        if (widget.type === "thinking" && widget.status === "active") {
          widgetMap.set(id, { ...widget, status: "completed" });
        }
        if (widget.type === "text" && widget.status === "active") {
          widgetMap.set(id, { ...widget, status: "completed" });
        }
      });
    }

    if (needsFullUpdate) {
      this.flush(); // Sync store before full update

      const orderedWidgets: Widget[] = [];
      const seenWidgetIds = new Set<string>();

      contentOrder.forEach((widgetId) => {
        const widget = widgetMap.get(widgetId);
        if (widget) {
          orderedWidgets.push(widget);
          seenWidgetIds.add(widgetId);
        }
      });

      widgetMap.forEach((widget: Widget, id: string) => {
        if (!seenWidgetIds.has(id)) {
          orderedWidgets.push(widget);
        }
      });

      this.dispatch.updateMessage(this.sessionId, this.messageId, {
        text: accumulatedText,
        widgets: orderedWidgets,
        metadata: {
          ...message.metadata,
          contentOrder: contentOrder.length > 0 ? contentOrder : undefined,
        },
      });
    }
  }

  private processTextPart(
    text: string,
    accumulatedText: string,
    widgetMap: Map<string, Widget>,
    contentOrder: string[],
    isPartial: boolean,
    author?: string,
  ): { text: string; type: "create" | "append" } {
    if (!text) return { text: accumulatedText, type: "append" };

    // De-duplication: Check if text already exists in accumulated text (within same artifact)
    if (accumulatedText === text || accumulatedText.endsWith(text)) {
      return { text: accumulatedText, type: "append" };
    }

    // De-duplication: Check existing widgets from same author (cross-event)
    // This handles the case where partial=false resends complete text after partial=true chunks
    for (const [widgetId, widget] of widgetMap) {
      if (widget.type !== "text") continue;

      // Case-insensitive author comparison for consistent handling
      const widgetAuthor = (widget as TextWidget).data?.author?.toLowerCase();
      const currentAuthor = author?.toLowerCase();
      if (widgetAuthor === currentAuthor) {
        const widgetContent = widget.content || "";
        const bufferedContent = this.pendingTextBuffer.get(widgetId) || "";
        const fullContent = widgetContent + bufferedContent;

        // Exact match - text already exists
        if (fullContent === text) {
          return { text: accumulatedText, type: "append" };
        }

        // Common case: partial=false sends complete text that includes partial chunks
        // Example: widget has "ABC" from chunks, partial=false sends "ABCDEF"
        if (
          !isPartial &&
          fullContent.length > 0 &&
          text.startsWith(fullContent)
        ) {
          return { text: accumulatedText, type: "append" };
        }

        // Reverse case: widget has complete text, incoming is a prefix (rare edge case)
        if (fullContent.length > 0 && fullContent.startsWith(text)) {
          return { text: accumulatedText, type: "append" };
        }
      }
    }

    const newAccumulatedText = accumulatedText + text;

    let targetTextWidgetId = this.activeTextWidgetId;
    let shouldUseCached = false;

    // Cache validation: check if active widget is still valid (case-insensitive comparison)
    if (targetTextWidgetId && widgetMap.has(targetTextWidgetId)) {
      const cachedAuthorLower = this.activeTextWidgetAuthor?.toLowerCase();
      const authorLower = author?.toLowerCase();
      if (author && authorLower !== cachedAuthorLower) {
        shouldUseCached = false;
      } else {
        shouldUseCached = true;
      }
    } else {
      shouldUseCached = false;
    }

    if (!shouldUseCached) {
      // Find where to append new text
      const lastNonTextWidgetId = contentOrder
        .filter((id) => {
          const widget = widgetMap.get(id);
          return widget && widget.type !== "text";
        })
        .pop();

      if (lastNonTextWidgetId) {
        targetTextWidgetId =
          TEXT_MARKER_PREFIX + "_after_" + lastNonTextWidgetId;
      } else {
        // Include messageId to ensure uniqueness across messages
        targetTextWidgetId = TEXT_MARKER_PREFIX + "_start_" + this.messageId;
      }

      // If resolving to same author, reuse (case-insensitive comparison)
      if (!lastNonTextWidgetId && contentOrder.length > 0) {
        const lastWidgetId = contentOrder[contentOrder.length - 1];
        const lastWidget = widgetMap.get(lastWidgetId);
        if (lastWidget?.type === "text") {
          const existingAuthor = lastWidget.data.author?.toLowerCase();
          const currentAuthor = author?.toLowerCase();
          if (currentAuthor === existingAuthor) {
            targetTextWidgetId = lastWidgetId;
          }
        }
      }

      // Collision check (case-insensitive comparison)
      const existing = widgetMap.get(targetTextWidgetId as string);
      if (existing && existing.type === "text") {
        const existingAuthorLower = existing.data.author?.toLowerCase();
        const authorLower = author?.toLowerCase();
        if (authorLower !== existingAuthorLower) {
          // Include messageId to ensure uniqueness across messages
          targetTextWidgetId =
            TEXT_MARKER_PREFIX +
            "_" +
            author +
            "_" +
            this.messageId +
            "_" +
            Date.now();
        }
      }

      this.activeTextWidgetId = targetTextWidgetId as string;
      this.activeTextWidgetAuthor = author || null;
    }

    const resolvedId = this.activeTextWidgetId as string;

    if (!widgetMap.has(resolvedId)) {
      // PERFORMANCE: Queue initial text to buffer as well
      this.queueTextUpdate(resolvedId, text);

      const textWidget: TextWidget = {
        id: resolvedId,
        type: "text",
        status: isPartial ? "active" : "completed",
        content: text, // Local state for read-consistency during parsing
        data: { author },
        isExpanded: true,
      };
      widgetMap.set(resolvedId, textWidget);
      if (!contentOrder.includes(resolvedId)) {
        contentOrder.push(resolvedId);
      }
      return { text: newAccumulatedText, type: "create" };
    } else {
      const existing = widgetMap.get(resolvedId);

      if (existing && existing.type === "text") {
        // PERF: BUFFER UPDATE
        this.queueTextUpdate(resolvedId, text);

        const textWidget = existing as TextWidget;
        const newContent = (textWidget.content || "") + text;

        widgetMap.set(resolvedId, {
          ...textWidget,
          content: newContent,
          status: isPartial ? textWidget.status : ("completed" as const),
        });

        return { text: newAccumulatedText, type: "append" };
      }
    }

    return { text: newAccumulatedText, type: "create" };
  }

  private processToolCallFromPart(
    data: Record<string, unknown>,
    widgetMap: Map<string, Widget>,
    contentOrder: string[],
    author?: string,
  ) {
    const id = data.id as string;
    const widgetId = "tool_" + id;

    if (this.createdToolWidgets.has(id) || widgetMap.has(widgetId)) {
      return;
    }

    this.createdToolWidgets.add(id);

    const toolWidget: ToolWidget = {
      id: widgetId,
      type: "tool",
      status: "working",
      content: "",
      data: {
        name: (data.name as string) || "unknown",
        args: (data.arguments || data.input || {}) as Record<string, unknown>,
        author: author,
      },
      isExpanded: true,
    };

    widgetMap.set(widgetId, toolWidget);
    if (!contentOrder.includes(widgetId)) {
      contentOrder.push(widgetId);
    }

    // CRITICAL: Reset active text widget so next text creates a new widget after this tool
    this.activeTextWidgetId = null;
    this.activeTextWidgetAuthor = null;
  }

  private processToolResult(
    tr: ToolResultMeta,
    widgetMap: Map<string, Widget>,
  ): boolean {
    const widgetId = "tool_" + tr.tool_call_id;
    const existing = widgetMap.get(widgetId);
    if (!existing || existing.type !== "tool") return false;

    const newContent =
      typeof tr.content === "string" ? tr.content : JSON.stringify(tr.content);

    const existingContent = existing.content || "";
    const isIncremental =
      existing.status === "working" &&
      existingContent.length > 0 &&
      newContent.length > 0 &&
      !newContent.includes(existingContent);

    // Tool updates are applied directly to ensure real-time responsiveness.
    // Unlike LLM text generation, tool output is typically slower (line-by-line)
    // and benefits from immediate feedback without buffering.

    const updatedContent = isIncremental
      ? existingContent + newContent
      : newContent || existingContent;

    let status: ToolWidgetStatus = "success";
    if (tr.is_error) {
      status = "failed";
    } else if (tr.status === "working") {
      status = "working";
    } else if (tr.status === "failed") {
      status = "failed";
    } else if (isIncremental) {
      status = "working";
    }

    // For status changes or non-incremental updates, do full update
    widgetMap.set(widgetId, {
      ...existing,
      status,
      content: updatedContent,
      isExpanded: status === "working",
    });

    return true; // Trigger full update
  }

  private processThinking(
    id: string,
    content: string,
    isCompleted: boolean,
    type: string | undefined,
    widgetMap: Map<string, Widget>,
    contentOrder: string[],
    author?: string,
  ) {
    const widgetId = "thinking_" + id;

    if (this.createdThinkingWidgets.has(id)) {
      const existing = widgetMap.get(widgetId) as ThinkingWidget | undefined;
      if (existing) {
        const newContent = isCompleted
          ? content
          : (existing.content || "") + content;

        widgetMap.set(widgetId, {
          ...existing,
          content: newContent,
          status: isCompleted ? "completed" : existing.status,
          isExpanded: isCompleted ? false : existing.isExpanded,
        });
      }
      return;
    }

    this.createdThinkingWidgets.add(id);

    const thinkingWidget: ThinkingWidget = {
      id: widgetId,
      type: "thinking",
      status: isCompleted ? "completed" : "active",
      content: content,
      data: {
        type: (type || "default") as "todo" | "goal" | "reflection" | "default",
        author: author,
      },
      isExpanded: !isCompleted,
    };

    widgetMap.set(widgetId, thinkingWidget);
    if (!contentOrder.includes(widgetId)) {
      contentOrder.push(widgetId);
    }

    // CRITICAL: Reset active text widget so next text creates a new widget after thinking
    this.activeTextWidgetId = null;
    this.activeTextWidgetAuthor = null;
  }

  private handleStatusUpdate(result: A2AResult) {
    const state = result.status?.state;
    const message = this.dispatch.getMessage(this.sessionId, this.messageId);
    if (!message) return;

    if (state === "failed") {
      const errorText =
        result.status?.message?.parts?.[0]?.text ||
        "Agent execution failed with unknown error.";

      const alertContent =
        "\n\n> [!CAUTION]\n> **Agent Run Failed**\n> " + errorText + "\n";

      this.dispatch.updateMessage(this.sessionId, this.messageId, {
        text: (message.text || "") + alertContent,
      });
      return;
    }

    if (state !== "input-required" && state !== "input_required") return;

    const widgetMap = new Map<string, Widget>();
    const contentOrder: string[] = message.metadata?.contentOrder
      ? [...message.metadata.contentOrder]
      : [];

    message.widgets.forEach((w) => {
      widgetMap.set(w.id, w);
    });

    const taskId = result.taskId;
    const toolCallIDs = result.metadata?.long_running_tool_ids || [];
    const inputPrompt =
      result.metadata?.input_prompt || "Human input required.";
    const widgetId = "approval_" + (taskId || this.messageId);

    if (widgetMap.has(widgetId)) return;

    let toolName = "Unknown Tool";
    let toolInput: Record<string, unknown> = {};

    if (toolCallIDs.length > 0) {
      for (const toolCallID of toolCallIDs) {
        const toolWidgetId = "tool_" + toolCallID;
        const toolWidget = widgetMap.get(toolWidgetId);
        if (toolWidget && toolWidget.type === "tool") {
          toolName = toolWidget.data.name || "Unknown Tool";
          toolInput = toolWidget.data.args || {};
          break;
        }
      }
    }

    const approvalWidget: ApprovalWidget = {
      id: widgetId,
      type: "approval",
      status: "pending",
      content: inputPrompt,
      data: {
        toolName: toolName,
        toolInput: toolInput,
        task_id: taskId || undefined,
        tool_call_ids: toolCallIDs,
        prompt: inputPrompt,
      },
      isExpanded: true,
    };

    widgetMap.set(widgetId, approvalWidget);
    if (!contentOrder.includes(widgetId)) {
      contentOrder.push(widgetId);
    }

    // CRITICAL: Reset active text widget so next text creates a new widget after approval
    this.activeTextWidgetId = null;
    this.activeTextWidgetAuthor = null;

    const orderedWidgets: Widget[] = [];
    contentOrder.forEach((id) => {
      const w = widgetMap.get(id);
      if (w) orderedWidgets.push(w);
    });

    this.dispatch.updateMessage(this.sessionId, this.messageId, {
      widgets: orderedWidgets,
      metadata: {
        ...message.metadata,
        contentOrder: contentOrder.length > 0 ? contentOrder : undefined,
      },
    });
  }

  private finalizeStream() {
    const message = this.dispatch.getMessage(this.sessionId, this.messageId);
    if (!message) return;

    // PERFORMANCE: Finalize all streaming text widgets (commit buffer to message)
    const textWidgets = message.widgets.filter((w) => w.type === "text");
    textWidgets.forEach((widget) => {
      this.dispatch.finalizeStreamingText(
        this.sessionId,
        this.messageId,
        widget.id,
      );
    });

    const hasActiveWidgets = message.widgets.some(
      (w) =>
        (w.type === "thinking" || w.type === "text") && w.status === "active",
    );

    if (hasActiveWidgets) {
      const updatedWidgets = message.widgets.map((w) =>
        (w.type === "thinking" || w.type === "text") && w.status === "active"
          ? { ...w, status: "completed" as const }
          : w,
      );
      this.dispatch.updateMessage(this.sessionId, this.messageId, {
        widgets: updatedWidgets,
      });
    }
  }
}

interface A2APart {
  kind: string;
  text?: string;
  data?: unknown;
}

interface A2AArtifact {
  artifactId: string;
  parts: A2APart[];
}

interface A2AResult {
  kind?: "status-update" | "artifact-update" | "task";
  taskId?: string;
  status?: {
    state: string;
    message?: {
      parts?: { text: string }[];
    };
  };
  artifact?: A2AArtifact;
  artifacts?: A2AArtifact[];
  metadata?: {
    partial?: boolean;
    thinking?: ThinkingMeta;
    tool_calls?: ToolCallMeta[];
    tool_results?: ToolResultMeta[];
    long_running_tool_ids?: string[];
    input_prompt?: string;
    [key: string]: unknown;
  };
}

interface ThinkingMeta {
  id: string;
  status: "active" | "completed";
  content: string;
  type?: string;
}

interface ToolCallMeta {
  id: string;
  name: string;
  args?: Record<string, unknown>;
  status?: string;
}

interface ToolResultMeta {
  tool_call_id: string;
  content: string | Record<string, unknown>;
  status?: string;
  is_error?: boolean;
}
