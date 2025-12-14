import { memo, useCallback } from "react";
import {
  Wrench,
  ChevronDown,
  CheckCircle2,
  XCircle,
  Loader2,
  Sparkles,
  StopCircle,
} from "lucide-react";
import type { ToolWidget as ToolWidgetType } from "../../types";
import { cn } from "../../lib/utils";
import { useWidgetExpansion } from "./useWidgetExpansion";
import {
  getWidgetStatusStyles,
  getWidgetContainerClasses,
  getWidgetHeaderClasses,
} from "./widgetStyles";
import { useAutoScroll } from "./useAutoScroll";

interface ToolWidgetProps {
  widget: ToolWidgetType;
  taskId?: string | null;
  onExpansionChange?: (expanded: boolean) => void;
  shouldAnimate?: boolean;
}

/**
 * ToolWidget displays tool execution status with expandable input/output.
 * Memoized to prevent re-renders when parent updates but widget props unchanged.
 */
export const ToolWidget = memo<ToolWidgetProps>(function ToolWidget({
  widget,
  taskId,
  onExpansionChange,
  shouldAnimate = false,
}) {
  const { name, args } = widget.data;
  const status = widget.status;

  // Use shared expansion hook - tools only auto-expand when actively working/streaming
  // They don't auto-expand by default, only when status changes to 'working'
  const { isExpanded, isActive, isCompleted, handleToggle } =
    useWidgetExpansion({
      widget,
      onExpansionChange,
      autoExpandWhenActive: true, // Auto-expand when actively working (for streaming visibility)
      activeStatuses: ["working"], // Only 'working', not 'pending' - we want to see progress
      completedStatuses: ["success", "failed"],
      collapseDelay: 4000, // 4 seconds
    });

  const statusStyles = getWidgetStatusStyles(status, isCompleted);

  // Auto-scroll result content when streaming
  const resultContentRef = useAutoScroll<HTMLPreElement>(
    widget.content,
    isActive && !!widget.content,
    isExpanded && isActive,
  );

  // Cancel tool execution handler - uses task-scoped endpoint
  const handleCancel = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation(); // Don't toggle expansion
      if (!taskId) {
        console.warn("No taskId available for cancellation");
        return;
      }
      try {
        await fetch(`/api/tasks/${taskId}/toolCalls/${widget.id}/cancel`, {
          method: "POST",
        });
      } catch (error) {
        console.error("Failed to cancel tool:", error);
      }
    },
    [taskId, widget.id],
  );

  return (
    <div
      className={getWidgetContainerClasses(
        statusStyles,
        isExpanded,
        isCompleted,
      )}
      role="region"
      aria-label={`Tool: ${name}`}
    >
      <div
        className={getWidgetHeaderClasses(statusStyles, isActive)}
        onClick={handleToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            handleToggle();
          }
        }}
        role="button"
        tabIndex={0}
        aria-expanded={isExpanded}
        aria-label={`Toggle ${name} tool details. Status: ${status}`}
      >
        <div className={cn("relative", statusStyles.iconColor)}>
          {isActive && (
            <Sparkles
              size={12}
              className="absolute -top-1 -right-1 animate-pulse opacity-70"
            />
          )}
          <Wrench
            size={isCompleted ? 14 : 16}
            className={cn(
              "transition-transform duration-200",
              shouldAnimate &&
              "animate-[badgeLifecycle_2s_ease-in-out_infinite]",
              isExpanded && !isCompleted && "rotate-12",
            )}
          />
        </div>

        <span
          className={cn("font-medium flex-1 text-sm", statusStyles.textColor)}
        >
          Tool: {name}
        </span>

        <div className="ml-auto flex items-center gap-2">
          {status === "working" && (
            <>
              <Loader2 size={14} className="animate-spin text-yellow-400" />
              <button
                onClick={handleCancel}
                className="p-0.5 rounded hover:bg-white/10 transition-colors"
                title="Cancel tool execution"
                aria-label="Cancel tool execution"
              >
                <StopCircle
                  size={14}
                  className="text-red-400 hover:text-red-300"
                />
              </button>
            </>
          )}
          {status === "success" && (
            <CheckCircle2
              size={14}
              className="text-green-500 transition-all duration-300"
            />
          )}
          {status === "failed" && (
            <XCircle
              size={14}
              className="text-red-500 transition-all duration-300"
            />
          )}

          <ChevronDown
            size={14}
            className={cn(
              "transition-transform duration-300 text-gray-400",
              isExpanded ? "rotate-0" : "-rotate-90",
            )}
          />
        </div>
      </div>

      <div
        className={cn(
          "overflow-hidden transition-all duration-300 ease-in-out",
          isExpanded ? "max-h-[400px] opacity-100" : "max-h-0 opacity-0",
        )}
      >
        <div
          className={cn(
            "p-3 space-y-2 border-t border-white/10",
            isCompleted ? "bg-black/10" : "bg-black/30",
          )}
        >
          {/* Input */}
          <div className="space-y-2">
            <div className="text-xs font-semibold text-gray-400 uppercase tracking-wider">
              Input
            </div>
            <pre
              className={cn(
                "bg-black/60 p-3 rounded-lg overflow-x-auto text-xs max-h-[120px] overflow-y-auto",
                "border border-white/10",
                "text-gray-300 font-mono leading-relaxed",
                "scrollbar-thin scrollbar-thumb-white/20 scrollbar-track-transparent",
              )}
            >
              {JSON.stringify(args, null, 2)}
            </pre>
          </div>

          {/* Output - Show even when streaming (working status) */}
          {(widget.content || (isActive && status === "working")) && (
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <div className="text-xs font-semibold text-gray-400 uppercase tracking-wider">
                  Result
                </div>
                {status === "working" && widget.content && (
                  <span className="text-xs text-yellow-400 animate-pulse">
                    Streaming...
                  </span>
                )}
              </div>
              <pre
                ref={resultContentRef}
                className={cn(
                  "bg-black/60 p-3 rounded-lg overflow-x-auto text-xs max-h-[200px] overflow-y-auto",
                  "border border-white/10",
                  "font-mono leading-relaxed",
                  "scrollbar-thin scrollbar-thumb-white/20 scrollbar-track-transparent",
                  status === "failed"
                    ? "text-red-300 border-red-500/20"
                    : status === "working"
                      ? "text-yellow-300 border-yellow-500/20"
                      : "text-green-300 border-green-500/20",
                )}
              >
                {widget.content ||
                  (status === "working" ? "Waiting for result..." : "")}
              </pre>
            </div>
          )}
        </div>
      </div>
    </div>
  );
});
