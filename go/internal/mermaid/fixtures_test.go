// Trusted Mermaid fixtures used to test renderer and catalog compatibility.
package mermaid

type Example struct{ Name, Source string }

var Corpus = []Example{
	{"Flowchart", "flowchart LR\n A[Start] --> B[Done]"},
	{"Sequence", "sequenceDiagram\n Alice->>Bob: Hello\n Bob-->>Alice: World"},
	{"State", "stateDiagram-v2\n [*] --> Ready\n Ready --> [*]"},
	{"ER", "erDiagram\n CUSTOMER ||--o{ ORDER : places\n CUSTOMER {\n string name\n }"},
	{"Pie", "pie title Pets\n \"Dogs\" : 42\n \"Cats\" : 30"},
	{"Unicode and long label", "flowchart LR\n A[\"日本語 café 🚀\"] --> B[\"A long label with many words to check the layout of this diagram\"]"},
	{"Network zones", `flowchart LR
    user[Remote user] -->|TLS 443| waf[WAF and load balancer]
    user -->|VPN| vpn[VPN gateway]
    subgraph public[Public subnet / DMZ]
      waf
      vpn
      bastion[Bastion host]
    end
    subgraph private[Private application subnet]
      app[Application service]
      worker[Background worker]
    end
    subgraph data[Restricted data subnet]
      db[(Primary database)]
      cache[(Cache)]
    end
    waf -->|HTTPS 8443| app
    vpn -->|SSH 22| bastion
    bastion -->|SSH 22| app
    app -->|SQL 5432| db
    app -->|TCP 6379| cache
    worker -->|SQL 5432| db`},
	{"Kubernetes deployment", `flowchart TB
    internet[Internet] --> ingress
    subgraph cluster[Kubernetes cluster]
      ingress[Ingress controller]
      subgraph webns[Web namespace]
        svc[API service]
        pod1[API pod 1]
        pod2[API pod 2]
        ingress --> svc
        svc --> pod1
        svc --> pod2
      end
      subgraph workns[Workers namespace]
        job[Worker deployment]
        queue[(Message queue)]
        queue --> job
      end
      pod1 --> queue
      pod2 --> queue
    end
    pod1 --> db[(Managed database)]
    pod2 --> db
    job --> db`},
	{"Architecture beta", `architecture-beta
    group cloud(cloud)[Application environment]
    service edge(internet)[Gateway] in cloud
    service app(server)[API service] in cloud
    service db(database)[Database] in cloud
    service store(disk)[Object storage] in cloud
    edge:R --> L:app
    app:R --> L:db
    app:B --> T:store`},
	{"Large flowchart", "flowchart LR\n A[Start] --> B[Collect input]\n B --> C{Valid?}\n C -->|yes| D[Process]\n C -->|no| E[Report error]\n E --> B\n D --> F[Save]\n F --> G[Notify]\n G --> H[Finish]\n D --> I[Audit]\n I --> H"},
}
