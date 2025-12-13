import { memo } from "react";
import {
    ListTodo,
    ChevronDown,
    CheckCircle2,
    XCircle,
    Loader2,
    Sparkles,
} from "lucide-react";
import type { ToolWidget as ToolWidgetType, TodoItem } from "../../types";
import { cn } from "../../lib/utils";
import { useWidgetExpansion } from "./useWidgetExpansion";
import {
    getWidgetStatusStyles,
    getWidgetContainerClasses,
    getWidgetHeaderClasses,
} from "./widgetStyles";
import { TodoList } from "../Chat/TodoList";

interface TodoWidgetProps {
    widget: ToolWidgetType;
    onExpansionChange?: (expanded: boolean) => void;
    shouldAnimate?: boolean;
}

/**
 * TodoWidget displays a structured list of todos from the todo_write tool.
 */
export const TodoWidget = memo<TodoWidgetProps>(function TodoWidget({
    widget,
    onExpansionChange,
    shouldAnimate = false,
}) {
    const { name, args } = widget.data;
    const status = widget.status;

    // Safe cast args to expected structure
    const argsTodos = (args as any)?.todos as TodoItem[] || [];

    // Try to get full list from result if available (tool returned success)
    let todos = argsTodos;
    if (status === "success" && widget.content) {
        try {
            const result = JSON.parse(widget.content);
            if (result.current_todos && Array.isArray(result.current_todos)) {
                todos = result.current_todos;
            }
        } catch (e) {
            // Ignore parse errors, stick to args
        }
    }

    // Use shared expansion hook
    // Auto-expand if we have todos to show
    const { isExpanded, isActive, isCompleted, handleToggle } =
        useWidgetExpansion({
            widget,
            onExpansionChange,
            autoExpandWhenActive: true,
            activeStatuses: ["working", "success"], // Expand on success too for todos so user sees the list immediately
            completedStatuses: ["failed"],
            collapseDelay: 0, // No auto-collapse for todos
        });

    const statusStyles = getWidgetStatusStyles(status, isCompleted);

    return (
        <div
            className={getWidgetContainerClasses(
                statusStyles,
                isExpanded,
                isCompleted,
            )}
            role="region"
            aria-label={`Todo List: ${name}`}
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
                aria-label={`Toggle todo list details. Status: ${status}`}
            >
                <div className={cn("relative", statusStyles.iconColor)}>
                    {isActive && (
                        <Sparkles
                            size={12}
                            className="absolute -top-1 -right-1 animate-pulse opacity-70"
                        />
                    )}
                    <ListTodo
                        size={isCompleted ? 14 : 16}
                        className={cn(
                            "transition-transform duration-200",
                            shouldAnimate &&
                            "animate-[badgeLifecycle_2s_ease-in-out_infinite]",
                        )}
                    />
                </div>

                <span
                    className={cn("font-medium flex-1 text-sm", statusStyles.textColor)}
                >
                    Task List
                    {todos.length > 0 && <span className="opacity-60 ml-2 text-xs">({todos.length} items)</span>}
                </span>

                <div className="ml-auto flex items-center gap-2">
                    {status === "working" && (
                        <Loader2 size={14} className="animate-spin text-yellow-400" />
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
                    isExpanded ? "max-h-[1000px] opacity-100" : "max-h-0 opacity-0",
                )}
            >
                <div
                    className={cn(
                        "p-3 border-t border-white/10",
                        isCompleted ? "bg-black/10" : "bg-black/30",
                    )}
                >
                    {todos.length > 0 ? (
                        // Render the TodoList component
                        // We wrapped it in a div to match padding/style of other widgets
                        <div className="-ml-4"> {/* TodoList has its own left margin/border we might want to neutralize or adjust */}
                            <TodoList todos={todos} />
                        </div>
                    ) : (
                        <div className="text-gray-500 text-xs italic px-2">No tasks in this list.</div>
                    )}

                    {/* If there was an error or other tool output, we might want to show it? 
              For now let's just show the todos as that is the primary purpose. 
          */}
                </div>

                {/* If status is failed, show error if available in content */}
                {status === 'failed' && widget.content && (
                    <div className="p-3 border-t border-red-500/20 bg-red-900/10 text-red-300 text-xs font-mono">
                        {widget.content}
                    </div>
                )}
            </div>
        </div>
    );
});
