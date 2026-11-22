// Deterministic, local-only examples for the GUI's Mermaid sizing preview.
export const mermaidPreview: Record<string, string> = {
  tiny: `### Small: two steps
A compact diagram should not require a large empty box.

\`\`\`mermaid
flowchart LR
  A[Start] --> B[Done]
\`\`\``,
  wide: `### Wide: request lifecycle
This one exercises fit-to-width, without enlarging small diagrams.

\`\`\`mermaid
flowchart LR
  A[Open project] --> B[Choose conversation] --> C[Prepare context] --> D[Run tools] --> E[Stream response] --> F[Save result]
\`\`\``,
  tall: `### Tall: decision path
The height should grow with the rendering, but stop at the maximum and scroll.

\`\`\`mermaid
flowchart TD
  A[Receive request] --> B{Authenticated?}
  B -->|Yes| C[Load project]
  B -->|No| X[Ask to sign in]
  C --> D[Read conversation]
  D --> E[Select model]
  E --> F[Build context]
  F --> G[Run assistant]
  G --> H[Persist output]
  H --> I[Show response]
\`\`\``,
  square: `### Balanced: conversation lifecycle
A more square diagram makes the contrast with the wide and tall examples clear.

\`\`\`mermaid
stateDiagram-v2
  [*] --> Idle
  Idle --> Running: submit
  Running --> Waiting: tool call
  Waiting --> Running: tool result
  Running --> Idle: complete
  Running --> Failed: error
  Failed --> Idle: retry
\`\`\``,
};
