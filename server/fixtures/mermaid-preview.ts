// Deterministic, local-only examples for the GUI's Mermaid sizing preview.
export const mermaidPreview: Record<string, string> = {
  "wide-data": `### Wide fictional workshop

\`\`\`mermaid
flowchart LR
    subgraph venue["Imaginary venue: west gallery"]
        ARCHIVE[("Collection of painted scenery<br/>/sets/seasonal--panels--catalogue<br/>+ illustrated inventory card")]
        subgraph visitors["Visiting teams: by arrangement"]
            GUEST["Visiting artist with a sketchbook<br/>NOTES_FOLDER=studio/drawings"]
        end
        subgraph workshop["Community theatre workshop"]
            DESK{{"Information desk<br/>stage-and-prop-register<br/>coordinates the production schedule"}}
            subgraph studio["Main studio: shared worktables and tools"]
                subgraph crew["Small volunteer crew, rotating weekly"]
                    CURATOR["wardrobe coordinator<br/>checks costumes and labels<br/>keeps a handwritten catalogue of every item"]
                    MAKER["scenery painter<br/>prepares sketches, then mixes colours"]
                end
            end
            subgraph balcony["Balcony workspace"]
                LIGHT["lighting assistant<br/>adjusts lamps and paper filters<br/>records changes in the rehearsal notebook"]
                ACTOR["performer<br/>practises entrances and cues"]
            end
            subgraph garden["Garden workspace"]
                SOUND["sound assistant<br/>collects recordings and small instruments<br/>sets out the portable music stands"]
                DANCER["choreographer<br/>arranges movement and cues"]
            end
        end
    end
    LIBRARY(("Story library"))
    CURATOR -- "1. choose a tale from the library" --> LIBRARY
    MAKER -- "adds sketches to the collection" --> ARCHIVE
    MAKER -- "2. requests a costume fitting" --> CURATOR
    CURATOR -- "checks the illustrated catalogue" --> ARCHIVE
    GUEST -- "consults the collection / shares sketches" --> ARCHIVE
    GUEST -. "leaves a note for the crew<br/>before the next rehearsal" .-> MAKER
    CURATOR -. "weekly update" .-> DESK
    SOUND -. "weekly update" .-> DESK
    LIGHT -. "weekly update" .-> DESK
    ACTOR -- "3. review the evening cues<br/>with the lighting assistant" --> LIGHT
    LIGHT -- "4. ask about the afternoon schedule" --> DESK
    LIGHT -- "5. borrow a costume and return it later" --> CURATOR
    SOUND -. "share a melody with<br/>the lighting team" .-> LIGHT
    ACTOR -- "6. rehearse a scene on the balcony" --> LIGHT
    DANCER -- "arrange a short rehearsal" --> LIGHT
\`\`\``,
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
