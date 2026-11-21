package mermaid

// Representative examples, not an exhaustive Mermaid syntax reference. All
// sources are local trusted fixtures and are tested with the pinned renderer.
var galleryExamples = []Example{
	{"Type - Agent flow", `agentflow-beta TB
  input["Question"] --> agent["Assistant"]
  agent --> output["Response"]`},
	{"Type - Block", `block
  columns 3
  input["Input"] process["Process"] output["Output"]`},
	{"Type - C4", `C4Context
  title Service context
  Person(customer, "Customer", "Uses the application")
  System(app, "Application", "Provides a service")
  Rel(customer, app, "Uses", "HTTPS")`},
	{"Type - Class", `classDiagram
  class Service {
    +String name
    +start()
  }
  class Worker {
    +run()
  }
  Service --> Worker : starts`},
	{"Type - Cynefin", `cynefin-beta
  title Decision contexts
  complex
  complicated
  clear
  chaotic`},
	{"Type - Event modeling", `eventmodeling
  tf 01 ui OrderForm
  tf 02 cmd SubmitOrder
  tf 03 evt OrderSubmitted`},
	{"Type - Gantt", `gantt
  title Delivery plan
  dateFormat YYYY-MM-DD
  section Release
  Design :a, 2025-01-01, 5d
  Build :b, after a, 7d
  Test :after b, 3d`},
	{"Type - Git graph", `gitGraph
  commit id: "Initial"
  branch feature
  commit id: "Feature"
  checkout main
  merge feature`},
	{"Type - Info", `info`},
	{"Type - Ishikawa", `ishikawa-beta
  Slow delivery
  Process
    Manual review
    Waiting for approval
  Systems
    Build time
    Flaky tests`},
	{"Type - Journey", `journey
  title Customer journey
  section Purchase
    Browse: 4: Customer
    Checkout: 2: Customer
  section Delivery
    Receive order: 5: Customer`},
	{"Type - Kanban", `kanban
  todo[To do]
    task1[Review design]
  doing[In progress]
    task2[Build feature]
  done[Done]
    task3[Plan work]`},
	{"Type - Mindmap", `mindmap
  root((Architecture))
    Clients
      Desktop
      Terminal
    Services
      API
      Database`},
	{"Type - Packet", `packet
  title Message header
  0-7: "Version"
  8-15: "Flags"
  16-31: "Length"
  32-63: "Payload"`},
	{"Type - Quadrant", `quadrantChart
  title Work priority
  x-axis Low impact --> High impact
  y-axis Low effort --> High effort
  quadrant-1 Plan
  quadrant-2 Investigate
  quadrant-3 Defer
  quadrant-4 Do first
  Item A: [0.7, 0.3]
  Item B: [0.4, 0.8]`},
	{"Type - Radar", `radar-beta
  axis speed["Speed"], quality["Quality"], cost["Cost"]
  curve plan["Plan"]{70, 90, 60}
  max 100
  min 0`},
	{"Type - Railroad", `railroad-ebnf-beta
  expression = term ( "+" term )* ;
  term = "number" ;`},
	{"Type - Requirement", `requirementDiagram
  requirement availability {
    id: 1
    text: "Service remains available"
    risk: high
    verifymethod: test
  }
  element service {
    type: system
  }
  service - satisfies -> availability`},
	{"Type - Sankey", `sankey-beta
  Solar,Grid,30
  Wind,Grid,20
  Grid,Homes,35
  Grid,Industry,15`},
	{"Type - Swimlane", `swimlane-beta
  subgraph Customer
    request[Send request]
  end
  subgraph Service
    process[Process request]
    respond[Send response]
  end
  request --> process
  process --> respond`},
	{"Type - Timeline", `timeline
  title Project milestones
  2024 : Prototype
  2025 : Pilot : Feedback
  2026 : Release`},
	{"Type - Treemap", `treemap-beta
  "Applications"
    "Frontend": 30
    "Backend": 40
  "Data"
    "Storage": 20
    "Analytics": 10`},
	{"Type - Tree view", `treeView-beta
  "workspace"
    "clients"
      "desktop"
      "terminal"
    "server"
      "api"`},
	{"Type - Use case", `usecase-beta
  actor Customer
  Order("Place order")
  Track("Track order")
  Customer --> Order
  Customer --> Track`},
	{"Type - Venn", `venn-beta
  title Team overlap
  set Design["Design"]
  set Engineering["Engineering"]
  union Design,Engineering["Product"]`},
	{"Type - Wardley", `wardley-beta
  title Service landscape
  component Platform [0.4, 0.5]
  component Compute [0.7, 0.3]
  Platform -> Compute`},
	{"Type - XY chart", `xychart
  title "Requests over time"
  x-axis [Jan, Feb, Mar, Apr]
  y-axis "Requests" 0 --> 100
  bar [30, 45, 70, 80]
  line [25, 40, 60, 75]`},
	{"Feature - Architecture icons", `architecture-beta
  service cloud1(cloud)[Cloud]
  service database1(database)[Database]
  service disk1(disk)[Disk]
  service internet1(internet)[Internet]
  service server1(server)[Server]
  cloud1:R -- L:database1
  database1:R -- L:disk1
  disk1:R -- L:internet1
  internet1:R -- L:server1`},
	{"Feature - Flowchart shapes", `flowchart LR
  a[Rectangle] --> b(Rounded)
  b --> c([Stadium])
  c --> d{Decision}
  d --> e[(Database)]
  e --> f((Circle))`},
	{"Feature - Edges and labels", `flowchart LR
  a[Source] -->|solid| b[One]
  a -.->|dotted| c[Two]
  a ==>|thick| d[Three]
  b --- c`},
	{"Feature - Groups", `flowchart LR
  subgraph edge["Edge zone"]
    gateway[Gateway]
  end
  subgraph private["Private zone"]
    app[Application]
    db[(Database)]
  end
  gateway -->|HTTPS| app
  app -->|SQL| db`},
	{"Feature - Styling", `flowchart LR
  a[Healthy] --> b[Warning]
  classDef healthy fill:#d9eedc,stroke:#397448,color:#16391e
  classDef warning fill:#fff1c6,stroke:#986f00,color:#382c00
  class a healthy
  class b warning`},
	{"Feature - Sequence notes", `sequenceDiagram
  participant C as Client
  participant S as Service
  C->>S: Request
  activate S
  Note over C,S: Process request
  S-->>C: Response
  deactivate S`},
}

func init() {
	// Keep the fixture ordering stable for compatibility tests.
	last := Corpus[len(Corpus)-1]
	Corpus = append(Corpus[:len(Corpus)-1], galleryExamples...)
	Corpus = append(Corpus, last)
}
