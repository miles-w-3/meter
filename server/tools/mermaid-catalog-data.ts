// Catalog v1: original examples validated against the Mermaid regression corpus.
export const catalogEntries = [
  {
    "kind": "diagram_type",
    "name": "flowchart",
    "description": "Show nodes, directional connections and grouped steps.",
    "example": "flowchart LR\n A[Start] --> B[Done]",
    "fixture": "Flowchart"
  },
  {
    "kind": "diagram_type",
    "name": "sequence",
    "description": "Show messages between participants over time.",
    "example": "sequenceDiagram\n Alice->>Bob: Hello\n Bob-->>Alice: World",
    "fixture": "Sequence"
  },
  {
    "kind": "diagram_type",
    "name": "state",
    "description": "Show states and transitions.",
    "example": "stateDiagram-v2\n [*] --> Ready\n Ready --> [*]",
    "fixture": "State"
  },
  {
    "kind": "diagram_type",
    "name": "er",
    "description": "Show entities and relationships.",
    "example": "erDiagram\n CUSTOMER ||--o{ ORDER : places\n CUSTOMER {\n string name\n }",
    "fixture": "ER"
  },
  {
    "kind": "diagram_type",
    "name": "pie",
    "description": "Show values as proportions of a whole.",
    "example": "pie title Pets\n \"Dogs\" : 42\n \"Cats\" : 30",
    "fixture": "Pie"
  },
  {
    "kind": "diagram_type",
    "name": "architecture",
    "description": "Show services, groups and infrastructure connections.",
    "example": "architecture-beta\n    group cloud(cloud)[Application environment]\n    service edge(internet)[Gateway] in cloud\n    service app(server)[API service] in cloud\n    service db(database)[Database] in cloud\n    service store(disk)[Object storage] in cloud\n    edge:R --> L:app\n    app:R --> L:db\n    app:B --> T:store",
    "fixture": "Architecture beta"
  },
  {
    "kind": "diagram_type",
    "name": "agentflow",
    "description": "Show agent workflows and transitions.",
    "example": "agentflow-beta TB\n  input[\"Question\"] --> agent[\"Assistant\"]\n  agent --> output[\"Response\"]",
    "fixture": "Type - Agent flow"
  },
  {
    "kind": "diagram_type",
    "name": "block",
    "description": "Arrange labeled blocks in a grid.",
    "example": "block\n  columns 3\n  input[\"Input\"] process[\"Process\"] output[\"Output\"]",
    "fixture": "Type - Block"
  },
  {
    "kind": "diagram_type",
    "name": "c4",
    "description": "Describe software systems and their relationships with C4 notation.",
    "example": "C4Context\n  title Service context\n  Person(customer, \"Customer\", \"Uses the application\")\n  System(app, \"Application\", \"Provides a service\")\n  Rel(customer, app, \"Uses\", \"HTTPS\")",
    "fixture": "Type - C4"
  },
  {
    "kind": "diagram_type",
    "name": "class",
    "description": "Show classes, members and relationships.",
    "example": "classDiagram\n  class Service {\n    +String name\n    +start()\n  }\n  class Worker {\n    +run()\n  }\n  Service --> Worker : starts",
    "fixture": "Type - Class"
  },
  {
    "kind": "diagram_type",
    "name": "cynefin",
    "description": "Show contexts in the Cynefin decision framework.",
    "example": "cynefin-beta\n  title Decision contexts\n  complex\n  complicated\n  clear\n  chaotic",
    "fixture": "Type - Cynefin"
  },
  {
    "kind": "diagram_type",
    "name": "eventmodeling",
    "description": "Show user interface, commands and events.",
    "example": "eventmodeling\n  tf 01 ui OrderForm\n  tf 02 cmd SubmitOrder\n  tf 03 evt OrderSubmitted",
    "fixture": "Type - Event modeling"
  },
  {
    "kind": "diagram_type",
    "name": "gantt",
    "description": "Show tasks and scheduling over time.",
    "example": "gantt\n  title Delivery plan\n  dateFormat YYYY-MM-DD\n  section Release\n  Design :a, 2025-01-01, 5d\n  Build :b, after a, 7d\n  Test :after b, 3d",
    "fixture": "Type - Gantt"
  },
  {
    "kind": "diagram_type",
    "name": "gitGraph",
    "description": "Show commits, branches and merges.",
    "example": "gitGraph\n  commit id: \"Initial\"\n  branch feature\n  commit id: \"Feature\"\n  checkout main\n  merge feature",
    "fixture": "Type - Git graph"
  },
  {
    "kind": "diagram_type",
    "name": "info",
    "description": "Show renderer version information.",
    "example": "info",
    "fixture": "Type - Info"
  },
  {
    "kind": "diagram_type",
    "name": "ishikawa",
    "description": "Show causes contributing to an effect.",
    "example": "ishikawa-beta\n  Slow delivery\n  Process\n    Manual review\n    Waiting for approval\n  Systems\n    Build time\n    Flaky tests",
    "fixture": "Type - Ishikawa"
  },
  {
    "kind": "diagram_type",
    "name": "journey",
    "description": "Show stages and scores in a user journey.",
    "example": "journey\n  title Customer journey\n  section Purchase\n    Browse: 4: Customer\n    Checkout: 2: Customer\n  section Delivery\n    Receive order: 5: Customer",
    "fixture": "Type - Journey"
  },
  {
    "kind": "diagram_type",
    "name": "kanban",
    "description": "Show work items in columns.",
    "example": "kanban\n  todo[To do]\n    task1[Review design]\n  doing[In progress]\n    task2[Build feature]\n  done[Done]\n    task3[Plan work]",
    "fixture": "Type - Kanban"
  },
  {
    "kind": "diagram_type",
    "name": "mindmap",
    "description": "Show hierarchical ideas branching from a root.",
    "example": "mindmap\n  root((Architecture))\n    Clients\n      Desktop\n      Terminal\n    Services\n      API\n      Database",
    "fixture": "Type - Mindmap"
  },
  {
    "kind": "diagram_type",
    "name": "packet",
    "description": "Show fields and bit ranges in a packet.",
    "example": "packet\n  title Message header\n  0-7: \"Version\"\n  8-15: \"Flags\"\n  16-31: \"Length\"\n  32-63: \"Payload\"",
    "fixture": "Type - Packet"
  },
  {
    "kind": "diagram_type",
    "name": "quadrantChart",
    "description": "Show points in four quadrants.",
    "example": "quadrantChart\n  title Work priority\n  x-axis Low impact --> High impact\n  y-axis Low effort --> High effort\n  quadrant-1 Plan\n  quadrant-2 Investigate\n  quadrant-3 Defer\n  quadrant-4 Do first\n  Item A: [0.7, 0.3]\n  Item B: [0.4, 0.8]",
    "fixture": "Type - Quadrant"
  },
  {
    "kind": "diagram_type",
    "name": "radar",
    "description": "Compare values across several axes.",
    "example": "radar-beta\n  axis speed[\"Speed\"], quality[\"Quality\"], cost[\"Cost\"]\n  curve plan[\"Plan\"]{70, 90, 60}\n  max 100\n  min 0",
    "fixture": "Type - Radar"
  },
  {
    "kind": "diagram_type",
    "name": "railroad",
    "description": "Show grammar rules as railroad diagrams.",
    "example": "railroad-ebnf-beta\n  expression = term ( \"+\" term )* ;\n  term = \"number\" ;",
    "fixture": "Type - Railroad"
  },
  {
    "kind": "diagram_type",
    "name": "requirement",
    "description": "Show requirements, elements and relationships.",
    "example": "requirementDiagram\n  requirement availability {\n    id: 1\n    text: \"Service remains available\"\n    risk: high\n    verifymethod: test\n  }\n  element service {\n    type: system\n  }\n  service - satisfies -> availability",
    "fixture": "Type - Requirement"
  },
  {
    "kind": "diagram_type",
    "name": "sankey",
    "description": "Show weighted flows between nodes.",
    "example": "sankey-beta\n  Solar,Grid,30\n  Wind,Grid,20\n  Grid,Homes,35\n  Grid,Industry,15",
    "fixture": "Type - Sankey"
  },
  {
    "kind": "diagram_type",
    "name": "swimlane",
    "description": "Group workflow steps into lanes.",
    "example": "swimlane-beta\n  subgraph Customer\n    request[Send request]\n  end\n  subgraph Service\n    process[Process request]\n    respond[Send response]\n  end\n  request --> process\n  process --> respond",
    "fixture": "Type - Swimlane"
  },
  {
    "kind": "diagram_type",
    "name": "timeline",
    "description": "Show dated events along a timeline.",
    "example": "timeline\n  title Project milestones\n  2024 : Prototype\n  2025 : Pilot : Feedback\n  2026 : Release",
    "fixture": "Type - Timeline"
  },
  {
    "kind": "diagram_type",
    "name": "treemap",
    "description": "Show hierarchical data as sized rectangles.",
    "example": "treemap-beta\n  \"Applications\"\n    \"Frontend\": 30\n    \"Backend\": 40\n  \"Data\"\n    \"Storage\": 20\n    \"Analytics\": 10",
    "fixture": "Type - Treemap"
  },
  {
    "kind": "diagram_type",
    "name": "treeView",
    "description": "Show hierarchical items as a tree.",
    "example": "treeView-beta\n  \"workspace\"\n    \"clients\"\n      \"desktop\"\n      \"terminal\"\n    \"server\"\n      \"api\"",
    "fixture": "Type - Tree view"
  },
  {
    "kind": "diagram_type",
    "name": "usecase",
    "description": "Show actors and use cases.",
    "example": "usecase-beta\n  actor Customer\n  Order(\"Place order\")\n  Track(\"Track order\")\n  Customer --> Order\n  Customer --> Track",
    "fixture": "Type - Use case"
  },
  {
    "kind": "diagram_type",
    "name": "venn",
    "description": "Show intersections between sets.",
    "example": "venn-beta\n  title Team overlap\n  set Design[\"Design\"]\n  set Engineering[\"Engineering\"]\n  union Design,Engineering[\"Product\"]",
    "fixture": "Type - Venn"
  },
  {
    "kind": "diagram_type",
    "name": "wardley",
    "description": "Show components on a Wardley map.",
    "example": "wardley-beta\n  title Service landscape\n  component Platform [0.4, 0.5]\n  component Compute [0.7, 0.3]\n  Platform -> Compute",
    "fixture": "Type - Wardley"
  },
  {
    "kind": "diagram_type",
    "name": "xychart",
    "description": "Plot values along x and y axes.",
    "example": "xychart\n  title \"Requests over time\"\n  x-axis [Jan, Feb, Mar, Apr]\n  y-axis \"Requests\" 0 --> 100\n  bar [30, 45, 70, 80]\n  line [25, 40, 60, 75]",
    "fixture": "Type - XY chart"
  },
  {
    "kind": "icon",
    "name": "cloud",
    "description": "Generic cloud infrastructure icon.",
    "keywords": [
      "cloud",
      "environment"
    ],
    "diagramType": "architecture",
    "example": "service component(cloud)[Component]",
    "fixture": "Feature - Architecture icons"
  },
  {
    "kind": "icon",
    "name": "database",
    "description": "Generic database icon.",
    "keywords": [
      "database",
      "data",
      "storage"
    ],
    "diagramType": "architecture",
    "example": "service component(database)[Component]",
    "fixture": "Feature - Architecture icons"
  },
  {
    "kind": "icon",
    "name": "disk",
    "description": "Generic disk icon.",
    "keywords": [
      "disk",
      "storage"
    ],
    "diagramType": "architecture",
    "example": "service component(disk)[Component]",
    "fixture": "Feature - Architecture icons"
  },
  {
    "kind": "icon",
    "name": "internet",
    "description": "Generic internet or network icon.",
    "keywords": [
      "internet",
      "network",
      "gateway",
      "external"
    ],
    "diagramType": "architecture",
    "example": "service component(internet)[Component]",
    "fixture": "Feature - Architecture icons"
  },
  {
    "kind": "icon",
    "name": "server",
    "description": "Generic server icon.",
    "keywords": [
      "server",
      "compute",
      "service",
      "host"
    ],
    "diagramType": "architecture",
    "example": "service component(server)[Component]",
    "fixture": "Feature - Architecture icons"
  },
  {
    "kind": "feature",
    "name": "flowchart shapes",
    "description": "Node shapes including rounded, decision, database and circle.",
    "diagramType": "flowchart",
    "example": "flowchart LR\n  a[Rectangle] --> b(Rounded)\n  b --> c([Stadium])\n  c --> d{Decision}\n  d --> e[(Database)]\n  e --> f((Circle))",
    "fixture": "Feature - Flowchart shapes"
  },
  {
    "kind": "feature",
    "name": "flowchart edges",
    "description": "Solid, dotted and thick edges with optional labels.",
    "diagramType": "flowchart",
    "example": "flowchart LR\n  a[Source] -->|solid| b[One]\n  a -.->|dotted| c[Two]\n  a ==>|thick| d[Three]\n  b --- c",
    "fixture": "Feature - Edges and labels"
  },
  {
    "kind": "feature",
    "name": "flowchart subgraphs",
    "description": "Group nodes in labeled boundaries.",
    "diagramType": "flowchart",
    "example": "flowchart LR\n  subgraph edge[\"Edge zone\"]\n    gateway[Gateway]\n  end\n  subgraph private[\"Private zone\"]\n    app[Application]\n    db[(Database)]\n  end\n  gateway -->|HTTPS| app\n  app -->|SQL| db",
    "fixture": "Feature - Groups"
  },
  {
    "kind": "feature",
    "name": "flowchart styling",
    "description": "Use class definitions to style nodes.",
    "diagramType": "flowchart",
    "example": "flowchart LR\n  a[Healthy] --> b[Warning]\n  classDef healthy fill:#d9eedc,stroke:#397448,color:#16391e\n  classDef warning fill:#fff1c6,stroke:#986f00,color:#382c00\n  class a healthy\n  class b warning",
    "fixture": "Feature - Styling"
  },
  {
    "kind": "feature",
    "name": "sequence notes and activation",
    "description": "Add notes and activation intervals to sequence diagrams.",
    "diagramType": "sequence",
    "example": "sequenceDiagram\n  participant C as Client\n  participant S as Service\n  C->>S: Request\n  activate S\n  Note over C,S: Process request\n  S-->>C: Response\n  deactivate S",
    "fixture": "Feature - Sequence notes"
  }
] as const;
