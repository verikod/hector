import * as yaml from 'js-yaml';
import type { Node, Edge } from '@xyflow/react';

interface Config {
  version?: string;
  name?: string;
  llms?: Record<string, any>;
  agents?: Record<string, any>;
  databases?: Record<string, any>;
  embedders?: Record<string, any>;
  vector_stores?: Record<string, any>;
  [key: string]: any;
}

export interface GraphData {
  nodes: Node[];
  edges: Edge[];
}

/**
 * Converts YAML config to React Flow visualization
 * Optimized layout with proper workflow group sizing
 */
export const yamlToGraph = (yamlContent: string): GraphData => {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  try {
    const config = yaml.load(yamlContent) as Config;

    if (!config || !config.agents) {
      return { nodes, edges };
    }

    // Constants
    const NODE_WIDTH = 220;
    const NODE_HEIGHT = 80;
    const HORIZONTAL_GAP = 80;
    const VERTICAL_GAP = 60;
    const GROUP_PADDING = 60;

    // Track processed agents to avoid duplicates
    const processedAgents = new Set<string>();

    let currentY = 50;

    // Helper to calculate dimensions recursively
    const getLayout = (agentId: string, type?: string): { width: number; height: number; childNodes: string[] } => {
      const agent = config.agents![agentId];
      const subAgents = agent?.sub_agents || [];
      const isWorkflow = type && ['sequential', 'parallel', 'loop'].includes(type);

      if (!isWorkflow || subAgents.length === 0) {
        return { width: NODE_WIDTH, height: NODE_HEIGHT, childNodes: [] };
      }

      // Calculate dimensions based on children
      let width = 0;
      let height = 0;

      if (type === 'sequential') {
        // Sum of widths + gaps
        const childrenDimensions = subAgents.map((sub: string) =>
          getLayout(sub, config.agents![sub]?.type)
        );
        width = childrenDimensions.reduce((acc: number, dim: any) => acc + dim.width, 0) +
          ((subAgents.length - 1) * HORIZONTAL_GAP) + (GROUP_PADDING * 2);
        height = Math.max(...childrenDimensions.map((dim: any) => dim.height)) + (GROUP_PADDING * 2) + 40;
      } else {
        // Parallel/Loop: Max width, Sum of heights
        const childrenDimensions = subAgents.map((sub: string) =>
          getLayout(sub, config.agents![sub]?.type)
        );
        width = Math.max(...childrenDimensions.map((dim: any) => dim.width)) + (GROUP_PADDING * 2);
        height = childrenDimensions.reduce((acc: number, dim: any) => acc + dim.height, 0) +
          ((subAgents.length - 1) * VERTICAL_GAP) + (GROUP_PADDING * 2) + 40;
      }

      return { width, height, childNodes: subAgents };
    };

    // Recursive render function
    const renderNode = (agentId: string, x: number, y: number, parentId?: string) => {
      if (processedAgents.has(agentId)) {
        // If already processed but as a root, we might need to edges?
        // But for strict hierarchy, we shouldn't process twice.
        // However, if it's referenced multiple times, we might need a visual link or duplicate.
        // For now, assume unique ownership or just link to existing.
        return;
      }
      processedAgents.add(agentId);

      const agent = config.agents![agentId];
      if (!agent) return;

      const type = agent.type;
      const isWorkflow = type && ['sequential', 'parallel', 'loop'].includes(type);
      const subAgents = agent.sub_agents || [];

      if (!isWorkflow || subAgents.length === 0) {
        // Render simple agent node
        nodes.push({
          id: agentId,
          type: 'agent',
          position: { x, y },
          parentId,
          extent: parentId ? 'parent' : undefined,
          data: {
            label: agent.name || agentId,
            agentId: agentId, // Include agent ID for matching with backend author
            llm: agent.llm,
            description: agent.description,
            instruction: agent.instruction,
          },
          style: { width: NODE_WIDTH, height: NODE_HEIGHT },
        });
        return;
      }

      // Render workflow group
      const dim = getLayout(agentId, type);

      nodes.push({
        id: agentId,
        type: 'workflowGroup',
        position: { x, y },
        parentId,
        data: {
          label: agentId,
          workflowType: type,
          subAgents,
          maxIterations: agent.max_iterations,
        },
        style: {
          width: dim.width,
          height: dim.height,
          zIndex: -1,
        },
      });

      // Render children
      let childX = GROUP_PADDING;
      let childY = GROUP_PADDING + 40;

      subAgents.forEach((subId: string, index: number) => {
        const subDim = getLayout(subId, config.agents![subId]?.type);

        renderNode(subId, childX, childY, agentId);

        // Add Edges
        // Add Edges
        if (type === 'sequential' && index < subAgents.length - 1) {
          const nextId = subAgents[index + 1];
          edges.push({
            id: `edge-${subId}-${nextId}`,
            source: subId,
            target: nextId,
            sourceHandle: 'right', // Horizontal flow: Right -> Left
            targetHandle: 'left',
            type: 'default', // Smooth Bezier
            style: { stroke: '#3b82f6', strokeWidth: 2 },
            zIndex: 10,
          });
        } else if (type === 'loop' && index < subAgents.length - 1) {
          const nextId = subAgents[index + 1];
          edges.push({
            id: `edge-${subId}-${nextId}`,
            source: subId,
            target: nextId,
            sourceHandle: 'bottom', // Vertical flow: Bottom -> Top
            targetHandle: 'top',
            type: 'default',
            style: { stroke: '#14b8a6', strokeWidth: 2 },
            zIndex: 10,
          });
        }

        // Move current cursor
        if (type === 'sequential') {
          childX += subDim.width + HORIZONTAL_GAP;
        } else {
          childY += subDim.height + VERTICAL_GAP;
        }
      });

      // Loop back edge
      if (type === 'loop' && subAgents.length > 1) {
        edges.push({
          id: `edge-loop-${agentId}`,
          source: subAgents[subAgents.length - 1],
          target: subAgents[0],
          // Loop back: Use Right handles for a clean side curve
          sourceHandle: 'right',
          targetHandle: 'right',
          type: 'default',
          style: { stroke: '#14b8a6', strokeWidth: 2, strokeDasharray: '5 5' },
          animated: true,
          zIndex: 10,
        });
      }
    };

    // Main Loop: Render all top-level agents (roots)
    // A top-level agent is one that is NOT a sub_agent of anyone else
    const allSubAgents = new Set<string>();
    Object.values(config.agents).forEach((a: any) => {
      if (a.sub_agents) a.sub_agents.forEach((s: string) => allSubAgents.add(s));
    });

    const roots = Object.keys(config.agents).filter(id => !allSubAgents.has(id));

    roots.forEach(rootId => {
      const dim = getLayout(rootId, config.agents![rootId]?.type);
      renderNode(rootId, 100, currentY, undefined);
      currentY += dim.height + 100;
    });

    // If there were cycles or orphans not caught, handle them?
    // For now, this covers the tree structure.

    return { nodes, edges };
  } catch (error) {
    console.error('Failed to parse YAML:', error);
    return { nodes: [], edges: [] };
  }
};

/**
 * Validates YAML for canvas visualization
 */
export const validateYAMLForCanvas = (yamlContent: string): { valid: boolean; error?: string } => {
  try {
    const config = yaml.load(yamlContent) as Config;
    if (!config) {
      return { valid: false, error: 'Empty configuration' };
    }
    return { valid: true };
  } catch (error) {
    return {
      valid: false,
      error: error instanceof Error ? error.message : 'Invalid YAML'
    };
  }
};
