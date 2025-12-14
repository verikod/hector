import React from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Bot } from "lucide-react";
import { cn } from "../../../lib/utils";
import { useStore } from "../../../store/useStore";
import { getAgentColor, getAgentColorClasses } from "../../../lib/colors";

interface AgentNodeData extends Record<string, unknown> {
  label: string;
  agentId?: string;
  llm?: string;
  description?: string;
  instruction?: string;
}

export const AgentNode: React.FC<NodeProps> = ({ data, selected }) => {
  const nodeData = data as AgentNodeData;
  const activeAgentId = useStore((state) => state.activeAgentId);

  // Compare against agentId (internal name) first, fallback to label for backwards compatibility
  // Case-insensitive comparison - backend author names may differ in casing from YAML config
  const compareId = nodeData.agentId || nodeData.label;
  const isActive =
    activeAgentId?.toLowerCase() === compareId?.toLowerCase();

  // Get dynamic colors based on agent name
  const agentColor = getAgentColor(nodeData.label);
  const colors = getAgentColorClasses(agentColor);

  return (
    <div className="relative group/node">
      {/* Active Pulse Effect - Intensified */}
      {isActive && (
        <div className="absolute -inset-4 rounded-xl bg-gradient-to-r from-blue-500 via-purple-500 to-pink-500 blur-xl opacity-60 animate-pulse transition duration-1000 group-hover/node:opacity-80 group-hover/node:duration-200 pointer-events-none"></div>
      )}

      <div
        className={cn(
          "px-4 py-3 shadow-2xl rounded-xl border transition-all min-w-[220px] relative overflow-visible backdrop-blur-xl",
          // Dynamic Colors
          colors.bg,
          colors.border,
          isActive
            ? "scale-110 z-50 ring-4 ring-white/60 brightness-110 translate-y-[-2px]"
            : selected
              ? "ring-2 ring-white/30"
              : "hover:border-white/50 hover:scale-105 hover:z-40",
        )}
      >
        {/* Handles - Hidden (opacity-0) but functional if needed for edge rendering */}
        <Handle
          type="target"
          position={Position.Top}
          id="top"
          className="w-2 h-2 !opacity-0 !pointer-events-none !-top-2"
        />
        <Handle
          type="target"
          position={Position.Left}
          id="left"
          className="w-2 h-2 !opacity-0 !pointer-events-none !-left-2"
        />

        {/* Header */}
        <div className="flex items-center gap-3 mb-3 relative z-10">
          <div
            className={cn(
              "w-9 h-9 rounded-lg flex items-center justify-center shadow-inner border border-white/20 transition-transform duration-500",
              colors.bg,
              isActive && "rotate-6 scale-110",
            )}
          >
            <Bot size={20} className="text-white/90" />
          </div>
          <div className="flex-1 min-w-0">
            <div
              className={cn(
                "font-bold truncate tracking-tight text-white text-base shadow-black/50 drop-shadow-sm",
              )}
            >
              {nodeData.label}
            </div>
            <div className="text-[10px] uppercase tracking-wider font-bold opacity-70 text-white flex items-center gap-1">
              Agent
              {isActive && (
                <span className="w-1.5 h-1.5 rounded-full bg-green-400 animate-ping inline-block ml-1" />
              )}
            </div>
          </div>
        </div>

        {/* LLM Info */}
        <div className="flex items-center justify-between text-xs text-white/50 font-mono bg-black/30 rounded px-2.5 py-1.5 border border-white/5 relative z-10 shadow-inner">
          <span className="truncate max-w-[140px] opacity-80 group-hover/node:opacity-100 transition-opacity">
            {nodeData.llm || "default"}
          </span>
        </div>

        <Handle
          type="source"
          position={Position.Right}
          id="right"
          className="w-2 h-2 !opacity-0 !pointer-events-none !-right-2"
        />
        <Handle
          type="source"
          position={Position.Bottom}
          id="bottom"
          className="w-2 h-2 !opacity-0 !pointer-events-none !-bottom-2"
        />
      </div>
    </div>
  );
};
